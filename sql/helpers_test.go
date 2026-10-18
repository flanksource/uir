package sql_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	sqlsource "github.com/flanksource/uir/sql"
)

// edge is one relationship of a routine as these specs compare it.
type edge struct {
	Type   uir.RelationshipType
	From   string
	To     string
	Line   int
	Guards []string
}

// routineView is one lowered routine: its identifier, where it starts, its
// parameters and what its body calls, reads and writes.
type routineView struct {
	ID     string
	Line   int
	Params []string
	Edges  []edge
}

func edges(routine uir.MethodNode) []edge {
	out := []edge{}
	for _, rel := range uir.CollectRelationships(routine) {
		Expect(rel.Path).To(Equal(routine.Path), "a relationship carries the file of its routine")
		Expect(rel.StartLine).NotTo(BeNil(), "a relationship carries the line of its statement")
		out = append(out, edge{
			Type: rel.RelationshipType, From: rel.GetFrom().GetIdentifier().String(), To: rel.To.GetIdentifier().String(),
			Line: *rel.StartLine, Guards: guardText(rel.Guards),
		})
	}
	return out
}

func guardText(guards []uir.ConditionStmt) []string {
	text := []string{}
	for _, guard := range guards {
		text = append(text, guard.Pretty().String())
	}
	return text
}

func routineViews(file sqlsource.File) []routineView {
	views := []routineView{}
	for _, routine := range file.Routines {
		Expect(routine.StartLine).NotTo(BeNil(), "routine %s has a start line", routine.Method)
		params := []string{}
		for _, param := range routine.Params {
			params = append(params, param.Field+" "+param.SQLType)
		}
		views = append(views, routineView{ID: routine.GetIdentifier().String(), Line: *routine.StartLine, Params: params, Edges: edges(routine)})
	}
	return views
}

func lowerFixture(name, dialect string) sqlsource.File {
	path := filepath.Join("..", "fixtures", "sql", name)
	src, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	file, err := sqlsource.LowerFile(path, src, dialect)
	Expect(err).NotTo(HaveOccurred())
	return file
}

func lowerPostgres(src string) (sqlsource.File, error) {
	return sqlsource.LowerFile("inline.sql", []byte(src), "postgres")
}

// lowerOne lowers a source holding one routine and returns it with the
// file's diagnostics.
func lowerOne(src, dialect string) (uir.MethodNode, []sqlsource.Diagnostic) {
	file, err := sqlsource.LowerFile("inline.sql", []byte(src), dialect)
	Expect(err).NotTo(HaveOccurred())
	Expect(file.Routines).To(HaveLen(1))
	return file.Routines[0], file.Diagnostics
}

func targets(routine uir.MethodNode) []string {
	out := []string{}
	for _, e := range edges(routine) {
		out = append(out, string(e.Type)+" "+e.To)
	}
	return out
}
