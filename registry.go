package uir

import (
	"encoding/json"
	"fmt"
)

// Factory constructs a zero value of a registered concrete variant.
type Factory[T any] func() T

// Registry resolves a polymorphic JSON document to a concrete type using a
// discriminator field, e.g. "node_type" for Node or "statement_type" for Statement.
type Registry[T any] struct {
	field string
	m     map[string]Factory[T]
}

func NewRegistry[T any](fieldName string) *Registry[T] {
	return &Registry[T]{field: fieldName, m: map[string]Factory[T]{}}
}

func (r *Registry[T]) Register(kind string, ctor Factory[T]) {
	r.m[kind] = ctor
}

func (r *Registry[T]) UnmarshalByType(b []byte) (T, error) {
	var zero T
	peek := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &peek); err != nil {
		return zero, err
	}
	return r.Unmarshal(peek)
}
func (r *Registry[T]) Unmarshal(peek map[string]json.RawMessage) (T, error) {
	var zero T

	tf, ok := peek[r.field]
	if !ok {
		return zero, fmt.Errorf("missing %q", r.field)
	}
	var kind string
	if err := json.Unmarshal(tf, &kind); err != nil {
		return zero, err
	}

	ctor, ok := r.m[kind]
	if !ok {
		return zero, fmt.Errorf("unknown %q", kind)
	}
	target := ctor()

	// Convert peek map back to JSON bytes for unmarshaling
	fullBytes, err := json.Marshal(peek)
	if err != nil {
		return zero, err
	}

	if err := json.Unmarshal(fullBytes, &target); err != nil {
		return zero, err
	}
	return target, nil
}
