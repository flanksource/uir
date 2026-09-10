// Package schemagen derives schema/uir.schema.json from the Go types that
// define the model, so the two cannot drift.
//
// Shape comes from reflection over the same fields encoding/json marshals;
// descriptions and enum values come from the package source, because doc
// comments are invisible to reflection and most enum constants are computed
// expressions rather than literals. The node and statement unions are driven by
// the uir.Nodes and uir.Statements registries, so a newly registered type
// appears in the schema without anyone editing a list.
package schemagen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/flanksource/uir"
	"github.com/google/uuid"
)

const (
	packageName = "uir"
	dialect     = "https://json-schema.org/draft/2020-12/schema"

	// nodeUnion and statementUnion name the two polymorphic $defs. Both are Go
	// interfaces, so neither can collide with a generated struct definition.
	nodeUnion      = "Node"
	statementUnion = "Statement"

	// The fields uir's registries key on when reading a polymorphic document.
	nodeDiscriminator      = "node_type"
	statementDiscriminator = "statement_type"
)

// Schema is the subset of JSON Schema 2020-12 the model needs. Field order is
// the emitted key order; maps marshal sorted, so output is deterministic.
type Schema struct {
	Schema               string             `json:"$schema,omitempty"`
	Title                string             `json:"title,omitempty"`
	Description          string             `json:"description,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Ref                  string             `json:"$ref,omitempty"`
	Const                string             `json:"const,omitempty"`
	Enum                 []string           `json:"enum,omitempty"`
	OneOf                []*Schema          `json:"oneOf,omitempty"`
	AnyOf                []*Schema          `json:"anyOf,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	AdditionalProperties any                `json:"additionalProperties,omitempty"`
	Defs                 map[string]*Schema `json:"$defs,omitempty"`
}

var (
	timeType      = reflect.TypeOf(time.Time{})
	uuidType      = reflect.TypeOf(uuid.UUID{})
	nodeType      = reflect.TypeOf((*uir.Node)(nil)).Elem()
	statementType = reflect.TypeOf((*uir.Statement)(nil)).Elem()
	marshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
)

// Generate returns the schema for the uir package whose source lives in dir.
func Generate(dir string) ([]byte, error) {
	root := reflect.TypeOf(uir.UIR{})
	src, err := loadSource(dir, root.PkgPath())
	if err != nil {
		return nil, err
	}

	g := &generator{src: src, pkgPath: root.PkgPath(), defs: map[string]*Schema{}}
	doc, err := g.build(root)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encoding schema: %w", err)
	}
	return out.Bytes(), nil
}

type generator struct {
	src     *sourceFacts
	pkgPath string
	defs    map[string]*Schema
}

// build describes both serialized forms of a document: the UIR struct, and the
// flat array of polymorphic nodes that uir.UnmarshalJSON reads.
func (g *generator) build(root reflect.Type) (*Schema, error) {
	rootRef, err := g.define(root)
	if err != nil {
		return nil, err
	}

	unions := []struct {
		name    string
		key     string
		members []unionMember
	}{
		{nodeUnion, nodeDiscriminator, membersOf(uir.Nodes, func(n uir.Node) string { return string(n.GetType()) })},
		{statementUnion, statementDiscriminator, membersOf(uir.Statements, func(s uir.Statement) string { return string(s.GetStatementType()) })},
	}
	for _, u := range unions {
		union, err := g.union(u.key, u.members)
		if err != nil {
			return nil, fmt.Errorf("%s union: %w", u.name, err)
		}
		g.defs[u.name] = union
	}

	return &Schema{
		Schema:      dialect,
		Title:       "UIR",
		Description: g.src.typeDocs[root.Name()],
		OneOf:       []*Schema{rootRef, {Type: "array", Items: ref(nodeUnion)}},
		Defs:        g.defs,
	}, nil
}

// unionMember pairs a registered type with the discriminator value the
// marshaler stamps on it.
type unionMember struct {
	typ   reflect.Type
	value string
}

// union defines every member of a registry and returns the choice over them, so
// that registering a node or statement is the only step needed to describe it.
//
// Each member's discriminator is pinned to its own value. Without that every
// branch would accept every kind, since the field's type is the shared enum.
//
// The choice is anyOf rather than oneOf because the discriminator is optional:
// a node nested in a UIR document routinely omits node_type, and since every
// other property is optional too, such a node matches several branches at once.
// oneOf demands exactly one match and would reject it — anyOf says what is
// actually true, that the node is shaped like one of the registered kinds.
func (g *generator) union(key string, members []unionMember) (*Schema, error) {
	refs := make([]*Schema, 0, len(members))
	for _, member := range members {
		r, err := g.define(member.typ)
		if err != nil {
			return nil, err
		}
		def := g.defs[member.typ.Name()]
		declared, ok := def.Properties[key]
		if !ok {
			return nil, fmt.Errorf("%s is registered under %q but has no such field to carry it", member.typ.Name(), key)
		}
		if member.value == "" {
			return nil, fmt.Errorf("%s is registered with an empty %s", member.typ.Name(), key)
		}
		def.Properties[key] = &Schema{Description: declared.Description, Type: "string", Const: member.value}
		refs = append(refs, r)
	}
	return &Schema{AnyOf: refs}, nil
}

