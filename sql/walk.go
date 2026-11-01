package sql

import (
	"slices"
	"strings"

	"github.com/flanksource/sqlglot-go/sqlglot"
)

// walk visits an expression. A Table that is not a registered FROM item is a
// read on its own; a SELECT … INTO table writes it.
func (a *access) walk(e *sqlglot.Expression) error {
	if e == nil {
		return nil
	}
	switch e.Class {
	case "Select", "Union", "Intersect", "Except":
		return a.query(e)
	case "Column":
		return nil
	case "Command":
		return &UnsupportedSQLError{Class: e.Class}
	case "Anonymous":
		return a.unqualifiedCall(e)
	case "Dot":
		if fn := child(e, "expression"); fn != nil && fn.Class == "Anonymous" {
			return a.dotCall(e, fn)
		}
	case "Subquery":
		if a.sourced[e] {
			return nil
		}
	case "Table":
		if a.sourced[e] {
			return a.walkArgs(e, "this", "alias", "db", "catalog")
		}
		return a.source(e)
	case "Into":
		return a.into(e)
	}
	return a.walkArgs(e)
}

// into is a SELECT … INTO: a table it creates, or, in plpgsql, the
// variables it assigns.
func (a *access) into(e *sqlglot.Expression) error {
	if a.opts.intoVariables {
		return nil
	}
	if table := e.This(); table != nil && table.Class == "Table" {
		a.targets[table] = true
		return a.source(table)
	}
	return a.walkArgs(e)
}

func (a *access) walkArgs(e *sqlglot.Expression, skip ...string) error {
	for _, key := range e.Keys {
		if slices.Contains(skip, key) {
			continue
		}
		if err := a.walkKey(e, key); err != nil {
			return err
		}
	}
	return nil
}

func (a *access) walkKey(e *sqlglot.Expression, key string) error {
	switch v := e.Args[key].(type) {
	case *sqlglot.Expression:
		return a.walk(v)
	case []*sqlglot.Expression:
		for _, item := range v {
			if err := a.walk(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// query walks a SELECT or set operation: its CTEs, then its sources, then
// everything else it holds.
func (a *access) query(e *sqlglot.Expression) error {
	switch e.Class {
	case "Select":
		return a.selectQuery(e)
	case "Subquery":
		return a.query(e.This())
	case "Union", "Intersect", "Except":
		if err := a.with(e); err != nil {
			return err
		}
		for _, arm := range []string{"this", "expression"} {
			if err := a.query(child(e, arm)); err != nil {
				return err
			}
		}
		return a.walkArgs(e, "this", "expression", "with_")
	}
	return a.walk(e)
}

func (a *access) selectQuery(e *sqlglot.Expression) error {
	if err := a.with(e); err != nil {
		return err
	}
	if from := child(e, "from_"); from != nil {
		if err := a.source(from.This()); err != nil {
			return err
		}
	}
	for _, join := range list(e, "joins") {
		if err := a.source(join.This()); err != nil {
			return err
		}
	}
	return a.walkArgs(e, "with_")
}

// with registers each CTE of a statement by name, after walking its query,
// so a later FROM of that name resolves to the CTE and never to a table.
func (a *access) with(e *sqlglot.Expression) error {
	for _, cte := range children(child(e, "with_")) {
		if err := a.query(cte.This()); err != nil {
			return err
		}
		a.ctes[strings.ToLower(child(cte, "alias").Name())] = true
	}
	return nil
}
