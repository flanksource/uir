package uir

import (
	"bytes"
	"fmt"
	"strings"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// The UIR codec decodes in one pass over a jsontext.Decoder (the
// encoding/json/v2 API, go-json-experiment). Every interface-typed slot — a
// block child, a Node field — holds its value under its kind,
// {"<kind>": <value>}, so the concrete type is known before the value streams
// and no subtree is buffered or re-scanned to find its discriminator. The
// UnmarshalJSON methods delegate here, so encoding/json callers take the same
// path below the first custom-decoded value.

// slotUnmarshalers decode the interface-typed slots.
var slotUnmarshalers = jsonv2.JoinUnmarshalers(
	jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *Statement) error {
		stmt, err := decodeSlot(dec, StatementMarshaler)
		*v = stmt
		return err
	}),
	jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *Node) error {
		node, err := decodeSlot(dec, NodeMarshaler)
		*v = node
		return err
	}),
)

// Unmarshal decodes UIR JSON — a node, a statement, a slice of them, or any
// struct holding them — into v in one pass.
func Unmarshal(data []byte, v any) error {
	return jsonv2.Unmarshal(data, v, jsonv2.WithUnmarshalers(slotUnmarshalers))
}

// unmarshalDecode is the decode every UnmarshalJSONFrom delegates to. It carries
// the slot unmarshalers itself because a method can be entered without them:
// encoding/json built on encoding/json/v2 (Go 1.27, or GOEXPERIMENT=jsonv2)
// calls UnmarshalJSONFrom ahead of UnmarshalJSON, so a plain json.Unmarshal of a
// struct holding a block reaches here with no UIR options on the decoder.
func unmarshalDecode(dec *jsontext.Decoder, v any) error {
	return jsonv2.UnmarshalDecode(dec, v, jsonv2.WithUnmarshalers(slotUnmarshalers))
}

// slotKeyPrefix marks a slot's kind key. No UIR field name starts with it, so a
// slot whose first key lacks it is the legacy inline layout — what the Python and
// Java producers and every artifact written before the tagged layout carry — and
// is read through the registry instead.
const slotKeyPrefix = "$"

// SlotKey is the key a value of kind is held under in an interface-typed slot.
func SlotKey(kind string) string { return slotKeyPrefix + kind }

func readNull(dec *jsontext.Decoder) (bool, error) {
	if dec.PeekKind() != 'n' {
		return false, nil
	}
	_, err := dec.ReadToken()
	return true, err
}

// decodeSlot reads one slot value: the tagged layout in one pass, the legacy
// inline layout through the registry. JSON null is the absent value.
func decodeSlot[T any](dec *jsontext.Decoder, r *Registry[T]) (T, error) {
	var zero T
	if null, err := readNull(dec); null || err != nil {
		return zero, err
	}
	if tok, err := dec.ReadToken(); err != nil {
		return zero, err
	} else if tok.Kind() != '{' {
		return zero, fmt.Errorf("%s: want an object, got %v", r.field, tok.Kind())
	}
	if dec.PeekKind() == '}' {
		if _, err := dec.ReadToken(); err != nil {
			return zero, err
		}
		return r.UnmarshalByType([]byte("{}"))
	}
	name, err := dec.ReadToken()
	if err != nil {
		return zero, err
	}
	key := name.String()
	kind, tagged := strings.CutPrefix(key, slotKeyPrefix)
	if !tagged {
		legacy, err := readLegacySlotFrom(dec, key)
		if err != nil {
			return zero, err
		}
		return r.UnmarshalByType(legacy)
	}
	ctor, ok := r.m[kind]
	if !ok {
		return zero, fmt.Errorf("unknown %s %q", r.field, kind)
	}
	target := ctor()
	if err := jsonv2.UnmarshalDecode(dec, any(target)); err != nil {
		return zero, fmt.Errorf("%s %q: %w", r.field, kind, err)
	}
	if tok, err := dec.ReadToken(); err != nil {
		return zero, err
	} else if tok.Kind() != '}' {
		return zero, fmt.Errorf("%s %q: a slot holds exactly one kind", r.field, kind)
	}
	if err := r.checkSlotType(kind, any(target)); err != nil {
		return zero, err
	}
	return target, nil
}

// readLegacySlotFrom re-assembles a legacy inline slot whose opening brace and
// first member name the caller already read, so the registry can decode it.
func readLegacySlotFrom(dec *jsontext.Decoder, first string) ([]byte, error) {
	var buf bytes.Buffer
	enc := jsontext.NewEncoder(&buf)
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return nil, err
	}
	for name := first; ; {
		if err := enc.WriteToken(jsontext.String(name)); err != nil {
			return nil, err
		}
		value, err := dec.ReadValue()
		if err != nil {
			return nil, err
		}
		if err := enc.WriteValue(value); err != nil {
			return nil, err
		}
		tok, err := dec.ReadToken()
		if err != nil {
			return nil, err
		}
		if tok.Kind() == '}' {
			break
		}
		name = tok.String()
	}
	if err := enc.WriteToken(jsontext.EndObject); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// carriedTyped is a decoded statement whose Type field the slot reconciles with
// the slot's kind.
type carriedTyped interface {
	carriedType() StatementType
	setCarriedType(StatementType)
}