// define registers t under its own name and returns a reference to it. The
// placeholder goes in before the fields are walked, because the model is deeply
// recursive — a TypedNode holds TypedNodes.
func (g *generator) define(t reflect.Type) (*Schema, error) {
	if t.PkgPath() != g.pkgPath {
		return nil, fmt.Errorf("%s is defined outside %s; add an explicit mapping to schemaFor", t, g.pkgPath)
	}

	name := t.Name()
	if _, done := g.defs[name]; done {
		return ref(name), nil
	}
	def := &Schema{}
	g.defs[name] = def

	object, err := g.object(t)
	if err != nil {
		return nil, err
	}
	*def = *object
	def.Description = g.src.typeDocs[name]
	return ref(name), nil
}

// object describes a struct as a closed set of properties: additionalProperties
// is false, so a field the generator fails to describe makes real documents
// invalid rather than passing silently.
func (g *generator) object(t reflect.Type) (*Schema, error) {
	properties := map[string]*Schema{}
	for _, f := range jsonFields(t) {
		property, err := g.schemaFor(f.Type)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", t.Name(), f.GoName, err)
		}
		switch {
		case f.Nullable():
			// Without omitempty a nil pointer, interface, slice or map is written as
			// null, which no $ref to an object definition would accept.
			property = &Schema{AnyOf: []*Schema{property, {Type: "null"}}}
		case g.emitsZero(f):
			property = &Schema{AnyOf: []*Schema{property, {Type: "string", Enum: []string{""}}}}
		}
		if doc := g.src.fieldDoc(f.Owner, f.GoName); doc != "" {
			property.Description = doc
		}
		properties[f.Name] = property
	}

	schema := &Schema{Type: "object", AdditionalProperties: false}
	if len(properties) > 0 {
		schema.Properties = properties
	}
	return schema, nil
}

func (g *generator) schemaFor(t reflect.Type) (*Schema, error) {
	t = elem(t)
	switch t {
	case timeType:
		return &Schema{Type: "string", Format: "date-time"}, nil
	case uuidType:
		return &Schema{Type: "string", Format: "uuid"}, nil
	}

	if t.Kind() == reflect.Interface {
		switch t {
		case nodeType:
			return ref(nodeUnion), nil
		case statementType:
			return ref(statementUnion), nil
		default:
			// any, api.Textable: the shape is not knowable from the Go type.
			return &Schema{}, nil
		}
	}
	if err := g.checkMarshaler(t); err != nil {
		return nil, err
	}

	switch t.Kind() {
	case reflect.String:
		return g.stringSchema(t), nil
	case reflect.Bool:
		return &Schema{Type: "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Schema{Type: "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return &Schema{Type: "number"}, nil
	case reflect.Slice, reflect.Array:
		items, err := g.schemaFor(t.Elem())
		if err != nil {
			return nil, err
		}
		return &Schema{Type: "array", Items: items}, nil
	case reflect.Map:
		return g.mapSchema(t)
	case reflect.Struct:
		if t.Name() == "" {
			return g.object(t)
		}
		return g.define(t)
	default:
		return nil, fmt.Errorf("no JSON Schema mapping for %s (kind %s)", t, t.Kind())
	}
}

// emitsZero reports an enum field that is written even when unset, and so can
// carry Go's zero value — the empty string, which is not one of the constants
// unless the type happens to declare it the way Visibility declares VisibilityNA.
func (g *generator) emitsZero(f jsonField) bool {
	t := elem(f.Type)
	if f.OmitEmpty || t.Kind() != reflect.String || t.PkgPath() != g.pkgPath {
		return false
	}
	enum := g.src.enums[t.Name()]
	return len(enum) > 0 && !slices.Contains(enum, "")
}

func (g *generator) mapSchema(t reflect.Type) (*Schema, error) {
	if t.Key().Kind() != reflect.String {
		return nil, fmt.Errorf("map key %s is not a string", t.Key())
	}
	values, err := g.schemaFor(t.Elem())
	if err != nil {
		return nil, err
	}
	return &Schema{Type: "object", AdditionalProperties: values}, nil
}

// stringSchema gives every named string type its own definition, carrying the
// values of its constants when it has any.
func (g *generator) stringSchema(t reflect.Type) *Schema {
	name := t.Name()
	if name == "" || t.PkgPath() != g.pkgPath {
		return &Schema{Type: "string"}
	}
	if _, done := g.defs[name]; !done {
		g.defs[name] = &Schema{
			Description: g.src.typeDocs[name],
			Type:        "string",
			Enum:        g.src.enums[name],
		}
	}
	return ref(name)
}

// checkMarshaler refuses to describe an outside type that encodes itself, since
// reflection over its fields would describe a shape it never emits — the way a
// uuid.UUID reflects as 16 integers but marshals as a string.
func (g *generator) checkMarshaler(t reflect.Type) error {
	if t.PkgPath() == "" || t.PkgPath() == g.pkgPath {
		return nil
	}
	if t.Implements(marshalerType) || reflect.PointerTo(t).Implements(marshalerType) {
		return fmt.Errorf("%s has a custom MarshalJSON, so its shape cannot be reflected; add an explicit mapping to schemaFor", t)
	}
	return nil
}

// membersOf reads the concrete type and discriminator behind each member of a
// registry — the same pair uir's own marshaler registers.
func membersOf[T any](members []T, discriminator func(T) string) []unionMember {
	out := make([]unionMember, 0, len(members))
	for i := range members {
		out = append(out, unionMember{typ: elem(reflect.TypeOf(members[i])), value: discriminator(members[i])})
	}
	return out
}

func ref(name string) *Schema {
	return &Schema{Ref: "#/$defs/" + name}
}
