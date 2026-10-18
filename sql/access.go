package sql

import (
	"fmt"
	"strings"

	"github.com/flanksource/sqlglot-go/sqlglot"

	"github.com/flanksource/uir"
)

// UnsupportedSQLError reports a statement that parsed but whose data access
// cannot be accounted for: an opaque Command, a bare expression, or a node the
// lowering has no rule for. Class names the sqlglot node class.
type UnsupportedSQLError struct{ Class string }

func (e *UnsupportedSQLError) Error() string {
	return "unsupported SQL statement: " + e.Class
}

// statementOptions say how one statement is read.
type statementOptions struct {
	dialect, defaultSchema string
	// intoVariables reads SELECT … INTO as assigning variables, as plpgsql
	// does, rather than as creating a table.
	intoVariables bool
}

// site is where a statement is written: its file, the line it starts on and
// its text as written.
type site struct {
	path string
	line int
	text string
}

// access walks one parsed statement and records each table it reads or
// writes, once per mode, and each routine it calls. Writes come first, then
// reads and calls in the order they are met.
type access struct {
	opts    statementOptions
	at      site
	ctes    map[string]bool
	targets map[*sqlglot.Expression]bool
	sourced map[*sqlglot.Expression]bool
	seen    map[string]bool
	writes  []uir.Statement
	reads   []uir.Statement
}

// lowerStatement parses text, one statement, and returns the calls, reads
// and writes it makes, each located at at.
func lowerStatement(text string, at site, opts statementOptions) ([]uir.Statement, error) {
	root, err := sqlglot.ParseOne(text, opts.dialect)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("the parser returned no statement")
	}
	if len(root.FindAll("Command")) > 0 {
		return nil, &UnsupportedSQLError{Class: "Command"}
	}
	a := &access{opts: opts, at: at, ctes: map[string]bool{}, targets: map[*sqlglot.Expression]bool{},
		sourced: map[*sqlglot.Expression]bool{}, seen: map[string]bool{}}
	if err := a.statement(root); err != nil {
		return nil, err
	}
	return append(a.writes, a.reads...), nil
}

// statement dispatches one statement; a Block recurses into its own.
func (a *access) statement(e *sqlglot.Expression) error {
	switch e.Class {
	case "Block":
		for _, item := range children(e) {
			if item.Class == "EndStatement" {
				continue
			}
			if err := a.statement(item); err != nil {
				return err
			}
		}
		return nil
	case "Declare", "Select", "Union", "Intersect", "Except", "Subquery", "Set":
		return a.walk(e)
	case "Execute", "ExecuteSql":
		return a.execute(e)
	case "Insert":
		return a.insert(e)
	case "Update":
		return a.update(e)
	case "Delete":
		return a.delete(e)
	case "Merge":
		return a.merge(e)
	}
	return &UnsupportedSQLError{Class: e.Class}
}

func (a *access) table(mode uir.RelationshipType, ref tableRef) {
	id := uir.Identifier{Module: ref.catalog, Package: a.schema(ref), Type: ref.name, NodeType: uir.NodeTypeTable}
	key := strings.ToLower(string(mode) + "\x00" + id.String())
	if a.seen[key] {
		return
	}
	a.seen[key] = true
	if mode == uir.RelationshipTypeWrite {
		write := uir.NewRecordWrite(uir.RecordTypeTable, uir.NewRef(id)).
			WithSource(a.at.path, a.at.line, a.at.line).WithSourceCode(a.at.text).Build()
		a.writes = append(a.writes, write)
		return
	}
	read := uir.NewRecordRead(uir.RecordTypeTable, uir.NewRef(id)).
		WithSource(a.at.path, a.at.line, a.at.line).WithSourceCode(a.at.text).Build()
	a.reads = append(a.reads, *read)
}

// call records a call to a stored procedure or function.
func (a *access) call(ref tableRef) {
	id := uir.Identifier{Module: ref.catalog, Package: a.schema(ref), NodeType: uir.NodeTypeFunction}
	call := uir.NewMethodCall(ref.name, id).WithSource(a.at.path, a.at.line, a.at.line).WithSourceCode(a.at.text)
	a.reads = append(a.reads, call.MethodCallStmt)
}

func (a *access) schema(ref tableRef) string {
	if ref.schema == "" {
		return a.opts.defaultSchema
	}
	return ref.schema
}

func child(e *sqlglot.Expression, key string) *sqlglot.Expression {
	if e == nil {
		return nil
	}
	value, _ := e.Args[key].(*sqlglot.Expression)
	return value
}

func children(e *sqlglot.Expression) []*sqlglot.Expression {
	return list(e, "expressions")
}

func list(e *sqlglot.Expression, key string) []*sqlglot.Expression {
	if e == nil {
		return nil
	}
	value, _ := e.Args[key].([]*sqlglot.Expression)
	return value
}
