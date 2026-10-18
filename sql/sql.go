// Package sql lowers SQL source files into UIR nodes whose bodies hold the
// calls, table reads and table writes their statements make. It reads T-SQL
// scripts and T-SQL or PostgreSQL routine definitions.
//
// Positions are per statement: sqlglot-go expressions carry no offsets, so
// every call, read and write a statement makes carries the line the statement
// starts on and the statement's text.
package sql

import (
	"fmt"

	"github.com/flanksource/uir"
)

// File is what one SQL source file lowers to.
type File struct {
	// Routines holds one method per CREATE PROCEDURE or CREATE FUNCTION, in
	// file order, plus a T-SQL script method named for the file when anonymous
	// statements access data or need diagnostics. Each Body holds calls, reads
	// and writes under their guards.
	Routines []uir.MethodNode
	// Diagnostics are the statements that could not be lowered. None is ever
	// dropped silently.
	Diagnostics []Diagnostic
}

// Diagnostic is one statement that could not be lowered: the 1-based line it
// starts on in the file, its text, and why.
type Diagnostic struct {
	Line      int
	Statement string
	Message   string
}

// dialect is how one SQL dialect's files are lowered.
type dialect struct {
	name, defaultSchema string
	lower               func(f *fileLowering) error
}

var dialects = map[string]dialect{
	"tsql":     {name: "tsql", defaultSchema: "dbo", lower: lowerTSQL},
	"postgres": {name: "postgres", defaultSchema: "public", lower: lowerPostgres},
}

// LowerFile lowers the routines and anonymous T-SQL statements in src. path
// locates the source and its statements; dialect is "tsql" or "postgres".
func LowerFile(path string, src []byte, dialect string) (File, error) {
	d, ok := dialects[dialect]
	if !ok {
		return File{}, fmt.Errorf("sql: dialect %q is not lowered: use tsql or postgres", dialect)
	}
	f := &fileLowering{path: path, src: string(src), dialect: d}
	if err := d.lower(f); err != nil {
		return File{}, fmt.Errorf("sql: %s: %w", path, err)
	}
	if len(f.file.Routines) == 0 {
		return File{}, fmt.Errorf("sql: %s: no routines: the file has no CREATE PROCEDURE or CREATE FUNCTION (%d diagnostics)", path, len(f.file.Diagnostics))
	}
	return f.file, nil
}

// fileLowering is the state of one LowerFile call.
type fileLowering struct {
	path    string
	src     string
	dialect dialect
	file    File
}

func (f *fileLowering) diagnose(line int, statement, message string) {
	f.file.Diagnostics = append(f.file.Diagnostics, Diagnostic{Line: line, Statement: statement, Message: message})
}
