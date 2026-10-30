package sql

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/flanksource/sqlglot-go/sqlglot"

	"github.com/flanksource/uir"
)

// routineNode is the method a CREATE PROCEDURE or FUNCTION makes, in the
// shape databasecatalog publishes for a catalog routine: Package is the
// schema (the dialect's default when none is written), Method the name.
func (f *fileLowering) routineNode(routine sqlglot.Routine, line int, body []uir.Statement) (uir.MethodNode, error) {
	schema := routine.Schema
	if schema == "" {
		schema = f.dialect.defaultSchema
	}
	builder := uir.NewMethod(routine.Name, uir.Identifier{Package: schema, NodeType: uir.NodeTypeFunction}).
		WithSource(f.path, line, line)
	for _, param := range routine.Params {
		sqlType, err := sqlglot.Generate(param.Type, f.dialect.name)
		if err != nil {
			return uir.MethodNode{}, fmt.Errorf("routine %s.%s: parameter %s type: %w", schema, routine.Name, param.Name, err)
		}
		field := uir.Field(param.Name, "")
		field.SQLType = sqlType
		builder.WithParam(field)
	}
	method := builder.WithBody(uir.NewBlock().WithStatements(body...).Build()).Build()
	method.Properties = map[string]any{"routine_type": strings.ToLower(routine.Kind)}
	return method, nil
}

func (f *fileLowering) scriptNode(line int, body []uir.Statement) uir.MethodNode {
	method := uir.NewMethod(filepath.Base(f.path), uir.Identifier{Signature: f.path, NodeType: uir.NodeTypeFunction}).
		WithSource(f.path, line, line).
		WithBody(uir.NewBlock().WithStatements(body...).Build()).Build()
	method.Properties = map[string]any{"routine_type": "script"}
	return method
}

// conditionExpr is a condition kept as its source text.
func conditionExpr(text string) uir.ConditionStmt {
	expr := uir.ExprStmt{}
	expr.Type, expr.Content = uir.ASTStatementTypeExpression, &text
	return uir.NewCondition(expr)
}

// lineAt is the 1-based line of offset in text.
func lineAt(text string, offset int) int {
	return strings.Count(text[:offset], "\n") + 1
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
