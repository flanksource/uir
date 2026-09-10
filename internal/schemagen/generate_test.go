package schemagen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/flanksource/uir"
)

const (
	sourceDir  = "../.."
	schemaPath = "../../schema/uir.schema.json"
)

func TestSchemaIsUpToDate(t *testing.T) {
	generated, err := Generate(sourceDir)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	committed, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("reading %s: %v", schemaPath, err)
	}
	if !bytes.Equal(generated, committed) {
		t.Fatalf("schema/uir.schema.json no longer matches the Go model (%d bytes generated, %d committed); run `make schema`", len(generated), len(committed))
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	first, err := Generate(sourceDir)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	second, err := Generate(sourceDir)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("two runs of Generate disagree, so every regeneration would show up as a diff")
	}
}

// TestEveryReferenceResolves catches a dangling $ref, which makes the schema
// unusable as a whole rather than wrong in one place.
func TestEveryReferenceResolves(t *testing.T) {
	schema := load(t)

	var walk func(*Schema, string)
	walk = func(s *Schema, path string) {
		if s == nil {
			return
		}
		if s.Ref != "" {
			name, ok := defName(s.Ref)
			if !ok {
				t.Errorf("%s: %q is not a local definition reference", path, s.Ref)
			} else if _, defined := schema.Defs[name]; !defined {
				t.Errorf("%s references #/$defs/%s, which is not defined", path, name)
			}
		}
		for i, branch := range branches(s) {
			walk(branch, fmt.Sprintf("%s[%d]", path, i))
		}
		walk(s.Items, path+".items")
		if values, ok := additionalSchema(s); ok {
			walk(values, path+".additionalProperties")
		}
		for _, key := range sortedKeys(s.Properties) {
			walk(s.Properties[key], path+"."+key)
		}
	}

	walk(&Schema{OneOf: schema.OneOf}, "UIR")
	for _, name := range sortedKeys(schema.Defs) {
		walk(schema.Defs[name], name)
	}
}

// TestEveryRegisteredTypeIsDescribed guards the registries: a node or statement
// that can be unmarshaled but has no definition is a document nobody can validate.
func TestEveryRegisteredTypeIsDescribed(t *testing.T) {
	schema := load(t)

	for _, union := range []struct {
		name    string
		key     string
		members []unionMember
	}{
		{nodeUnion, nodeDiscriminator, membersOf(uir.Nodes, func(n uir.Node) string { return string(n.GetType()) })},
		{statementUnion, statementDiscriminator, membersOf(uir.Statements, func(s uir.Statement) string { return string(s.GetStatementType()) })},
	} {
		if got, want := len(branches(schema.Defs[union.name])), len(union.members); got != want {
			t.Errorf("%s union has %d members, registry has %d", union.name, got, want)
		}
		for _, member := range union.members {
			def, ok := schema.Defs[member.typ.Name()]
			if !ok {
				t.Errorf("%s is registered but has no definition", member.typ.Name())
				continue
			}
			property, ok := def.Properties[union.key]
			if !ok {
				t.Errorf("%s has no %q property, so it cannot be told apart in a %s", member.typ.Name(), union.key, union.name)
				continue
			}
			if property.Const != member.value {
				t.Errorf("%s pins %s to %q, but its marshaler stamps %q", member.typ.Name(), union.key, property.Const, member.value)
			}
		}
	}
}

// TestSchemaAcceptsMarshaledStatements checks each registered statement against
// its own definition. MarshalStatement stamps the discriminator itself rather
// than relying on the struct field, so this is the one place where the emitted
// keys can drift away from the reflected ones.
func TestSchemaAcceptsMarshaledStatements(t *testing.T) {
	schema := load(t)

	for _, stmt := range uir.Statements {
		name := elem(reflect.TypeOf(stmt)).Name()
		t.Run(name, func(t *testing.T) {
			encoded, err := uir.MarshalStatement(stmt)
			if err != nil {
				t.Fatalf("MarshalStatement: %v", err)
			}
			for _, problem := range check(decode(t, encoded), ref(name), schema.Defs, name) {
				t.Error(problem)
			}
		})
	}
}

// TestSchemaAcceptsMarshaledDocument runs a populated document — nodes, nested
// types, statement bodies and record tables — through the same check.
func TestSchemaAcceptsMarshaledDocument(t *testing.T) {
	schema := load(t)

	encoded, err := json.Marshal(document())
	if err != nil {
		t.Fatalf("marshaling document: %v", err)
	}
	problems := check(decode(t, encoded), &Schema{OneOf: schema.OneOf}, schema.Defs, "UIR")
	for _, problem := range problems {
		t.Error(problem)
	}
	if len(problems) > 0 {
		t.Logf("document:\n%s", encoded)
	}
}

// document builds a UIR that exercises the shapes most likely to be described
// wrongly: inline-embedded bases, a polymorphic statement body, a map field and
// a table alongside a record.
func document() *uir.UIR {
	method := uir.NewMethod("CreateOrder").
		WithPackage("com.example.orders").
		WithType("OrderService").
		WithParam(uir.Field("customerId", uir.RecordFieldTypeString)).
		WithReturnType(uir.TypeReference{Name: "Order"}).
		WithBody(uir.NewBlock().
			WithStatement(uir.NewMethodCall("validate").WithPositionalArg(uir.VarExpr("customerId")).Build()).
			WithStatement(uir.NewIf(uir.VarExpr("valid")).
				WithThen(uir.NewBlock().WithStatement(uir.NewReturn(uir.VarExpr("order"))).Build()).
				Build()).
			WithStatement(uir.NewReturn(uir.LitExpr("", uir.RecordFieldTypeString))).
			Build()).
		Build()

	service := uir.NewType("OrderService").
		WithPackage("com.example.orders").
		WithMethod(method).
		Build()

	doc := &uir.UIR{}
	doc.Add(uir.NewPackage("com.example.orders").
		WithType(service).
		WithFunction(method).
		Build())
	return doc
}

