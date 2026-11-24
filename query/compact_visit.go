package query

import (
	"context"
	"errors"
)

// ParseCompactRows compiles a compact rule that produces positioned rows.
func ParseCompactRows(input string) (*Expr, error) {
	expression, err := ParseStructured(input, StructuredOptions{})
	if err != nil {
		return nil, err
	}
	return expression, validateCompactRows(expression)
}

func validateCompactRows(expression *Expr) error {
	if expression == nil {
		return nil
	}
	if expression.Kind == ExprPath {
		return errors.New("compact lint must produce rows; call paths are unsupported")
	}
	return errors.Join(validateCompactRows(expression.Left), validateCompactRows(expression.Right))
}

// VisitCompact visits every match with its occurrence target and witness.
func (pipeline *Pipeline) VisitCompact(ctx context.Context, expression *Expr, scope ModuleScopeOptions, visit func(QueryRow) error) (ModuleQueryResult, error) {
	if visit == nil {
		return ModuleQueryResult{}, errors.New("compact row visitor is required")
	}
	if err := validateCompactRows(expression); err != nil {
		return ModuleQueryResult{}, err
	}
	return pipeline.runExpr(ctx, expression, scope, visit)
}

func (index *compactIndex) visitCompactMatches(ctx context.Context, value compactValue, result *ModuleQueryResult, visit func(QueryRow) error) error {
	sortMatches(value.matches)
	records, err := index.sqlRecords(ctx, value, false)
	if err != nil {
		return err
	}
	if err := index.sqlWitnessKeys(ctx, records); err != nil {
		return err
	}
	if err := index.sqlRecordProperties(ctx, records); err != nil {
		return err
	}
	result.Total = len(records)
	for _, record := range records {
		if err := visit(record.queryRow(nil, false)); err != nil {
			return err
		}
	}
	return nil
}
