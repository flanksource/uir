package uir

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The codec property: every registered statement and node, filled so that every
// field it declares carries a value, decodes back to exactly what was encoded. The
// only normalisation is value-vs-pointer of a statement or node held in an
// interface slot — the registries always rebuild the pointer form.
var _ = Describe("UIR JSON codec round-trip", func() {
	DescribeTable("a fully populated statement survives MarshalStatement",
		func(prototype Statement, pointer bool) {
			expectRoundTrip(codecCase{Type: reflect.TypeOf(prototype), Pointer: pointer}, MarshalStatement, StatementMarshaler.UnmarshalByType)
		},
		codecEntries(Statements),
	)

	DescribeTable("a fully populated node survives MarshalNode",
		func(prototype Node, pointer bool) {
			expectRoundTrip(codecCase{Type: reflect.TypeOf(prototype), Pointer: pointer}, MarshalNode, UnmarshalNode)
		},
		codecEntries(Nodes),
	)

	// Every statement that names a node through the Node interface (a call's
	// method, a record read's record, an endpoint call's endpoint) is exercised
	// with each registered node kind in its own slot.
	DescribeTable("a statement survives with each node kind in its node slot",
		func(stmt Statement, kind Node, pointer bool) {
			expectRoundTrip(codecCase{Type: reflect.TypeOf(stmt), Node: kind, Pointer: pointer}, MarshalStatement, StatementMarshaler.UnmarshalByType)
		},
		nodeSlotEntries(),
	)

	// Every other case gives each block a single raw child; this block holds one of
	// every registered statement as an interface-held child.
	DescribeTable("a block holding every statement survives MarshalStatement",
		func(pointer bool) {
			expectRoundTrip(codecCase{Type: reflect.TypeFor[BlockStmt](), Pointer: pointer, EveryStatement: true}, MarshalStatement, StatementMarshaler.UnmarshalByType)
		},
		Entry("as value", false),
		Entry("as pointer", true),
	)

	// Every statement in the document — interface-held, block, or in a concrete
	// slot — carries a Type refined past its kind, which must come back exactly.
	DescribeTable("a block holding every statement refined past its kind survives MarshalStatement",
		func(pointer bool) {
			expectRoundTrip(codecCase{Type: reflect.TypeFor[BlockStmt](), Pointer: pointer, EveryStatement: true, Refinement: codecRefinement}, MarshalStatement, StatementMarshaler.UnmarshalByType)
		},
		Entry("as value", false),
		Entry("as pointer", true),
	)

	// A relationship's ends are Node slots too, and NewRelationship is how every
	// producer builds one, so an absent end must come back as absent as it was built.
	DescribeTable("a relationship built by NewRelationship survives json.Marshal",
		expectRelationshipRoundTrip,
		relationshipEntries(),
	)
})

func relationshipEntries() []TableEntry {
	entries := []TableEntry{Entry("with neither end", nil, nil)}
	for _, kind := range Nodes {
		entries = append(entries,
			Entry(fmt.Sprintf("from a %T and to nothing", kind), kind, nil),
			Entry(fmt.Sprintf("from nothing and to a %T", kind), nil, kind),
		)
	}
	return entries
}

// expectRelationshipRoundTrip is expectRoundTrip for a relationship, which is not
// itself registered: encoded through encoding/json, it must decode to its pointer
// twin, whose ends are the pointer form the node registry rebuilds.
func expectRelationshipRoundTrip(from, to Node) {
	GinkgoHelper()
	values := buildRelationship(from, to, false)
	pointers := buildRelationship(from, to, true)

	data, err := json.Marshal(values)
	Expect(err).NotTo(HaveOccurred())
	twin, err := json.Marshal(pointers)
	Expect(err).NotTo(HaveOccurred())
	Expect(string(data)).To(Equal(string(twin)), "a relationship's ends must encode alike as values and as pointers")

	var decoded UIRRelationship
	Expect(json.Unmarshal(data, &decoded)).To(Succeed(), "decoding %s", data)
	if !reflect.DeepEqual(pointers, decoded) {
		Expect(cmp.Diff(pointers, decoded, cmp.Exporter(func(reflect.Type) bool { return true }))).
			To(BeEmpty(), "encoded as %s", data)
	}
}

// buildRelationship fills every field of a relationship, then takes its ends from
// NewRelationship: a nil end stays nil, and any other is a fully populated node of
// that kind.
func buildRelationship(from, to Node, pointers bool) UIRRelationship {
	GinkgoHelper()
	f := codecCase{}.filler(pointers)
	rel := f.build(reflect.TypeFor[UIRRelationship]()).Interface().(UIRRelationship)
	expectFullyPopulated(reflect.ValueOf(rel))
	end := func(kind Node) Node {
		if kind == nil {
			return nil
		}
		return codecForm[Node](f.build(reflect.TypeOf(kind)), pointers)
	}
	built := NewRelationship(rel.RelationshipType, end(from), end(to)).Build()
	rel.From, rel.To = built.From, built.To
	return rel
}

