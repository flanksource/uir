package sql

import (
	"strings"

	"github.com/flanksource/sqlglot-go/sqlglot"
)

// typeMethods are the methods of SQL Server's xml type. `XMLData.value(…)`
// parses exactly like a schema-qualified call `dbo.fn(…)`; a method of a value
// is not a user-defined function.
var typeMethods = map[string]bool{"value": true, "query": true, "exist": true, "nodes": true, "modify": true}

// dotCall handles a Dot whose right side is a function call. A call
// qualified by one or two plain names (`dbo.fn(…)`, `db.dbo.fn(…)`) is a
// user-defined function; anything else is a method of a value, whose
// receiver and arguments are walked as ordinary expressions.
func (a *access) dotCall(dot, fn *sqlglot.Expression) error {
	parts, ok := qualifierParts(dot.This())
	if ok && len(parts) <= 2 && !typeMethods[strings.ToLower(fn.Name())] {
		ref := tableRef{schema: parts[len(parts)-1], name: fn.Name()}
		if len(parts) == 2 {
			ref.catalog = parts[0]
		}
		return a.function(ref, fn)
	}
	if err := a.walk(dot.This()); err != nil {
		return err
	}
	return a.walkArgs(fn)
}

// unqualifiedCall handles a call by a bare name. SQL Server resolves a scalar
// user-defined function only by its schema-qualified name, so in T-SQL a bare
// name is a builtin sqlglot keeps untyped (DB_NAME, ISNUMERIC, QUOTENAME, …)
// and only its arguments are walked. PostgreSQL resolves functions by bare
// name, so there it is a call.
func (a *access) unqualifiedCall(fn *sqlglot.Expression) error {
	if a.opts.dialect == "tsql" {
		return a.walkArgs(fn)
	}
	return a.function(tableRef{name: fn.Name()}, fn)
}

// qualifierParts reads `a` or `a.b` as its names; ok is false for anything
// that is not a chain of plain identifiers.
func qualifierParts(e *sqlglot.Expression) ([]string, bool) {
	switch {
	case e == nil:
		return nil, false
	case e.Class == "Identifier":
		return []string{e.Name()}, e.Name() != ""
	case e.Class == "Dot":
		left, ok := qualifierParts(e.This())
		if !ok {
			return nil, false
		}
		right, ok := qualifierParts(child(e, "expression"))
		return append(left, right...), ok
	}
	return nil, false
}

// function records a call to a function sqlglot does not model as a builtin
// (an Anonymous node), then walks its arguments for the calls they hold.
func (a *access) function(ref tableRef, fn *sqlglot.Expression) error {
	if ref.name == "" {
		return &UnsupportedSQLError{Class: "Anonymous(unnamed)"}
	}
	a.call(ref)
	return a.walkArgs(fn)
}
