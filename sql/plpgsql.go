package sql

import (
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/sqlglot-go/sqlglot"

	"github.com/flanksource/uir"
)

// plError is a plpgsql body whose block structure does not read: an IF with
// no END IF, a loop with no END LOOP.
type plError struct{ err error }

// plParser reads a plpgsql body's block structure over its tokens and lowers
// the SQL statements it holds.
type plParser struct {
	*pgBody
	i int
}

// plpgsql lowers a plpgsql body: one block, its statements nested in the
// IF, WHILE, LOOP, FOR and EXCEPTION that hold them.
func (b *pgBody) plpgsql() (stmts []uir.Statement, err error) {
	p := &plParser{pgBody: b}
	defer func() {
		if r := recover(); r != nil {
			failure, ok := r.(plError)
			if !ok {
				panic(r)
			}
			stmts, err = nil, fmt.Errorf("plpgsql body: %w", failure.err)
		}
	}()
	stmts = p.one()
	p.skip(";")
	if p.i < len(p.text.tokens) {
		p.fail("%q after the body's END", p.text.span(p.i, len(p.text.tokens)-1))
	}
	return stmts, nil
}

func (p *plParser) fail(format string, args ...any) {
	line := p.text.firstLine
	if p.i < len(p.text.tokens) {
		line = p.text.line(p.text.tokens[p.i].Start)
	}
	panic(plError{fmt.Errorf("line %d: "+format, append([]any{line}, args...)...)})
}

// word is the keyword or bare name of token i in upper case, and "" for a
// literal, a quoted name or past the end.
func (p *plParser) word(i int) string {
	if i >= len(p.text.tokens) {
		return ""
	}
	switch token := p.text.tokens[i]; token.Type {
	case sqlglot.TokSTRING, sqlglot.TokHEREDOC_STRING, sqlglot.TokIDENTIFIER, sqlglot.TokNUMBER:
		return ""
	default:
		return strings.ToUpper(token.Text)
	}
}

func (p *plParser) at(words ...string) bool {
	return slices.Contains(words, p.word(p.i))
}

// skip consumes words in order when the next tokens are them.
func (p *plParser) skip(words ...string) bool {
	for k, word := range words {
		if p.word(p.i+k) != word {
			return false
		}
	}
	p.i += len(words)
	return true
}

func (p *plParser) expect(words ...string) {
	if !p.skip(words...) {
		p.fail("expected %s", strings.Join(words, " "))
	}
}

// until consumes the tokens up to the word at nesting depth 0 and the word
// itself, returning the range [from, to] before it.
func (p *plParser) until(word string) (int, int) {
	from, depth, cases := p.i, 0, 0
	for ; p.i < len(p.text.tokens); p.i++ {
		switch p.text.tokens[p.i].Type {
		case sqlglot.TokL_PAREN:
			depth++
		case sqlglot.TokR_PAREN:
			depth--
		}
		switch w := p.word(p.i); {
		case w == "CASE":
			cases++
		case w == "END" && cases > 0:
			cases--
		case w == word && depth == 0 && cases == 0:
			if p.i == from {
				p.fail("nothing before %s", word)
			}
			p.i++
			return from, p.i - 2
		}
	}
	p.fail("no %s", word)
	return 0, 0
}

// statements lowers statements up to the word that ends their list.
func (p *plParser) statements() []uir.Statement {
	var out []uir.Statement
	for p.i < len(p.text.tokens) && !p.at("END", "ELSIF", "ELSEIF", "ELSE", "EXCEPTION", "WHEN") {
		out = append(out, p.one()...)
	}
	return out
}

// one lowers the next statement and everything it governs.
func (p *plParser) one() []uir.Statement {
	if p.text.tokens[p.i].Type == sqlglot.TokLT && p.word(p.i+1) == "<" {
		p.i += 5 // <<label>>
	}
	switch p.word(p.i) {
	case "DECLARE", "BEGIN":
		return p.block()
	case "IF":
		p.i++
		stmts := p.ifChain()
		p.expect("END", "IF")
		p.endStatement()
		return stmts
	case "WHILE", "LOOP", "FOR", "FOREACH":
		return p.loop()
	case "CASE":
		p.caseStatement()
		return nil
	}
	return p.simple()
}

// caseStatement records a CASE … END CASE statement, which is not lowered,
// as a diagnostic.
func (p *plParser) caseStatement() {
	from := p.i
	for p.i++; p.i < len(p.text.tokens) && (p.word(p.i) != "END" || p.word(p.i+1) != "CASE"); p.i++ {
	}
	p.expect("END", "CASE")
	p.f.diagnose(p.text.line(p.text.tokens[from].Start), p.text.span(from, p.i-1), "a plpgsql CASE statement is not lowered")
	p.expect(";")
}

// endStatement consumes the label and semicolon that close a statement.
func (p *plParser) endStatement() {
	if p.i < len(p.text.tokens) && p.text.tokens[p.i].Type != sqlglot.TokSEMICOLON {
		p.i++
	}
	p.expect(";")
}

// block lowers [DECLARE …] BEGIN … [EXCEPTION WHEN … THEN …] END: the
// handlers' statements are its catch.
func (p *plParser) block() []uir.Statement {
	if p.skip("DECLARE") {
		for !p.at("BEGIN") {
			p.until(";")
		}
	}
	p.expect("BEGIN")
	body := p.statements()
	var catch []uir.Statement
	if p.skip("EXCEPTION") {
		for p.skip("WHEN") {
			p.until("THEN")
			catch = append(catch, p.statements()...)
		}
	}
	p.expect("END")
	if p.i < len(p.text.tokens) && !p.at(";") {
		p.i++ // label
	}
	if p.at(";") {
		p.i++
	}
	if catch == nil {
		return body
	}
	return []uir.Statement{uir.TryStmt{Body: blockOf(body), Catch: blockOf(catch)}}
}

// ifChain lowers IF's condition, the statements it runs and its ELSIF or
// ELSE, the IF keyword already read: an ELSIF is an IF in the ELSE, whose
// condition is read only when the conditions before it fail.
func (p *plParser) ifChain() []uir.Statement {
	from, to := p.until("THEN")
	reads, guard := p.condition(from, to)
	stmt := uir.NewIf(guard.Expr).WithThen(blockOf(p.statements()))
	switch {
	case p.skip("ELSIF"), p.skip("ELSEIF"):
		stmt.WithElse(blockOf(p.ifChain()))
	case p.skip("ELSE"):
		stmt.WithElse(blockOf(p.statements()))
	}
	return append(reads, stmt.Build())
}

// condition is what the condition [from, to] reads, and the condition.
func (p *plParser) condition(from, to int) ([]uir.Statement, uir.ConditionStmt) {
	text := p.text.span(from, to)
	at := site{path: p.f.path, line: p.text.line(p.text.tokens[from].Start), text: text}
	reads, err := lowerStatement("SELECT 1 WHERE "+text, at, p.opts)
	if err != nil {
		p.f.diagnose(at.line, text, err.Error())
	}
	return reads, conditionExpr(text)
}