// codecCase is one round-trip: the type to fill, the node kind for its first
// Node-typed slot (a NodeRef when unset), whether the top-level value is encoded
// through a pointer, whether its first block holds every registered statement
// rather than a single raw child, and the suffix that refines every statement's
// Type past its kind (none when empty).
type codecCase struct {
	Type           reflect.Type
	Node           Node
	Pointer        bool
	EveryStatement bool
	Refinement     string
}

// codecRefinement refines a kind by the enum's own convention — call:package
// refines call — so every kind accepts it.
const codecRefinement = ":refined"

func (c codecCase) filler(pointers bool) *codecFiller {
	node := c.Node
	if node == nil {
		node = NodeRef{}
	}
	return &codecFiller{node: node, pointers: pointers, onPath: map[reflect.Type]int{}, childrenFilled: !c.EveryStatement, refinement: c.Refinement}
}

// expectRoundTrip builds the case twice from the same deterministic filler — once
// with values in every interface slot, once with pointers — and encodes the value
// twin in the requested top-level form. Both twins must encode to the same bytes,
// and the decoded document must equal the pointer twin, since the registries
// always rebuild the pointer form: that is the whole of the value-vs-pointer
// normalisation.
func expectRoundTrip[T any](c codecCase, marshal func(T) ([]byte, error), unmarshal func([]byte) (T, error)) {
	GinkgoHelper()
	t := c.Type
	values := c.filler(false).build(t)
	pointers := c.filler(true).build(t)
	expectFullyPopulated(values)

	data, err := marshal(codecForm[T](values, c.Pointer))
	Expect(err).NotTo(HaveOccurred())
	twin, err := marshal(codecForm[T](pointers, true))
	Expect(err).NotTo(HaveOccurred())
	Expect(string(data)).To(Equal(string(twin)), "a statement or node must encode alike as a value and as a pointer")

	decoded, err := unmarshal(data)
	Expect(err).NotTo(HaveOccurred(), "decoding %s", data)
	got := reflect.ValueOf(decoded)
	Expect(got.Kind()).To(Equal(reflect.Pointer), "the registry decodes %s into its pointer form", t)
	want := pointers.Interface()
	if !reflect.DeepEqual(want, got.Elem().Interface()) {
		Expect(cmp.Diff(want, got.Elem().Interface(), cmp.Exporter(func(reflect.Type) bool { return true }))).
			To(BeEmpty(), "encoded as %s", data)
	}
}

func codecEntries[T any](prototypes []T) []TableEntry {
	var entries []TableEntry
	for _, prototype := range prototypes {
		for _, pointers := range []bool{false, true} {
			form := "value"
			if pointers {
				form = "pointer"
			}
			entries = append(entries, Entry(fmt.Sprintf("%T as %s", prototype, form), prototype, pointers))
		}
	}
	return entries
}

// nodeSlotEntries pairs every registered statement that declares a Node-typed
// field with every registered node kind.
func nodeSlotEntries() []TableEntry {
	var entries []TableEntry
	for _, stmt := range Statements {
		if !hasNodeField(reflect.TypeOf(stmt)) {
			continue
		}
		for _, kind := range Nodes {
			for _, pointer := range []bool{false, true} {
				entries = append(entries, Entry(fmt.Sprintf("%T naming a %T (pointer %t)", stmt, kind, pointer), stmt, kind, pointer))
			}
		}
	}
	return entries
}

func hasNodeField(t reflect.Type) bool {
	for _, field := range reflect.VisibleFields(t) {
		if field.Type == reflect.TypeFor[Node]() {
			return true
		}
	}
	return false
}

// codecForm returns value as T in value or pointer form.
func codecForm[T any](value reflect.Value, pointers bool) T {
	if pointers {
		ptr := reflect.New(value.Type())
		ptr.Elem().Set(value)
		return ptr.Interface().(T)
	}
	return value.Interface().(T)
}

// expectFullyPopulated guards the filler itself: a field it skipped would make the
// round-trip vacuous for that field.
func expectFullyPopulated(v reflect.Value) {
	for _, field := range reflect.VisibleFields(v.Type()) {
		if !field.IsExported() || field.Anonymous || field.Tag.Get("json") == "-" {
			continue
		}
		Expect(v.FieldByIndex(field.Index).IsZero()).To(BeFalse(), "%s.%s was not populated", v.Type(), field.Name)
	}
}

// codecFillDepth is how many times one struct type may appear on the path from
// the root; the model is recursive (an ExprStmt holds BinaryStmts holding
// ExprStmts), so the filler stops there and leaves the deeper slot empty.
const codecFillDepth = 2

// codecFiller builds values in which every exported field is non-zero, down to
// codecFillDepth, with node the prototype placed in the first Node-typed slot.
type codecFiller struct {
	node           Node
	pointers       bool
	seq            int
	onPath         map[reflect.Type]int
	childrenFilled bool
	refinement     string
}

