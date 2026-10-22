package migrate

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	model "github.com/flanksource/sqlglot-go/migrate"
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
	return desired, nil
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
