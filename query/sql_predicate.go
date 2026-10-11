package query

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/flanksource/uir/query/sqlsyntax"
)

type sqlTruth uint8

const (
	sqlUnknown sqlTruth = iota
	sqlFalse
	sqlTrue
)

type sqlPredicate func(sqlRecord) sqlTruth

func sqlBool(value bool) sqlTruth {
	if value {
		return sqlTrue
	}
	return sqlFalse
}
func sqlNot(value sqlTruth) sqlTruth {
	if value == sqlUnknown {
		return value
	}
	return sqlBool(value == sqlFalse)
}

func (rows *sqlRows) bindPredicate(ctx context.Context, index *compactIndex, predicate *sqlsyntax.Predicate) (sqlPredicate, error) {
	if predicate == nil {
		return nil, nil
	}
	var alternatives [][]sqlPredicate
	for _, term := range predicate.Terms {
		var conjunction []sqlPredicate
		for _, condition := range term.Terms {
			bound, err := rows.bindCondition(ctx, index, condition)
			if err != nil {
				return nil, err
			}
			conjunction = append(conjunction, bound)
		}
		alternatives = append(alternatives, conjunction)
	}
	return func(record sqlRecord) sqlTruth {
		result := sqlFalse
		for _, conjunction := range alternatives {
			value := sqlTrue
			for _, condition := range conjunction {
				part := condition(record)
				if part == sqlFalse {
					value = sqlFalse
					break
				}
				if part == sqlUnknown {
					value = sqlUnknown
				}
			}
			if value == sqlTrue {
				return sqlTrue
			}
			if value == sqlUnknown {
				result = sqlUnknown
			}
		}
		return result
	}, nil
}

func (rows *sqlRows) bindCondition(ctx context.Context, index *compactIndex, condition *sqlsyntax.Condition) (sqlPredicate, error) {
	if condition.Not != nil {
		inner, err := rows.bindCondition(ctx, index, condition.Not)
		if err != nil {
			return nil, err
		}
		return func(record sqlRecord) sqlTruth { return sqlNot(inner(record)) }, nil
	}
	if condition.Group != nil {
		return rows.bindPredicate(ctx, index, condition.Group)
	}
	comparison := condition.Comparison
	field, kind, err := rows.field(comparison.Field)
	if err != nil {
		return nil, sqlError(comparison.Pos, "%s", err)
	}
	if comparison.Null != nil {
		return func(record sqlRecord) sqlTruth {
			value := sqlBool(record.values[field] == nil)
			if comparison.Null.Not {
				return sqlNot(value)
			}
			return value
		}, nil
	}
	if comparison.Member != nil {
		return rows.bindMembership(ctx, index, comparison, field)
	}
	if comparison.Match != nil {
		if kind != "string" {
			return nil, sqlError(comparison.Pos, "MATCH requires a string field, got %s", kind)
		}
		pattern := string(comparison.Match.Pattern)
		fold := strings.EqualFold(comparison.Match.Operator, "IMATCH")
		if fold {
			pattern = sqlFold(pattern)
		}
		glob, err := compileSQLGlob(pattern)
		if err != nil {
			return nil, sqlError(comparison.Pos, "%s", err)
		}
		return func(record sqlRecord) sqlTruth {
			value := record.values[field]
			if value == nil {
				return sqlUnknown
			}
			text := value.(string)
			if fold {
				text = sqlFold(text)
			}
			matched := sqlBool(glob.matches(text))
			if comparison.Match.Not {
				return sqlNot(matched)
			}
			return matched
		}, nil
	}
	return bindSQLComparison(comparison, field, kind)
}

func (rows *sqlRows) bindMembership(ctx context.Context, index *compactIndex, comparison *sqlsyntax.Comparison, field string) (sqlPredicate, error) {
	if field != "symbol" {
		return nil, sqlError(comparison.Pos, "IN requires symbol identity on the left")
	}
	selector, err := sqlSelector(comparison.Member.Selector, index.kinds)
	if err != nil {
		return nil, sqlError(comparison.Pos, "%s", err)
	}
	value, err := index.resolveSelector(ctx, selector)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, symbol := range value.symbols {
		allowed[symbol.ID] = true
	}
	return func(record sqlRecord) sqlTruth {
		id := record.values["symbol.id"]
		if id == nil || id == "" {
			return sqlUnknown
		}
		matches := sqlBool(allowed[fmt.Sprint(id)])
		if comparison.Member.Not {
			return sqlNot(matches)
		}
		return matches
	}, nil
}

func bindSQLComparison(comparison *sqlsyntax.Comparison, field, kind string) (sqlPredicate, error) {
	value := comparison.Compare.Value.Value()
	valid := value == nil
	switch value.(type) {
	case string:
		valid = kind == "string"
	case float64:
		valid = kind == "number" || kind == "integer"
	case bool:
		valid = kind == "boolean"
	}
	if !valid {
		return nil, sqlError(comparison.Pos, "%s expects a %s literal", field, kind)
	}
	if kind == "object" || kind == "array" || kind == "symbol" {
		return nil, sqlError(comparison.Pos, "%s does not support scalar comparison", field)
	}
	return func(record sqlRecord) sqlTruth {
		left := record.values[field]
		if left == nil || value == nil {
			return sqlUnknown
		}
		compared := sqlCompareValues(left, value)
		switch comparison.Compare.Operator {
		case "=":
			return sqlBool(compared == 0)
		case "!=":
			return sqlBool(compared != 0)
		case "<":
			return sqlBool(compared < 0)
		case "<=":
			return sqlBool(compared <= 0)
		case ">":
			return sqlBool(compared > 0)
		case ">=":
			return sqlBool(compared >= 0)
		}
		panic("unbound SQL comparison")
	}, nil
}

func compileSQLGlob(pattern string) (selectorGlob, error) {
	if strings.HasSuffix(pattern, "/...") {
		pattern = strings.TrimSuffix(pattern, "...") + "**"
	}
	return compileGlob(pattern, true)
}

func sqlFold(text string) string {
	return strings.Map(func(r rune) rune {
		lowest := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < lowest {
				lowest = next
			}
		}
		return lowest
	}, text)
}