func (f *codecFiller) build(t reflect.Type) reflect.Value {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	v := reflect.New(t).Elem()
	f.fill(v)
	return v
}

func (f *codecFiller) next() int {
	f.seq++
	return f.seq
}

func (f *codecFiller) fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString(fmt.Sprintf("s%d", f.next()))
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(f.next()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(f.next()))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(f.next()) + 0.5)
	case reflect.Array:
		for i := range v.Len() {
			f.fill(v.Index(i))
		}
	case reflect.Pointer:
		if f.exhausted(v.Type().Elem()) {
			return
		}
		ptr := reflect.New(v.Type().Elem())
		f.fill(ptr.Elem())
		v.Set(ptr)
	case reflect.Slice:
		f.fillSlice(v)
	case reflect.Map:
		f.fillMap(v)
	case reflect.Struct:
		f.fillStruct(v)
	case reflect.Interface:
		f.fillInterface(v)
	default:
		Fail(fmt.Sprintf("the codec filler cannot populate a %s", v.Type()))
	}
}

func (f *codecFiller) exhausted(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct && f.onPath[t] >= codecFillDepth
}

func (f *codecFiller) fillSlice(v reflect.Value) {
	if v.Type().Elem() == reflect.TypeFor[Statement]() {
		v.Set(reflect.ValueOf(f.children()))
		return
	}
	if f.exhausted(v.Type().Elem()) {
		return
	}
	slice := reflect.MakeSlice(v.Type(), 1, 1)
	f.fill(slice.Index(0))
	v.Set(slice)
}

func (f *codecFiller) fillMap(v reflect.Value) {
	if f.exhausted(v.Type().Elem()) {
		return
	}
	key := reflect.New(v.Type().Key()).Elem()
	f.fill(key)
	value := reflect.New(v.Type().Elem()).Elem()
	f.fill(value)
	m := reflect.MakeMap(v.Type())
	m.SetMapIndex(key, value)
	v.Set(m)
}

// children puts one of every registered statement in the first block the filler
// reaches when the case asks for it (childrenFilled starts false), and a single
// raw statement in every other block, which keeps each document small.
func (f *codecFiller) children() []Statement {
	if f.childrenFilled {
		return []Statement{codecForm[Statement](f.build(reflect.TypeFor[RawStmt]()), f.pointers)}
	}
	f.childrenFilled = true
	children := make([]Statement, 0, len(Statements))
	for _, prototype := range Statements {
		children = append(children, codecForm[Statement](f.build(reflect.TypeOf(prototype)), f.pointers))
	}
	return children
}

func (f *codecFiller) fillStruct(v reflect.Value) {
	t := v.Type()
	if t == reflect.TypeFor[time.Time]() {
		v.Set(reflect.ValueOf(time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)))
		return
	}
	defer f.stampStatementType(v)
	if f.exhausted(t) {
		return
	}
	f.onPath[t]++
	defer func() { f.onPath[t]-- }()
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Tag.Get("json") == "-" {
			continue
		}
		if !field.IsExported() && !field.Anonymous {
			Fail(fmt.Sprintf("%s.%s is unexported, so encoding/json can never round-trip it; tag it json:\"-\" or export it", t, field.Name))
		}
		f.fill(v.Field(i))
	}
}

// stampStatementType sets a statement's Type to its kind, refined by the case's
// suffix. A Type the registry cannot resolve back to the statement's own kind is
// refused by MarshalStatement, so the filler never invents one.
func (f *codecFiller) stampStatementType(v reflect.Value) {
	// A struct reached through an unexported embedding (statementBase, nodeBase)
	// is never a statement itself.
	if !v.Addr().CanInterface() {
		return
	}
	stmt, ok := v.Addr().Interface().(Statement)
	if !ok {
		return
	}
	field := v.FieldByName("Type")
	if field.IsValid() && field.Type() == reflect.TypeFor[StatementType]() {
		field.SetString(string(stmt.GetStatementType()) + f.refinement)
	}
}

func (f *codecFiller) fillInterface(v reflect.Value) {
	switch {
	case v.Type() == reflect.TypeFor[Node]():
		// The chosen kind goes in the first slot only — the statement's own, since
		// every Node field precedes the statement's nested expressions — and a
		// reference goes everywhere else, which keeps a document that nests a
		// whole package per slot from growing without bound.
		kind := f.node
		f.node = NodeRef{}
		if f.exhausted(reflect.TypeOf(kind)) {
			return
		}
		v.Set(reflect.ValueOf(codecForm[Node](f.build(reflect.TypeOf(kind)), f.pointers)))
	case v.Type().NumMethod() == 0:
		v.Set(reflect.ValueOf(fmt.Sprintf("any%d", f.next())))
	default:
		Fail(fmt.Sprintf("the codec filler has no value for interface %s", v.Type()))
	}
}