func load(t *testing.T) *Schema {
	t.Helper()
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("reading %s: %v", schemaPath, err)
	}
	schema := &Schema{}
	if err := json.Unmarshal(raw, schema); err != nil {
		t.Fatalf("parsing %s: %v", schemaPath, err)
	}
	return schema
}

func decode(t *testing.T, encoded []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return value
}

// check reports every key in a marshaled document that its schema does not
// declare. additionalProperties is false throughout, so an undeclared key means
// the schema rejects a document the model itself produced.
func check(value any, schema *Schema, defs map[string]*Schema, path string) []string {
	schema = deref(schema, defs)
	if schema == nil || unconstrained(schema) {
		return nil
	}
	if len(branches(schema)) > 0 {
		return checkUnion(value, schema, defs, path)
	}

	switch typed := value.(type) {
	case map[string]any:
		return checkObject(typed, schema, defs, path)
	case []any:
		var problems []string
		for i, item := range typed {
			problems = append(problems, check(item, schema.Items, defs, fmt.Sprintf("%s[%d]", path, i))...)
		}
		return problems
	default:
		return nil
	}
}

func checkObject(value map[string]any, schema *Schema, defs map[string]*Schema, path string) []string {
	nested, isMap := additionalSchema(schema)

	var problems []string
	for _, key := range sortedKeys(value) {
		child, declared := schema.Properties[key]
		switch {
		case declared:
			problems = append(problems, check(value[key], child, defs, path+"."+key)...)
		case isMap:
			problems = append(problems, check(value[key], nested, defs, path+"."+key)...)
		default:
			problems = append(problems, fmt.Sprintf("%s.%s is marshaled by the model but not declared in the schema", path, key))
		}
	}
	return problems
}

// unconstrained reports the empty schema, which accepts anything — what an
// interface-typed field such as Value.Text or a map[string]any value becomes.
func unconstrained(schema *Schema) bool {
	return schema.Ref == "" && len(branches(schema)) == 0 && schema.Properties == nil &&
		schema.Items == nil && schema.AdditionalProperties == nil
}

// checkUnion accepts the document if any branch accepts it, preferring the
// branch named by the discriminator the model stamps.
func checkUnion(value any, schema *Schema, defs map[string]*Schema, path string) []string {
	if schema == nil || len(branches(schema)) == 0 {
		return nil
	}
	if named := discriminated(value, schema, defs); named != nil {
		return check(value, named, defs, path)
	}

	var best []string
	var tried int
	for _, branch := range branches(schema) {
		// A branch describing an array says nothing useful about an object; scoring
		// it against one would bury the failure of the branch that does apply.
		if !sameKind(deref(branch, defs), value) {
			continue
		}
		problems := check(value, branch, defs, path)
		if len(problems) == 0 {
			return nil
		}
		if tried == 0 || len(problems) < len(best) {
			best = problems
		}
		tried++
	}
	if tried == 0 {
		return []string{fmt.Sprintf("%s: no branch of the union describes a %T", path, value)}
	}
	return best
}

func sameKind(schema *Schema, value any) bool {
	if schema == nil || schema.Type == "" {
		return true
	}
	switch value.(type) {
	case map[string]any:
		return schema.Type == "object"
	case []any:
		return schema.Type == "array"
	default:
		return schema.Type != "object" && schema.Type != "array"
	}
}

// discriminated finds the branch whose definition declares the type stamped on
// the document, so a mismatch is reported against the right definition.
func discriminated(value any, schema *Schema, defs map[string]*Schema) *Schema {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range []string{statementDiscriminator, nodeDiscriminator} {
		stamped, ok := object[key].(string)
		if !ok {
			continue
		}
		for _, branch := range branches(schema) {
			resolved := deref(branch, defs)
			if resolved == nil {
				continue
			}
			if property := resolved.Properties[key]; property != nil && property.Const == stamped {
				return branch
			}
		}
	}
	return nil
}

func additionalSchema(schema *Schema) (*Schema, bool) {
	raw, ok := schema.AdditionalProperties.(map[string]any)
	if !ok {
		return nil, false
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	nested := &Schema{}
	if err := json.Unmarshal(encoded, nested); err != nil {
		return nil, false
	}
	return nested, true
}

// branches is every alternative a schema offers, whichever keyword holds them.
func branches(schema *Schema) []*Schema {
	if schema == nil {
		return nil
	}
	if len(schema.AnyOf) > 0 {
		return schema.AnyOf
	}
	return schema.OneOf
}

func deref(schema *Schema, defs map[string]*Schema) *Schema {
	for schema != nil && schema.Ref != "" {
		name, ok := defName(schema.Ref)
		if !ok {
			return nil
		}
		schema = defs[name]
	}
	return schema
}

func defName(ref string) (string, bool) {
	const prefix = "#/$defs/"
	if len(ref) <= len(prefix) || ref[:len(prefix)] != prefix {
		return "", false
	}
	return ref[len(prefix):], true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
