package query

import (
	"strings"

	"github.com/flanksource/uir/query/sqlsyntax"
	"github.com/flanksource/uir/storage"
)

// SQLRequirements are the analysis mode a row query needs and what its rows depend on.
type SQLRequirements struct {
	Mode    storage.AnalysisMode
	Closure SQLClosure
}

// AnalyzeSQLRows finds the minimum analysis mode and the closure of a row query, without opening an index.
func AnalyzeSQLRows(input string) (SQLRequirements, error) {
	if err := ValidateSQLRows(input); err != nil {
		return SQLRequirements{}, err
	}
	parsed, err := sqlsyntax.Parse(input)
	if err != nil {
		return SQLRequirements{}, err
	}
	mode, err := sqlMode(parsed)
	if err != nil {
		return SQLRequirements{}, err
	}
	return SQLRequirements{Mode: mode, Closure: sqlClosure(parsed)}, nil
}

func sqlMode(parsed *sqlsyntax.Query) (storage.AnalysisMode, error) {
	mode := storage.AnalysisTyped
	file := strings.EqualFold(parsed.Source.Name, "files")
	callable := strings.HasPrefix(parsed.Source.Selector, "func:") || strings.HasPrefix(parsed.Source.Selector, "method:")
	if callable {
		selector, err := parseSelector(parsed.Source.Selector)
		if err != nil {
			return mode, err
		}
		if selector.Owner != "" || selector.ModulePattern != "" {
			return mode, nil
		}
	}
	if !file && !callable {
		return mode, nil
	}
	fields := syntaxFields()
	if file {
		fields = sqlFileFields
	}
	rows := sqlRows{fields: fields, fileSource: file}
	if err := rows.bind(parsed); err != nil {
		return mode, nil
	}
	if err := rows.requirePredicateFields(parsed.Where); err != nil {
		return mode, nil
	}
	if !syntaxPredicate(parsed.Where) {
		return mode, nil
	}
	return storage.AnalysisSyntax, nil
}

func syntaxFields() map[string]string {
	return map[string]string{
		"path": "string", "line": "number", "column": "number", "package": "string", "module": "string", "snapshot": "string", "role": "string",
		"symbol.kind": "string", "symbol.name": "string", "symbol.query_name": "string", "symbol.module": "string", "symbol.package": "string",
		"symbol.metrics.param_count": "integer", "symbol.metrics.body_lines": "integer",
	}
}

func syntaxPredicate(predicate *sqlsyntax.Predicate) bool {
	if predicate == nil {
		return true
	}
	for _, term := range predicate.Terms {
		for _, condition := range term.Terms {
			if !syntaxCondition(condition) {
				return false
			}
		}
	}
	return true
}

func syntaxCondition(condition *sqlsyntax.Condition) bool {
	if condition.Not != nil {
		return syntaxCondition(condition.Not)
	}
	if condition.Group != nil {
		return syntaxPredicate(condition.Group)
	}
	return condition.Comparison.Member == nil
}
