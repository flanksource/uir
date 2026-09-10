package schemagen

import (
	"reflect"
	"strings"
)

// jsonField is one key encoding/json will emit for a struct, after embedded
// structs have been flattened and name conflicts resolved.
type jsonField struct {
	Name      string
	Type      reflect.Type
	Owner     string
	GoName    string
	OmitEmpty bool
	depth     int
	tagged    bool
}

// Nullable reports whether the field can marshal as JSON null: a nil pointer,
// interface, slice or map is only dropped when the tag says omitempty.
func (f jsonField) Nullable() bool {
	if f.OmitEmpty {
		return false
	}
	switch f.Type.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map:
		return true
	default:
		return false
	}
}

// jsonFields mirrors encoding/json's own field resolution: a breadth-first walk
// in which an embedded struct contributes its fields to the enclosing object,
// shallower fields win, and a same-depth collision cancels unless exactly one
// side is tagged. Anything less faithful describes a document the model never
// produces — nodeBase alone embeds Metadata and Identifier inline while nesting
// SourceCode under its own key.
func jsonFields(t reflect.Type) []jsonField {
	var found []jsonField
	visited := map[reflect.Type]bool{}
	current := []reflect.Type{t}

	for depth := 0; len(current) > 0; depth++ {
		var next []reflect.Type
		for _, st := range current {
			if visited[st] {
				continue
			}
			visited[st] = true
			for i := 0; i < st.NumField(); i++ {
				f := st.Field(i)
				embedded := elem(f.Type)
				if f.Anonymous {
					// An unexported embedded struct still promotes its exported fields.
					if !f.IsExported() && embedded.Kind() != reflect.Struct {
						continue
					}
				} else if !f.IsExported() {
					continue
				}

				tag := f.Tag.Get("json")
				if tag == "-" {
					continue
				}
				name, options, _ := strings.Cut(tag, ",")
				if name == "" && f.Anonymous && embedded.Kind() == reflect.Struct {
					next = append(next, embedded)
					continue
				}

				field := jsonField{
					Name:      name,
					Type:      f.Type,
					Owner:     st.Name(),
					GoName:    f.Name,
					OmitEmpty: hasOption(options, "omitempty"),
					depth:     depth,
					tagged:    name != "",
				}
				if field.Name == "" {
					// A named field tagged `json:",inline"` keeps its Go name: encoding/json
					// has no inline option, so the empty tag name falls back to the field.
					field.Name = f.Name
				}
				found = append(found, field)
			}
		}
		current = next
	}

	return resolve(found)
}

// resolve keeps one field per JSON key, in first-seen order.
func resolve(found []jsonField) []jsonField {
	byName := map[string][]jsonField{}
	var order []string
	for _, f := range found {
		if _, seen := byName[f.Name]; !seen {
			order = append(order, f.Name)
		}
		byName[f.Name] = append(byName[f.Name], f)
	}

	out := make([]jsonField, 0, len(order))
	for _, name := range order {
		if f, ok := dominant(byName[name]); ok {
			out = append(out, f)
		}
	}
	return out
}

// dominant applies encoding/json's rule for competing fields: the shallowest
// wins, and a tie at that depth is broken only by a json tag — otherwise every
// candidate is dropped and the key never appears at all.
func dominant(candidates []jsonField) (jsonField, bool) {
	shallowest := candidates[0].depth
	for _, f := range candidates[1:] {
		if f.depth < shallowest {
			shallowest = f.depth
		}
	}

	var atDepth, tagged []jsonField
	for _, f := range candidates {
		if f.depth != shallowest {
			continue
		}
		atDepth = append(atDepth, f)
		if f.tagged {
			tagged = append(tagged, f)
		}
	}

	switch {
	case len(atDepth) == 1:
		return atDepth[0], true
	case len(tagged) == 1:
		return tagged[0], true
	default:
		return jsonField{}, false
	}
}

func hasOption(options, want string) bool {
	for options != "" {
		var option string
		option, options, _ = strings.Cut(options, ",")
		if option == want {
			return true
		}
	}
	return false
}

func elem(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
