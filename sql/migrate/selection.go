package migrate

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/sqlglot-go/sqlglot"
)

func overlay(reference, target model.Schema, selections []Selection) (model.Schema, error) {
	data, err := json.Marshal(target)
	if err != nil {
		return model.Schema{}, err
	}
	var desired model.Schema
	if err := json.Unmarshal(data, &desired); err != nil {
		return desired, err
	}
	seen, whole := map[string]bool{}, map[model.Name]bool{}
	for _, s := range selections {
		if seen[s.ID()] {
			return desired, fmt.Errorf("duplicate selection %s", s.ID())
		}
		seen[s.ID()] = true
		if s.Kind == "table" || s.Kind == "view" {
			whole[s.Object] = true
		}
	}
	ordered := slices.Clone(selections)
	slices.SortFunc(ordered, func(a, b Selection) int { return strings.Compare(a.ID(), b.ID()) })
	for _, s := range ordered {
		if s.Kind == "trigger" {
			for _, schema := range []model.Schema{reference, target} {
				for _, d := range schema.Definitions {
					if d.Name == s.Object && d.Parent != nil && whole[*d.Parent] {
						return desired, fmt.Errorf("selection %s overlaps its whole parent", s.ID())
					}
				}
			}
		}
		if s.Member != "" && whole[s.Object] {
			return desired, fmt.Errorf("selection %s overlaps its whole object", s.ID())
		}
		if err := apply(&desired, reference, s); err != nil {
			return desired, fmt.Errorf("selection %s: %w", s.ID(), err)
		}
	}
	return desired, requireTableTypes(desired, reference)
}

func apply(desired *model.Schema, reference model.Schema, selection Selection) error {
	s := selection
	switch s.Kind {
	case "table":
		if s.Member != "" {
			return fmt.Errorf("whole table selection cannot name a member")
		}
		var err error
		desired.Tables, err = replace(desired.Tables, reference.Tables, s.Object, func(t model.Table) model.Name { return t.Name })
		if err != nil {
			return err
		}
		return replaceTriggers(desired, reference, s.Object)
	case "view", "procedure", "function", "trigger":
		if s.Member != "" {
			return fmt.Errorf("definition selection cannot name a member")
		}
		kind := strings.ToUpper(s.Kind)
		for _, schema := range []model.Schema{*desired, reference} {
			for _, d := range schema.Definitions {
				if d.Name == s.Object && d.Kind != kind {
					return fmt.Errorf("definition kind is %s, expected %s", d.Kind, kind)
				}
			}
		}
		var err error
		desired.Definitions, err = replace(desired.Definitions, reference.Definitions, s.Object, func(d model.Definition) model.Name { return d.Name })
		if err == nil && s.Kind == "view" {
			return replaceTriggers(desired, reference, s.Object)
		}
		return err
	case "table-type", "sequence":
		return replaceCatalogObject(desired, reference, s)
	case "column", "index", "foreign-key", "check":
		if s.Member == "" {
			return fmt.Errorf("member name is required")
		}
		at := slices.IndexFunc(desired.Tables, func(t model.Table) bool { return t.Name == s.Object })
		ref := slices.IndexFunc(reference.Tables, func(t model.Table) bool { return t.Name == s.Object })
		if at < 0 || ref < 0 {
			return fmt.Errorf("partial selection requires its table on both sides; select the whole table")
		}
		return replaceMember(&desired.Tables[at], reference.Tables[ref], s)
	default:
		return fmt.Errorf("unsupported selection kind %q", s.Kind)
	}
}

func replaceCatalogObject(desired *model.Schema, reference model.Schema, s Selection) (err error) {
	if s.Member != "" {
		return fmt.Errorf("%s selection cannot name a member", s.Kind)
	}
	if s.Kind == "sequence" {
		desired.Sequences, err = replace(desired.Sequences, reference.Sequences, s.Object, func(q model.Sequence) model.Name { return q.Name })
	} else {
		desired.TableTypes, err = replace(desired.TableTypes, reference.TableTypes, s.Object, func(t model.TableType) model.Name { return t.Name })
	}
	return err
}

