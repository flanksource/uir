package sql

import (
	"github.com/flanksource/sqlglot-go/sqlglot"
)

type plMigration struct {
	analysis *MigrationAnalysis
	origin   sqlglot.SourceRange
	depth    int
}

func (m *plMigration) read(body *pgBody, from, to int, prefix string) {
	origin := m.origin
	if origin.End == origin.Start {
		origin.Start += body.text.tokens[from].Start - len([]rune(prefix))
		origin.End = origin.Start
	}
	m.analysis.analyze(prefix+body.text.span(from, to), body.opts, origin, m.depth+1)
}

func (m *plMigration) assignment(p *plParser, from, to int) {
	for i := from + 1; i < to; i++ {
		token := p.text.tokens[i]
		if token.Type == sqlglot.TokCOLON_EQ || token.Type == sqlglot.TokEQ || p.word(i) == "DEFAULT" {
			m.read(p.pgBody, i+1, to, "SELECT ")
			return
		}
	}
}

func (m *plMigration) unsupported(body *pgBody, index int, message string) {
	token := body.text.tokens[index]
	m.analysis.coverage(migrationSpan(m.origin, sqlglot.SourceRange{Start: token.Start, End: token.End + 1}), message)
}

func (m *plMigration) execute(body *pgBody, from, to int) {
	if from < to {
		token := body.text.tokens[from+1]
		if from+1 == to && (token.Type == sqlglot.TokSTRING || token.Type == sqlglot.TokHEREDOC_STRING) {
			at := migrationSpan(m.origin, sqlglot.SourceRange{Start: token.Start, End: token.End + 1})
			m.analysis.analyze(token.Text, body.opts, at, m.depth+1)
			return
		}
	}
	m.unsupported(body, from, "runtime-generated PL/pgSQL SQL cannot be inspected")
}
