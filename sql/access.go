package sql

import (
	"fmt"

	"github.com/flanksource/sqlglot-go/sqlglot/access"

	"github.com/flanksource/uir"
)

// site is where a statement is written: its file, the line it starts on and
// its text as written.
type site struct {
	path string
	line int
	text string
}

// lowerStatement reads text, one statement, with sqlglot's access reader and
// returns the calls, reads and writes it makes, each located at at.
func lowerStatement(text string, at site, opts access.StatementOptions) ([]uir.Statement, error) {
	opts.Line = at.line
	accesses, err := access.ReadStatement(text, opts)
	if err != nil {
		return nil, err
	}
	return accessStatements(accesses, at), nil
}

// accessStatements lowers accesses, in order, each located at at.
func accessStatements(accesses []access.Access, at site) []uir.Statement {
	out := make([]uir.Statement, 0, len(accesses))
	for _, a := range accesses {
		out = append(out, accessStatement(a, at))
	}
	return out
}

// accessStatement is a table read or write as a record statement on a table
// identifier, and a call as a method call on a function identifier.
func accessStatement(a access.Access, at site) uir.Statement {
	table := uir.NewRef(uir.Identifier{Module: a.Object.Database, Package: a.Object.Schema, Type: a.Object.Name, NodeType: uir.NodeTypeTable})
	switch a.Kind {
	case access.Write:
		return uir.NewRecordWrite(uir.RecordTypeTable, table).WithSource(at.path, at.line, at.line).WithSourceCode(at.text).Build()
	case access.Read:
		return *uir.NewRecordRead(uir.RecordTypeTable, table).WithSource(at.path, at.line, at.line).WithSourceCode(at.text).Build()
	case access.Call:
		id := uir.Identifier{Module: a.Object.Database, Package: a.Object.Schema, NodeType: uir.NodeTypeFunction}
		return uir.NewMethodCall(a.Object.Name, id).WithSource(at.path, at.line, at.line).WithSourceCode(at.text).MethodCallStmt
	}
	panic(fmt.Sprintf("sql: access kind %q of %s is not lowered", a.Kind, a.Object))
}