// requireTableTypes fails when a desired routine takes a table-valued
// parameter whose type the desired schema lacks, naming the type to select.
// The type is never added implicitly: that would widen the selection.
func requireTableTypes(desired, reference model.Schema) error {
	for _, d := range desired.Definitions {
		if (d.Kind != "PROCEDURE" && d.Kind != "FUNCTION") || !strings.Contains(strings.ToUpper(d.SQL), "READONLY") {
			continue
		}
		types, err := tableTypeParams(d)
		if err != nil {
			return err
		}
		for _, name := range types {
			if findTableType(desired.TableTypes, name) >= 0 {
				continue
			}
			hint := "the reference lacks it too"
			if at := findTableType(reference.TableTypes, name); at >= 0 {
				hint = "select " + Selection{Kind: "table-type", Object: reference.TableTypes[at].Name}.ID()
			}
			return fmt.Errorf("%s %s.%s takes table type %s.%s, which the desired schema lacks; %s",
				strings.ToLower(d.Kind), d.Name.Schema, d.Name.Name, name.Schema, name.Name, hint)
		}
	}
	return nil
}

// tableTypeParams names the types of a routine's READONLY parameters; an
// unqualified type resolves in the routine's schema.
func tableTypeParams(d model.Definition) ([]model.Name, error) {
	script, err := sqlglot.ParseScript(d.SQL, "tsql")
	if err != nil || !script.Complete || script.Tree == nil {
		return nil, fmt.Errorf("%s %s.%s: parse definition: %v %v", strings.ToLower(d.Kind), d.Name.Schema, d.Name.Name, err, script.Diagnostics)
	}
	routine, err := sqlglot.RoutineOf(script.Tree)
	if err != nil {
		return nil, fmt.Errorf("%s %s.%s: %w", strings.ToLower(d.Kind), d.Name.Schema, d.Name.Name, err)
	}
	var out []model.Name
	for _, p := range routine.Params {
		if !p.ReadOnly {
			continue
		}
		spec, err := sqlglot.DataTypeSpecOf(p.Type, "tsql")
		if err != nil {
			return nil, fmt.Errorf("%s %s.%s parameter %s: %w", strings.ToLower(d.Kind), d.Name.Schema, d.Name.Name, p.Name, err)
		}
		parts := strings.Split(spec.Name, ".")
		for i := range parts {
			parts[i] = strings.Trim(parts[i], "[]")
		}
		name := model.Name{Schema: d.Name.Schema, Name: parts[len(parts)-1]}
		if len(parts) == 2 {
			name.Schema = parts[0]
		}
		out = append(out, name)
	}
	return out, nil
}

func findTableType(types []model.TableType, name model.Name) int {
	return slices.IndexFunc(types, func(t model.TableType) bool {
		return strings.EqualFold(t.Name.Schema, name.Schema) && strings.EqualFold(t.Name.Name, name.Name)
	})
}

func replaceMember(target *model.Table, reference model.Table, s Selection) (err error) {
	switch s.Kind {
	case "column":
		target.Columns, err = replace(target.Columns, reference.Columns, s.Member, func(c model.Column) string { return c.Name })
	case "index":
		target.Indexes, err = replace(target.Indexes, reference.Indexes, s.Member, func(i model.Index) string { return i.Name })
	case "foreign-key":
		target.ForeignKeys, err = replace(target.ForeignKeys, reference.ForeignKeys, s.Member, func(f model.ForeignKey) string { return f.Name })
	case "check":
		target.Checks, err = replace(target.Checks, reference.Checks, s.Member, func(c model.Check) string { return c.Name })
	}
	return err
}

func replace[T any, K comparable](target, reference []T, key K, name func(T) K) ([]T, error) {
	at := slices.IndexFunc(target, func(v T) bool { return name(v) == key })
	ref := slices.IndexFunc(reference, func(v T) bool { return name(v) == key })
	if at < 0 && ref < 0 {
		return nil, fmt.Errorf("unknown object or member %v", key)
	}
	if ref < 0 {
		return slices.Delete(target, at, at+1), nil
	}
	if at < 0 {
		return append(target, reference[ref]), nil
	}
	target[at] = reference[ref]
	return target, nil
}

func replaceTriggers(target *model.Schema, reference model.Schema, parent model.Name) error {
	belongs := func(d model.Definition) bool { return d.Kind == "TRIGGER" && d.Parent != nil && *d.Parent == parent }
	target.Definitions = slices.DeleteFunc(target.Definitions, belongs)
	for _, d := range reference.Definitions {
		if belongs(d) {
			target.Definitions = append(target.Definitions, d)
		}
	}
	return nil
}