// checkSlotType gives a tagged statement its kind when it carried none, as the
// registry's stamp does, and refuses a Type that does not refine the slot's kind.
func (r *Registry[T]) checkSlotType(kind string, target any) error {
	stmt, ok := target.(carriedTyped)
	if !ok {
		return nil
	}
	carried := stmt.carriedType()
	if carried == "" {
		stmt.setCarriedType(StatementType(kind))
		return nil
	}
	if string(carried) == kind {
		return nil
	}
	return r.checkRefinement(kind, string(carried))
}

// checkBlockKind applies a block's refinement and refuses a block stamped as
// anything that does not resolve to kind.
func checkBlockKind(s *BlockStmt, kind StatementType, refinement *StatementType) error {
	if refinement != nil {
		if s.Type != kind {
			return fmt.Errorf("a %s cannot be decoded from %s %q", kind, statementTypeField, s.Type)
		}
		s.Type = *refinement
	}
	if s.Type != "" && s.Type != kind {
		if err := StatementMarshaler.checkRefinement(string(kind), string(s.Type)); err != nil {
			return fmt.Errorf("a %s cannot be decoded: %w", kind, err)
		}
	}
	return nil
}

func (s *BlockStmt) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if null, err := readNull(dec); null || err != nil {
		return err
	}
	type alias BlockStmt
	aux := struct {
		*alias
		Refinement *StatementType `json:"statement_refinement,omitempty"`
	}{alias: (*alias)(s)}
	s.Children = nil
	if err := unmarshalDecode(dec, &aux); err != nil {
		return err
	}
	return checkBlockKind(s, s.GetStatementType(), aux.Refinement)
}

// TestStmt shadows the promoted BlockStmt decoder, which knows nothing about
// Name and would refuse the test kind.
func (t *TestStmt) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if null, err := readNull(dec); null || err != nil {
		return err
	}
	type blockAlias BlockStmt
	aux := struct {
		*blockAlias
		Name       string         `json:"name,omitempty"`
		Refinement *StatementType `json:"statement_refinement,omitempty"`
	}{blockAlias: (*blockAlias)(&t.BlockStmt)}
	t.Children = nil
	if err := unmarshalDecode(dec, &aux); err != nil {
		return err
	}
	t.Name = aux.Name
	return checkBlockKind(&t.BlockStmt, t.GetStatementType(), aux.Refinement)
}

// FunctionDeclStmt shadows the promoted MethodNode decoder: its method nests
// under "function".
func (f *FunctionDeclStmt) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if null, err := readNull(dec); null || err != nil {
		return err
	}
	type baseAlias statementBase
	aux := struct {
		*baseAlias
		Function *MethodNode `json:"function"`
	}{baseAlias: (*baseAlias)(&f.statementBase), Function: &f.MethodNode}
	return unmarshalDecode(dec, &aux)
}

func (m *MethodNode) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias MethodNode
	return unmarshalDecode(dec, (*alias)(m))
}

func (s *MethodCallStmt) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias MethodCallStmt
	aux := struct {
		*alias
		Method Node `json:"Method,omitempty"`
	}{alias: (*alias)(s)}
	if err := unmarshalDecode(dec, &aux); err != nil {
		return err
	}
	s.Method = aux.Method
	return nil
}

func (s *DispatchCallStmt) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias DispatchCallStmt
	aux := struct {
		*alias
		Method     Node   `json:"Method,omitempty"`
		Candidates []Node `json:"candidates,omitempty"`
	}{alias: (*alias)(s)}
	if err := unmarshalDecode(dec, &aux); err != nil {
		return err
	}
	for i, candidate := range aux.Candidates {
		if candidate == nil {
			return fmt.Errorf("DispatchCallStmt.candidates[%d] is null", i)
		}
	}
	s.Method, s.Candidates = aux.Method, aux.Candidates
	return nil
}

func (s *EndpointCallStmt) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias EndpointCallStmt
	aux := struct {
		*alias
		Endpoint Node `json:"endpoint,omitempty"`
	}{alias: (*alias)(s)}
	if err := unmarshalDecode(dec, &aux); err != nil {
		return err
	}
	s.Endpoint = aux.Endpoint
	return nil
}

func (s *RecordReadStmt) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias RecordReadStmt
	aux := struct {
		*alias
		Record Node `json:"Record,omitempty"`
	}{alias: (*alias)(s)}
	if err := unmarshalDecode(dec, &aux); err != nil {
		return err
	}
	s.Record = aux.Record
	return nil
}

func (s *RecordWriteStmt) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias RecordWriteStmt
	aux := struct {
		*alias
		Record Node `json:"Record,omitempty"`
	}{alias: (*alias)(s)}
	if err := unmarshalDecode(dec, &aux); err != nil {
		return err
	}
	s.Record = aux.Record
	return nil
}

// UnmarshalJSONFrom leaves From nil when the relationship has no source, as
// NewRelationship builds it.
func (r *UIRRelationship) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type alias UIRRelationship
	aux := struct {
		*alias
		From Node `json:"from,omitempty"`
		To   Node `json:"to,omitempty"`
	}{alias: (*alias)(r)}
	if err := unmarshalDecode(dec, &aux); err != nil {
		return err
	}
	r.From = nil
	if aux.From != nil {
		r.From = &aux.From
	}
	r.To = aux.To
	return nil
}
