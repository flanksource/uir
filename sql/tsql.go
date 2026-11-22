package sql

import (
	"fmt"

	"github.com/flanksource/sqlglot-go/sqlglot/access"

	"github.com/flanksource/uir"
)

// lowerTSQL lowers what sqlglot's access reader reads of a T-SQL file: each
// routine it defines, then its anonymous statements, across GO batches, as
// one script node.
func lowerTSQL(f *fileLowering) error {
	file, err := access.ReadTSQL(f.src)
	if err != nil {
		return err
	}
	for _, d := range file.Diagnostics {
		f.diagnose(d.Line, d.Statement, d.Message)
	}
	for _, routine := range file.Routines {
		method, err := f.routineNode(routine.Definition, routine.Line, f.tsqlStatements(routine.Body))
		if err != nil {
			f.diagnose(routine.Line, routine.Object.String(), err.Error())
			continue
		}
		f.file.Routines = append(f.file.Routines, method)
	}
	if file.Script != nil {
		f.file.Routines = append(f.file.Routines, f.scriptNode(file.Script.Line, f.tsqlStatements(file.Script.Body)))
	}
	return nil
}

func (f *fileLowering) tsqlStatements(nodes []access.Node) []uir.Statement {
	var out []uir.Statement
	for _, node := range nodes {
		out = append(out, f.tsqlNode(node)...)
	}
	return out
}

// tsqlNode is what one node does, located at its statement: a statement's
// accesses then the statements its dynamic SQL runs; an IF's or WHILE's
// condition accesses then the guard over what it governs; a TRY over its
// CATCH.
func (f *fileLowering) tsqlNode(node access.Node) []uir.Statement {
	stmts := accessStatements(node.Accesses, site{path: f.path, line: node.Line, text: node.Text})
	switch node.Kind {
	case access.StatementNode:
		return append(stmts, f.tsqlStatements(node.Dynamic)...)
	case access.IfNode:
		stmt := uir.NewIf(conditionExpr(node.Condition).Expr).WithThen(blockOf(f.tsqlStatements(node.Then)))
		if node.HasElse {
			stmt.WithElse(blockOf(f.tsqlStatements(node.Else)))
		}
		return append(stmts, stmt.Build())
	case access.WhileNode:
		return append(stmts, uir.WhileStmt{Condition: conditionExpr(node.Condition), Body: blockOf(f.tsqlStatements(node.Then))})
	case access.TryNode:
		try := uir.TryStmt{Body: blockOf(f.tsqlStatements(node.Then))}
		if node.HasCatch {
			try.Catch = blockOf(f.tsqlStatements(node.Catch))
		}
		return []uir.Statement{try}
	}
	panic(fmt.Sprintf("sql: T-SQL node kind %q on line %d is not lowered", node.Kind, node.Line))
}

func blockOf(stmts []uir.Statement) uir.BlockStmt {
	return uir.NewBlock().WithStatements(stmts...).Build()
}
