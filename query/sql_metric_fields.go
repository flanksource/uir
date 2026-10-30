package query

import (
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/uir/query/sqlsyntax"
)

func (rows *sqlRows) needsMetrics() bool {
	for field := range rows.required {
		if strings.HasPrefix(field, "symbol.metrics.") {
			return true
		}
	}
	return false
}

func (rows *sqlRows) needsCallableMetrics() bool {
	return rows.required["symbol.metrics.param_count"] || rows.required["symbol.metrics.body_lines"]
}

// requiredMeasures are the producer metrics the query requests: every symbol.metrics field but the
// callable ones.
func (rows *sqlRows) requiredMeasures() []string {
	var names []string
	for _, field := range slices.Sorted(maps.Keys(rows.required)) {
		if name, found := strings.CutPrefix(field, "symbol.metrics."); found && name != "param_count" && name != "body_lines" {
			names = append(names, name)
		}
	}
	return names
}

func (rows *sqlRows) needsProperties() bool {
	for field := range rows.required {
		if strings.HasPrefix(field, "symbol.props") || strings.HasPrefix(field, "relationship.") {
			return true
		}
	}
	return false
}

func (rows *sqlRows) requirePredicateFields(predicate *sqlsyntax.Predicate) error {
	if predicate == nil {
		return nil
	}
	for _, term := range predicate.Terms {
		for _, condition := range term.Terms {
			if err := rows.requireConditionFields(condition); err != nil {
				return err
			}
		}
	}
	return nil
}

func (rows *sqlRows) requireConditionFields(condition *sqlsyntax.Condition) error {
	if condition.Not != nil {
		return rows.requireConditionFields(condition.Not)
	}
	if condition.Group != nil {
		return rows.requirePredicateFields(condition.Group)
	}
	field, _, err := rows.field(condition.Comparison.Field)
	if err != nil {
		return sqlError(condition.Pos, "%s", err)
	}
	if condition.Comparison.Requires != nil {
		_, err = rows.bindBuildRequirement(condition.Comparison, field)
	}
	return err
}
