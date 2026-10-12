package uir

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	jsonv1 "github.com/go-json-experiment/json/v1"
)

// The UIR codec encodes in one pass over a jsontext.Encoder, with encoding/json
// (v1) semantics for every plain field. The types that own interface-typed
// slots write them as {"$<kind>": <value>} themselves (MarshalJSONTo), stamping
// the value's discriminator without buffering it; their MarshalJSON methods
// delegate here, so encoding/json callers take the same path below the first
// custom-encoded value.

// Marshal encodes UIR — a node, a statement, a slice of them, or any struct
// holding them — in one pass.
func Marshal(v any) ([]byte, error) {
	return jsonv2.Marshal(v, jsonv1.DefaultOptionsV1())
}

// statementSlots encodes block children: each under its kind.
type statementSlots []Statement

func (s statementSlots) MarshalJSONTo(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.BeginArray); err != nil {
		return err
	}
	for i, stmt := range s {
		if err := writeStatementSlot(enc, stmt); err != nil {
			return fmt.Errorf("children[%d]: %T: %w", i, stmt, err)
		}
	}
	return enc.WriteToken(jsontext.EndArray)
}

// nodeSlots encodes a Node list: each under its node_kind.
type nodeSlots []Node

func (s nodeSlots) MarshalJSONTo(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.BeginArray); err != nil {
		return err
	}
	for i, node := range s {
		if err := writeNodeSlot(enc, node); err != nil {
			return fmt.Errorf("nodes[%d]: %T: %w", i, node, err)
		}
	}
	return enc.WriteToken(jsontext.EndArray)
}

// nodeSlot encodes one Node-typed field. A field holds a *nodeSlot, nil for an
// absent node, so omitempty drops it.
type nodeSlot struct{ node Node }

func slotOf(node Node) *nodeSlot {
	if node == nil {
		return nil
	}
	return &nodeSlot{node: node}
}

func (s nodeSlot) MarshalJSONTo(enc *jsontext.Encoder) error {
	return writeNodeSlot(enc, s.node)
}

func writeSlot(enc *jsontext.Encoder, kind string, value any) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String(SlotKey(kind))); err != nil {
		return err
	}
	if err := jsonv2.MarshalEncode(enc, value); err != nil {
		return err
	}
	return enc.WriteToken(jsontext.EndObject)
}

// writeStatementSlot writes a statement under its kind, carrying its own Type —
// its kind, or the refinement it holds — under statement_type.
func writeStatementSlot(enc *jsontext.Encoder, stmt Statement) error {
	kind, err := StatementMarshaler.KindOf(stmt)
	if err != nil {
		return err
	}
	value := concreteValue(stmt)
	typed, ok := value.Addr().Interface().(carriedTyped)
	if !ok {
		return writeSlot(enc, kind, stamped(value, StatementMarshaler.field, kind))
	}
	if carried := typed.carriedType(); carried == "" {
		typed.setCarriedType(StatementType(kind))
	} else if string(carried) != kind {
		if err := StatementMarshaler.checkRefinement(kind, string(carried)); err != nil {
			return err
		}
	}
	return writeSlot(enc, kind, value.Addr().Interface())
}

// writeNodeSlot writes a node under its node_kind, stamped ahead of its fields.
func writeNodeSlot(enc *jsontext.Encoder, node Node) error {
	kind, err := NodeMarshaler.KindOf(node)
	if err != nil {
		return err
	}
	return writeSlot(enc, kind, stamped(concreteValue(node), NodeMarshaler.field, kind))
}

// concreteValue is an addressable copy of the concrete value behind v, which the
// encoder may stamp without touching the caller's value.
func concreteValue(v any) reflect.Value {
	value := reflect.ValueOf(v)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	out := reflect.New(value.Type()).Elem()
	out.Set(value)
	return out
}

// stampedValue writes leading string fields, then value's own members, minus any
// a leading field replaces.
type stampedValue struct {
	names, values []string
	value         reflect.Value
}

// stamped writes value with the given name/value string pairs ahead of its own
// fields.
func stamped(value reflect.Value, pairs ...string) any {
	s := stampedValue{value: value}
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			s.names, s.values = append(s.names, pairs[i]), append(s.values, pairs[i+1])
		}
	}
	return s
}

var (
	marshalerTo     = reflect.TypeFor[jsonv2.MarshalerTo]()
	marshalerV1     = reflect.TypeFor[jsonv2.Marshaler]()
	textMarshaler   = reflect.TypeFor[interface{ MarshalText() ([]byte, error) }]()
	inlinedTypes    sync.Map // map[inlinedKey]reflect.Type
	hasMarshalCache sync.Map // map[reflect.Type]bool
)

type inlinedKey struct {
	names string
	typ   reflect.Type
}

func hasMarshalMethod(t reflect.Type) bool {
	if cached, ok := hasMarshalCache.Load(t); ok {
		return cached.(bool)
	}
	p := reflect.PointerTo(t)
	has := p.Implements(marshalerTo) || p.Implements(marshalerV1) || p.Implements(textMarshaler)
	hasMarshalCache.Store(t, has)
	return has
}

