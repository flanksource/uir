package migrate

import (
	"fmt"
	"slices"
	"strings"

	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/sqlglot-go/migrate/engine"
	"github.com/flanksource/uir"
)

// planScope is the engine scope of a plan. A complete reference manages every
// object of the target's schemas, as the catalogue says they are all inspected.
// A declared reference manages only the objects it names, so the plan can add
// and change them but never drop anything.
func planScope(referenceCatalog, targetCatalog *uir.SQLCatalog, reference model.Schema) (engine.Scope, error) {
	if !targetCatalog.Complete {
		return engine.Scope{}, fmt.Errorf("target must be a complete SQL Server catalogue: it is the baseline the plan applies to")
	}
	if referenceCatalog.Complete {
		if err := sameScopes(referenceCatalog, targetCatalog); err != nil {
			return engine.Scope{}, err
		}
		return engine.Scope{Mode: engine.ScopeComplete, Schemas: slices.Clone(targetCatalog.Schemas)}, nil
	}
	for _, schema := range referenceCatalog.Schemas {
		if !slices.Contains(targetCatalog.Schemas, schema) {
			return engine.Scope{}, fmt.Errorf("declared reference schema %q is outside the target catalogue scopes %v", schema, targetCatalog.Schemas)
		}
	}
	return engine.Scope{Mode: engine.ScopeDeclared, Schemas: slices.Clone(referenceCatalog.Schemas), Objects: objectNames(reference)}, nil
}

func sameScopes(a, b *uir.SQLCatalog) error {
	x, y := slices.Clone(a.Schemas), slices.Clone(b.Schemas)
	slices.Sort(x)
	slices.Sort(y)
	if !slices.Equal(x, y) {
		return fmt.Errorf("reference and target catalogue scopes differ")
	}
	return nil
}

// unmanaged names the target objects a declared scope does not manage. They
// stay in the baseline so the dependents of a managed column on them are
// found, and they are context: never planned and never reported as preserved.
func unmanaged(target model.Schema, managed []model.Name) []model.Name {
	var out []model.Name
	for _, name := range objectNames(target) {
		if !slices.Contains(managed, name) {
			out = append(out, name)
		}
	}
	return out
}

// requireDeclared refuses a selection of something only the target holds: a
// declared reference never plans a drop, so it cannot ask for one.
func requireDeclared(reference model.Schema, selections []Selection) error {
	for _, s := range selections {
		if !referenceHolds(reference, s) {
			return fmt.Errorf("selection %s names something the declared reference does not declare; a declared scope never plans a drop", s.ID())
		}
	}
	return nil
}

func referenceHolds(reference model.Schema, s Selection) bool {
	switch s.Kind {
	case "table":
		return slices.ContainsFunc(reference.Tables, func(t model.Table) bool { return t.Name == s.Object })
	case "view", "procedure", "function", "trigger":
		kind := strings.ToUpper(s.Kind)
		return slices.ContainsFunc(reference.Definitions, func(d model.Definition) bool { return d.Name == s.Object && d.Kind == kind })
	case "table-type":
		return slices.ContainsFunc(reference.TableTypes, func(t model.TableType) bool { return t.Name == s.Object })
	case "sequence":
		return slices.ContainsFunc(reference.Sequences, func(q model.Sequence) bool { return q.Name == s.Object })
	}
	at := slices.IndexFunc(reference.Tables, func(t model.Table) bool { return t.Name == s.Object })
	return at >= 0 && holdsMember(reference.Tables[at], s)
}

func holdsMember(table model.Table, s Selection) bool {
	switch s.Kind {
	case "column":
		return slices.ContainsFunc(table.Columns, func(c model.Column) bool { return c.Name == s.Member })
	case "index":
		return slices.ContainsFunc(table.Indexes, func(i model.Index) bool { return i.Name == s.Member })
	case "foreign-key":
		return slices.ContainsFunc(table.ForeignKeys, func(f model.ForeignKey) bool { return f.Name == s.Member })
	case "check":
		return slices.ContainsFunc(table.Checks, func(c model.Check) bool { return c.Name == s.Member })
	}
	return false
}
