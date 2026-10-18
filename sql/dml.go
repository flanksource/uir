package sql

import (
	"strings"

	"github.com/flanksource/sqlglot-go/sqlglot"
)

// execute records EXEC / EXECUTE as a call to the stored procedure.
func (a *access) execute(e *sqlglot.Expression) error {
	ref, ok := tableRefOf(e.This())
	if !ok {
		return &UnsupportedSQLError{Class: e.Class}
	}
	a.call(ref)
	return nil
}

func (a *access) insert(e *sqlglot.Expression) error {
	if err := a.with(e); err != nil {
		return err
	}
	target := e.This()
	if target.Class == "Schema" {
		target = target.This()
	}
	a.targets[target] = true
	if err := a.source(target); err != nil {
		return err
	}
	return a.walkArgs(e, "this", "with_")
}

func (a *access) update(e *sqlglot.Expression) error {
	if err := a.with(e); err != nil {
		return err
	}
	from := child(e, "from_")
	if err := a.dmlTarget(e.This(), from.This()); err != nil {
		return err
	}
	if from != nil {
		if err := a.source(from.This()); err != nil {
			return err
		}
	}
	return a.walkArgs(e, "this", "with_")
}

func (a *access) delete(e *sqlglot.Expression) error {
	if err := a.with(e); err != nil {
		return err
	}
	from := e.This()
	if targets := list(e, "tables"); len(targets) > 0 {
		for _, target := range targets {
			if err := a.dmlTarget(target, from); err != nil {
				return err
			}
		}
	} else {
		a.targets[from] = true
	}
	if err := a.source(from); err != nil {
		return err
	}
	for _, using := range list(e, "using") {
		if err := a.source(using); err != nil {
			return err
		}
	}
	return a.walkArgs(e, "this", "tables", "using", "with_")
}

// dmlTarget marks the table an UPDATE or DELETE writes. T-SQL's `UPDATE a …
// FROM Orders a` names a FROM item by its alias, so the FROM item is the
// target; otherwise the named table is, and it is sourced here.
func (a *access) dmlTarget(target, from *sqlglot.Expression) error {
	if ref, ok := tableRefOf(target); ok && ref.schema == "" && child(target, "alias") == nil {
		for _, item := range fromItems(from) {
			if item.Class == "Table" && strings.EqualFold(aliasOf(item), ref.name) {
				a.targets[item] = true
				return nil
			}
		}
	}
	a.targets[target] = true
	return a.source(target)
}

func fromItems(from *sqlglot.Expression) []*sqlglot.Expression {
	if from == nil {
		return nil
	}
	items := []*sqlglot.Expression{from}
	for _, join := range list(from, "joins") {
		items = append(items, join.This())
	}
	return items
}

func (a *access) merge(e *sqlglot.Expression) error {
	if err := a.with(e); err != nil {
		return err
	}
	target := e.This()
	a.targets[target] = true
	for _, item := range []*sqlglot.Expression{target, child(e, "using")} {
		if err := a.source(item); err != nil {
			return err
		}
	}
	if err := a.walk(child(e, "on")); err != nil {
		return err
	}
	for _, when := range children(child(e, "whens")) {
		if err := a.mergeWhen(when); err != nil {
			return err
		}
	}
	return a.walk(child(e, "returning"))
}

// mergeWhen walks a WHEN branch's condition and the values it assigns or
// inserts; the columns it names belong to the target.
func (a *access) mergeWhen(when *sqlglot.Expression) error {
	if err := a.walk(child(when, "condition")); err != nil {
		return err
	}
	then := child(when, "then")
	switch then.Class {
	case "Update":
		for _, set := range children(then) {
			if err := a.walk(child(set, "expression")); err != nil {
				return err
			}
		}
	case "Insert":
		return a.walk(child(then, "expression"))
	}
	return nil
}
