package uir

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// Factory constructs a zero value of a registered concrete variant.
type Factory[T any] func() T

// Registry resolves a polymorphic JSON document to a concrete type using a
// discriminator field — "node_kind" for Node, "statement_type" for Statement —
// and stamps that discriminator when encoding. A kind names one concrete Go type,
// so it is looked up by type on the way out and by kind on the way in.
//
// A registry may also accept refinements (see acceptRefinements): a value whose
// discriminator field carries a kind finer than its registered one.
type Registry[T any] struct {
	field string
	m     map[string]Factory[T]
	kinds map[reflect.Type]string
	// refinementField carries a refined kind; empty when the registry takes none.
	refinementField string
	// crossHierarchy lists, per registered kind, the refinements it accepts from
	// outside its own ':'-hierarchy.
	crossHierarchy map[string][]string
}

func NewRegistry[T any](fieldName string) *Registry[T] {
	return &Registry[T]{field: fieldName, m: map[string]Factory[T]{}, kinds: map[reflect.Type]string{}}
}

// Register binds kind to the concrete type ctor builds. Registering a kind or a
// type twice would make one of them undecodable, so it panics.
func (r *Registry[T]) Register(kind string, ctor Factory[T]) {
	t := concreteType(ctor())
	if kind == "" {
		panic(fmt.Sprintf("%s: %s registered with an empty kind", r.field, t))
	}
	if _, dup := r.m[kind]; dup {
		panic(fmt.Sprintf("%s: kind %q registered twice", r.field, kind))
	}
	if prev, dup := r.kinds[t]; dup {
		panic(fmt.Sprintf("%s: %s registered as both %q and %q", r.field, t, prev, kind))
	}
	r.m[kind] = ctor
	r.kinds[t] = kind
}

// KindOf returns the kind v's concrete type is registered under; a value and a
// pointer to it share one kind.
func (r *Registry[T]) KindOf(v T) (string, error) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return "", fmt.Errorf("%s: a nil value has no kind", r.field)
	}
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return "", fmt.Errorf("%s: a nil %s has no kind", r.field, rv.Type())
	}
	kind, ok := r.kinds[concreteType(v)]
	if !ok {
		return "", fmt.Errorf("%s: %T is not registered", r.field, v)
	}
	return kind, nil
}

// Marshal encodes v with its kind stamped under the discriminator field.
func (r *Registry[T]) Marshal(v T) ([]byte, error) {
	kind, err := r.KindOf(v)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if err := r.stamp(fields, kind); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

// stamp writes kind under the discriminator field. A document that already
// carries a different kind there — a statement whose Type was refined past its
// kind — keeps that value under the refinement field when it refines kind, and is
// refused otherwise, since overwriting it would lose it silently.
func (r *Registry[T]) stamp(fields map[string]json.RawMessage, kind string) error {
	if existing, ok := fields[r.field]; ok {
		var carried string
		if err := json.Unmarshal(existing, &carried); err != nil {
			return fmt.Errorf("%s: %w", r.field, err)
		}
		if carried != "" && carried != kind {
			if err := r.checkRefinement(kind, carried); err != nil {
				return err
			}
			fields[r.refinementField] = existing
		}
	}
	encoded, err := json.Marshal(kind)
	if err != nil {
		return err
	}
	fields[r.field] = encoded
	return nil
}

// UnmarshalByType decodes a document into the concrete type its discriminator
// names.
func (r *Registry[T]) UnmarshalByType(b []byte) (T, error) {
	var zero T
	peek := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &peek); err != nil {
		return zero, err
	}
	tf, ok := peek[r.field]
	if !ok {
		return zero, fmt.Errorf("missing %q", r.field)
	}
	var kind string
	if err := json.Unmarshal(tf, &kind); err != nil {
		return zero, fmt.Errorf("%s: %w", r.field, err)
	}
	return r.decodeFields(kind, peek, b)
}

// Decode decodes data into a new value of the type registered under kind.
func (r *Registry[T]) Decode(kind string, data []byte) (T, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		var zero T
		return zero, fmt.Errorf("%s %q: %w", r.field, kind, err)
	}
	return r.decodeFields(kind, fields, data)
}

// decodeFields decodes data, already parsed into fields, as kind. A refinement is
// moved back under the discriminator field first, which is where the value's own
// decoder reads its Type from.
func (r *Registry[T]) decodeFields(kind string, fields map[string]json.RawMessage, data []byte) (T, error) {
	var zero T
	ctor, ok := r.m[kind]
	if !ok {
		return zero, fmt.Errorf("unknown %s %q", r.field, kind)
	}
	if refined, ok := fields[r.refinementField]; ok && r.refinementField != "" {
		var refinement string
		if err := json.Unmarshal(refined, &refinement); err != nil {
			return zero, fmt.Errorf("%s: %w", r.refinementField, err)
		}
		if err := r.checkRefinement(kind, refinement); err != nil {
			return zero, err
		}
		fields[r.field] = refined
		delete(fields, r.refinementField)
		var err error
		if data, err = json.Marshal(fields); err != nil {
			return zero, err
		}
	}
	target := ctor()
	if err := json.Unmarshal(data, any(target)); err != nil {
		return zero, fmt.Errorf("%s %q: %w", r.field, kind, err)
	}
	return target, nil
}

// acceptRefinements lets a value's discriminator field carry a kind refined past
// its registered one. The refinement travels under field, so the discriminator is
// always the registered kind. A kind accepts the refinements below it in its own
// ':'-hierarchy — call:package refines call, as long as no finer registered kind
// claims it — and those crossHierarchy lists for it.
func (r *Registry[T]) acceptRefinements(field string, crossHierarchy map[string][]string) {
	if field == "" || field == r.field {
		panic(fmt.Sprintf("%s: refinements need a field of their own, not %q", r.field, field))
	}
	for kind := range crossHierarchy {
		if _, ok := r.m[kind]; !ok {
			panic(fmt.Sprintf("%s: refinements declared for unregistered kind %q", r.field, kind))
		}
	}
	r.refinementField = field
	r.crossHierarchy = crossHierarchy
}

// RefinementField is the field a refined kind travels under, or empty when the
// registry accepts no refinements.
func (r *Registry[T]) RefinementField() string {
	return r.refinementField
}

// checkRefinement reports an error unless refined may refine kind: it resolves to
// kind through its longest registered ':'-prefix, or kind accepts it from outside
// its hierarchy.
func (r *Registry[T]) checkRefinement(kind, refined string) error {
	if r.refinementField == "" {
		return fmt.Errorf("%s %q is not %q, and the %s registry accepts no refinements", r.field, refined, kind, r.field)
	}
	if slices.Contains(r.crossHierarchy[kind], refined) {
		return nil
	}
	resolved := r.longestRegisteredPrefix(refined)
	if resolved == kind {
		return nil
	}
	if resolved == "" {
		return fmt.Errorf("%s %q does not refine %q: no registered kind is a prefix of it", r.refinementField, refined, kind)
	}
	return fmt.Errorf("%s %q does not refine %q: it resolves to %q", r.refinementField, refined, kind, resolved)
}

// longestRegisteredPrefix returns the registered kind that kind is, or refines by
// ':'-separated suffixes, or empty when there is none.
func (r *Registry[T]) longestRegisteredPrefix(kind string) string {
	for {
		if _, ok := r.m[kind]; ok {
			return kind
		}
		i := strings.LastIndex(kind, ":")
		if i < 0 {
			return ""
		}
		kind = kind[:i]
	}
}

func concreteType(v any) reflect.Type {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
