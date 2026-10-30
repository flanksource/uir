package sql

import (
	"github.com/flanksource/sqlglot-go/sqlglot"

	"github.com/flanksource/uir"
)

// loop lowers WHILE cond LOOP, LOOP, and FOR or FOREACH … LOOP, each up to
// END LOOP: a WHILE's body is guarded by its condition, and the query a FOR
// iterates over is read before its body.
func (p *plParser) loop() []uir.Statement {
	keyword := p.word(p.i)
	p.i++
	var before []uir.Statement
	var guard uir.ConditionStmt
	switch keyword {
	case "WHILE":
		from, to := p.until("LOOP")
		before, guard = p.condition(from, to)
	case "FOR", "FOREACH":
		from, to := p.until("LOOP")
		before = p.iterated(from, to)
	}
	body := blockOf(p.statements())
	p.expect("END", "LOOP")
	p.endStatement()
	if keyword == "FOR" || keyword == "FOREACH" {
		return append(before, uir.ForEachStmt{Body: body})
	}
	return append(before, uir.WhileStmt{Condition: guard, Body: body})
}

// iterated reads the query a FOR header [from, to] iterates over: what
// follows its IN when that is a SELECT or WITH. A range or an array reads
// nothing; a dynamic EXECUTE cannot be read and is a diagnostic.
func (p *plParser) iterated(from, to int) []uir.Statement {
	for i := from; i < to; i++ {
		if p.word(i) == "IN" {
			return p.query(i+1, to)
		}
	}
	return nil
}

// query lowers [from, to] when it is a SELECT or WITH query; a dynamic
// EXECUTE cannot be read and is a diagnostic, anything else reads nothing.
func (p *plParser) query(from, to int) []uir.Statement {
	switch p.word(from) {
	case "SELECT", "WITH":
		return p.lower(from, to, "")
	case "EXECUTE":
		p.f.diagnose(p.text.line(p.text.tokens[from].Start), p.text.span(from, to), "dynamic SQL is not lowered")
	}
	return nil
}

// skippedStatements touch no table and call no routine the lowering can
// name: messages, jumps, transaction control and cursor moves.
var skippedStatements = map[string]bool{
	"RAISE": true, "ASSERT": true, "NULL": true, "EXIT": true, "CONTINUE": true, "GET": true,
	"COMMIT": true, "ROLLBACK": true, "CLOSE": true, "FETCH": true, "MOVE": true,
}

// simple lowers a statement up to its semicolon. PERFORM and CALL are read as
// the SELECT of what they run, RETURN QUERY as its query; a DECLARE, an
// assignment, a RETURN of a value and the skippedStatements read nothing.
func (p *plParser) simple() []uir.Statement {
	from := p.i
	_, to := p.statementEnd()
	keyword := p.word(from)
	switch {
	case skippedStatements[keyword], p.assignment(from, to):
		return nil
	case keyword == "PERFORM", keyword == "CALL":
		if from == to {
			p.fail("%s runs nothing", keyword)
		}
		return p.lower(from, to, "SELECT ")
	case keyword == "RETURN" && p.word(from+1) == "QUERY" && from+2 <= to:
		return p.query(from+2, to)
	case keyword == "RETURN":
		return nil
	case keyword == "OPEN":
		return p.iteratedCursor(from, to)
	case keyword == "EXECUTE":
		p.f.diagnose(p.text.line(p.text.tokens[from].Start), p.text.span(from, to), "dynamic SQL is not lowered")
		return nil
	}
	return p.lower(from, to, "")
}

// statementEnd consumes the statement up to and with its semicolon, or to the
// body's end, and returns the range [from, to] before it.
func (p *plParser) statementEnd() (int, int) {
	from, depth := p.i, 0
	for ; p.i < len(p.text.tokens); p.i++ {
		switch p.text.tokens[p.i].Type {
		case sqlglot.TokL_PAREN:
			depth++
		case sqlglot.TokR_PAREN:
			depth--
		case sqlglot.TokSEMICOLON:
			if depth == 0 {
				p.i++
				return from, p.i - 2
			}
		}
	}
	return from, p.i - 1
}

// assignment reports whether [from, to] is `target := value` or
// `target = value`, the target a name or a field of one.
func (p *plParser) assignment(from, to int) bool {
	for i := from; i <= to && i < from+6; i++ {
		switch p.text.tokens[i].Type {
		case sqlglot.TokCOLON_EQ, sqlglot.TokEQ:
			return i > from
		case sqlglot.TokVAR, sqlglot.TokDOT, sqlglot.TokIDENTIFIER, sqlglot.TokL_BRACKET, sqlglot.TokR_BRACKET, sqlglot.TokNUMBER:
		default:
			return false
		}
	}
	return false
}

// iteratedCursor reads the query an OPEN cursor FOR runs.
func (p *plParser) iteratedCursor(from, to int) []uir.Statement {
	for i := from; i < to; i++ {
		if p.word(i) == "FOR" {
			return p.query(i+1, to)
		}
	}
	return nil
}
