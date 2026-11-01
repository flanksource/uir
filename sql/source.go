package sql

import (
	"strings"

	"github.com/flanksource/sqlglot-go/sqlglot"

	"github.com/flanksource/uir"
)

// tableRef is a real table or routine as written: catalog (database), schema
// and name.
type tableRef struct{ catalog, schema, name string }

// source registers a FROM item (and the joins sqlglot hangs off it),
// recording the read -- or the write, for a DML target -- of a real table.
func (a *access) source(item *sqlglot.Expression) error {
	if item == nil {
		return nil
	}
	a.sourced[item] = true
	switch item.Class {
	case "Table":
		if err := a.tableSource(item); err != nil {
			return err
		}
		for _, join := range list(item, "joins") {
			if err := a.source(join.This()); err != nil {
				return err
			}
		}
		return nil
	case "Subquery", "Lateral":
		return a.query(item.This())
	case "Values", "Unnest":
		return a.walkArgs(item, "alias")
	}
	return &UnsupportedSQLError{Class: item.Class}
}

// tableSource handles a Table FROM item: a CTE reference, a local table
// (table variable or #temp table), a table-valued function call, or a real
// table, which is the only one that is data access.
func (a *access) tableSource(item *sqlglot.Expression) error {
	this := item.This()
	switch {
	case this == nil:
		return &UnsupportedSQLError{Class: "Table"}
	case this.Class == "Parameter", isTemporary(this):
		return nil
	case this.Class == "Anonymous":
		return a.functionSource(item)
	}
	ref, ok := tableRefOf(item)
	if !ok {
		return &UnsupportedSQLError{Class: "Table(" + this.Class + ")"}
	}
	if a.ctes[strings.ToLower(ref.name)] && ref.schema == "" && ref.catalog == "" {
		return nil
	}
	mode := uir.RelationshipTypeRead
	if a.targets[item] {
		mode = uir.RelationshipTypeWrite
	}
	a.table(mode, ref)
	return nil
}

func isTemporary(name *sqlglot.Expression) bool {
	temporary, _ := name.Args["temporary"].(bool)
	return temporary || strings.HasPrefix(name.Name(), "#")
}

// functionSource records a table-valued function in FROM as a call.
func (a *access) functionSource(item *sqlglot.Expression) error {
	fn := item.This()
	a.call(tableRef{catalog: child(item, "catalog").Name(), schema: child(item, "db").Name(), name: fn.Name()})
	return a.walkArgs(fn)
}

// tableRefOf reads the name parts of a Table node whose name is an
// identifier: `t`, `s.t`, `db.s.t`, or the four-part `srv.db.s.t` sqlglot
// keeps as a Dot.
func tableRefOf(table *sqlglot.Expression) (tableRef, bool) {
	ref := tableRef{catalog: child(table, "catalog").Name(), schema: child(table, "db").Name()}
	this := table.This()
	switch {
	case this == nil:
		return ref, false
	case this.Class == "Identifier":
		ref.name = this.Name()
	case this.Class == "Dot":
		ref.catalog, ref.schema, ref.name = ref.schema, this.Name(), child(this, "expression").Name()
	default:
		return ref, false
	}
	return ref, ref.name != ""
}

// aliasOf is the name a FROM item is referred to by: its alias, or else its
// own name.
func aliasOf(item *sqlglot.Expression) string {
	if alias := child(item, "alias"); alias != nil {
		return alias.Name()
	}
	if ref, ok := tableRefOf(item); ok {
		return ref.name
	}
	return ""
}