func (s stampedValue) MarshalJSONTo(enc *jsontext.Encoder) error {
	if len(s.names) == 0 {
		return jsonv2.MarshalEncode(enc, s.value.Addr().Interface())
	}
	if !hasMarshalMethod(s.value.Type()) {
		return jsonv2.MarshalEncode(enc, s.inlined())
	}
	// A value that encodes itself cannot be inlined: encode it apart, then copy
	// its members behind the leading fields. Only small values take this path —
	// the call statements and relationships, whose slots hold shallow node refs.
	var buf bytes.Buffer
	if err := jsonv2.MarshalWrite(&buf, s.value.Addr().Interface(), enc.Options()); err != nil {
		return err
	}
	dec := jsontext.NewDecoder(&buf)
	if tok, err := dec.ReadToken(); err != nil {
		return err
	} else if tok.Kind() != '{' {
		return fmt.Errorf("%s encodes as %v, not an object", s.value.Type(), tok.Kind())
	}
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	for i, name := range s.names {
		if err := enc.WriteToken(jsontext.String(name)); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.String(s.values[i])); err != nil {
			return err
		}
	}
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		name := tok.String()
		value, err := dec.ReadValue()
		if err != nil {
			return err
		}
		if slices.Contains(s.names, name) {
			continue
		}
		if err := enc.WriteToken(jsontext.String(name)); err != nil {
			return err
		}
		if err := enc.WriteValue(value); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndObject)
}

// inlined builds (once per field set and type) a struct holding the leading
// fields and then the value's own fields inline, and fills it.
func (s stampedValue) inlined() any {
	key := inlinedKey{names: strings.Join(s.names, "\x00"), typ: s.value.Type()}
	cached, ok := inlinedTypes.Load(key)
	if !ok {
		fields := make([]reflect.StructField, 0, len(s.names)+1)
		for i, name := range s.names {
			fields = append(fields, reflect.StructField{
				Name: fmt.Sprintf("Lead%d", i),
				Type: reflect.TypeFor[string](),
				Tag:  reflect.StructTag(`json:"` + name + `"`),
			})
		}
		fields = append(fields, reflect.StructField{Name: "Value", Type: s.value.Type(), Tag: `json:",embed"`})
		cached, _ = inlinedTypes.LoadOrStore(key, reflect.StructOf(fields))
	}
	wrapper := reflect.New(cached.(reflect.Type)).Elem()
	for i, value := range s.values {
		wrapper.Field(i).SetString(value)
	}
	wrapper.Field(len(s.values)).Set(s.value)
	return wrapper.Addr().Interface()
}

// stampBlock stamps a block in a concrete field as the registry stamps it: its
// kind under statement_type, a Type refined past it under statement_refinement.
func stampBlock(typ *StatementType, kind StatementType) (StatementType, error) {
	carried := *typ
	*typ = kind
	if carried == "" || carried == kind {
		return "", nil
	}
	if err := StatementMarshaler.checkRefinement(string(kind), string(carried)); err != nil {
		return "", err
	}
	return carried, nil
}

func (b BlockStmt) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias BlockStmt
	shadow := alias(b)
	refinement, err := stampBlock(&shadow.Type, b.GetStatementType())
	if err != nil {
		return fmt.Errorf("%s: %w", b.GetStatementType(), err)
	}
	return jsonv2.MarshalEncode(enc, struct {
		alias
		Refinement StatementType  `json:"statement_refinement,omitempty"`
		Children   statementSlots `json:"children,omitempty"`
	}{shadow, refinement, statementSlots(b.Children)})
}

func (t TestStmt) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias BlockStmt
	shadow := alias(t.BlockStmt)
	refinement, err := stampBlock(&shadow.Type, t.GetStatementType())
	if err != nil {
		return fmt.Errorf("%s: %w", t.GetStatementType(), err)
	}
	return jsonv2.MarshalEncode(enc, struct {
		alias
		Name       string         `json:"name,omitempty"`
		Refinement StatementType  `json:"statement_refinement,omitempty"`
		Children   statementSlots `json:"children,omitempty"`
	}{shadow, t.Name, refinement, statementSlots(t.Children)})
}

func (s MethodCallStmt) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias MethodCallStmt
	return jsonv2.MarshalEncode(enc, struct {
		alias
		Method *nodeSlot `json:"Method,omitempty"`
	}{alias(s), slotOf(s.Method)})
}

func (s DispatchCallStmt) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias DispatchCallStmt
	for i, candidate := range s.Candidates {
		if candidate == nil {
			return fmt.Errorf("DispatchCallStmt.Candidates[%d] is nil", i)
		}
	}
	return jsonv2.MarshalEncode(enc, struct {
		alias
		Method     *nodeSlot `json:"Method,omitempty"`
		Candidates nodeSlots `json:"candidates,omitempty"`
	}{alias(s), slotOf(s.Method), nodeSlots(s.Candidates)})
}

func (s EndpointCallStmt) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias EndpointCallStmt
	return jsonv2.MarshalEncode(enc, struct {
		alias
		Endpoint *nodeSlot `json:"endpoint,omitempty"`
	}{alias(s), slotOf(s.Endpoint)})
}

func (s RecordReadStmt) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias RecordReadStmt
	return jsonv2.MarshalEncode(enc, struct {
		alias
		Record *nodeSlot `json:"Record,omitempty"`
	}{alias(s), slotOf(s.Record)})
}

func (s RecordWriteStmt) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias RecordWriteStmt
	return jsonv2.MarshalEncode(enc, struct {
		alias
		Record *nodeSlot `json:"Record,omitempty"`
	}{alias(s), slotOf(s.Record)})
}

func (r UIRRelationship) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias UIRRelationship
	return jsonv2.MarshalEncode(enc, struct {
		alias
		From *nodeSlot `json:"from,omitempty"`
		To   *nodeSlot `json:"to,omitempty"`
	}{alias(r), slotOf(r.GetFrom()), slotOf(r.To)})
}
