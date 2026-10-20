package query

import (
	"context"
	"errors"

	"strings"

	"github.com/flanksource/uir/query/sqlsyntax"
)

// SQLRow retains the source witness independently of the selected columns.
type SQLRow struct {
	Values map[string]any
	Match  ModuleMatch
}

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
func (pipeline *Pipeline) VisitSQL(ctx context.Context, input string, scope ModuleScopeOptions, visit func(SQLRow) error) (ModuleQueryResult, error) {
	if visit == nil {
		return ModuleQueryResult{}, errors.New("SQL row visitor is required")
	}
	if err := ValidateSQLRows(input); err != nil {
		return ModuleQueryResult{}, err
	}
	return pipeline.executeSQL(ctx, input, scope, visit)
}
