package query

import (
	"context"
	"errors"

	"strings"

	"github.com/flanksource/uir/query/sqlsyntax"
)

// QueryRow retains the symbol and source witness independently of the selected columns.
type QueryRow struct {
	Values      map[string]any
	Match       ModuleMatch
	Symbol      *ModuleSymbol
	Properties  map[string]any
	SymbolAlias bool
	// WitnessSymbol owns the source location, independently of projected symbols.
	WitnessSymbol *ModuleSymbol
}

// SQLRow is the row type accepted by VisitSQL.
type SQLRow = QueryRow

// ValidateSQLRows checks row-producing SQL syntax without opening an index.
func ValidateSQLRows(input string) error {
	parsed, err := sqlsyntax.Parse(input)
	if err != nil {
		return err
	}
	if parsed.Limit != nil {
		return errors.New("lint SQL must not contain LIMIT")
	}
	if strings.EqualFold(parsed.Source.Name, "graph") || strings.EqualFold(parsed.Source.Name, "path") {
		return errors.New("lint SQL must produce rows, not terminal graph/path results")
	}
	return nil
}

// VisitSQL visits every selected row, without interactive pagination.
func (pipeline *Pipeline) VisitSQL(ctx context.Context, input string, scope ModuleScopeOptions, visit func(QueryRow) error) (ModuleQueryResult, error) {
	if visit == nil {
		return ModuleQueryResult{}, errors.New("SQL row visitor is required")
	}
	if err := ValidateSQLRows(input); err != nil {
		return ModuleQueryResult{}, err
	}
	return pipeline.executeSQL(context.WithValue(ctx, ruleQueryKey{}, true), input, scope, visit)
}
