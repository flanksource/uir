package uir

// TypeReference represents a type in a language-agnostic way, supporting
// generics, unions, intersections, and other composite type constructs
// needed for bidirectional TypeScript mapping.
type TypeReference struct {
	// Base type name (e.g. "Array", "Map", "Promise", "string")
	Name string `json:"name,omitempty"`
	// For primitive/simple types, maps to RecordFieldType
	FieldType RecordFieldType `json:"fieldType,omitempty"`
	// Generic type arguments (e.g. Array<string> → TypeArgs: [{Name: "string"}])
	TypeArgs []TypeReference `json:"typeArgs,omitempty"`
	// Union members (e.g. string | number → Union: [{Name: "string"}, {Name: "number"}])
	Union []TypeReference `json:"union,omitempty"`
	// Intersection members (e.g. A & B → Intersection: [{Name: "A"}, {Name: "B"}])
	Intersection []TypeReference `json:"intersection,omitempty"`
	// Whether this type is nullable (e.g. string | null, or ?: optional)
	Optional bool `json:"optional,omitempty"`
	// For literal types (e.g. type X = "hello" | 42)
	LiteralValue *string `json:"literalValue,omitempty"`
	// For array/tuple types
	IsArray bool `json:"isArray,omitempty"`
	// For tuple types with positional elements
	TupleElements []TypeReference `json:"tupleElements,omitempty"`
	// For function types (e.g. (x: number) => string)
	FunctionParams  []TypeReference `json:"functionParams,omitempty"`
	FunctionReturns *TypeReference  `json:"functionReturns,omitempty"`
	// For mapped/conditional types, store the raw expression
	RawType string `json:"rawType,omitempty"`
	// Package qualifying the type for import resolution. When set, the TS
	// generator emits `import { Name } from "Package";` and renders the bare Name.
	Package string `json:"package,omitempty"`
}

func (t TypeReference) IsEmpty() bool {
	return t.Name == "" && t.FieldType == "" && len(t.Union) == 0 &&
		len(t.Intersection) == 0 && len(t.TypeArgs) == 0 && t.RawType == ""
}

// TypeParam represents a generic type parameter declaration (e.g. T extends Comparable)
type TypeParam struct {
	Name       string         `json:"name"`
	Constraint *TypeReference `json:"constraint,omitempty"`
	Default    *TypeReference `json:"default,omitempty"`
}

// SimpleType creates a TypeReference for a simple named type
func SimpleType(name string) TypeReference {
	return TypeReference{Name: name}
}

// GenericType creates a TypeReference with type arguments
func GenericType(name string, args ...TypeReference) TypeReference {
	return TypeReference{Name: name, TypeArgs: args}
}

// UnionType creates a TypeReference representing a union of types
func UnionType(members ...TypeReference) TypeReference {
	return TypeReference{Union: members}
}

// IntersectionType creates a TypeReference representing an intersection of types
func IntersectionType(members ...TypeReference) TypeReference {
	return TypeReference{Intersection: members}
}

// OptionalType wraps a type reference as optional
func OptionalType(inner TypeReference) TypeReference {
	inner.Optional = true
	return inner
}

// ArrayType creates an array type reference
func ArrayType(element TypeReference) TypeReference {
	return TypeReference{Name: "Array", IsArray: true, TypeArgs: []TypeReference{element}}
}
