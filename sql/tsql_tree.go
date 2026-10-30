package sql

import (
	"strings"

	"github.com/flanksource/uir"
)

// tsqlBody lowers one routine body: its statements, nested in the IF, WHILE,
// BEGIN … END and TRY … CATCH that hold them.
type tsqlBody struct {
	f         *fileLowering
	text      string
	firstLine int
	path      string
	opts      statementOptions
	entries   []tsqlEntry
	next      int
}

// tsqlEntry is one statement or control-flow marker of a body, in order.
type tsqlEntry struct {
	kind      segmentKind
	statement tsqlStatement
	line      int
}

func (b *tsqlBody) lower(start int) []uir.Statement {
	b.path = b.f.path
	for _, seg := range splitSegments(b.text, start) {
		if seg.kind != segmentStatements {
			b.entries = append(b.entries, tsqlEntry{kind: seg.kind, line: b.firstLine + lineAt(b.text, seg.start) - 1})
			continue
		}
		for _, statement := range b.runStatements(seg.start, seg.end) {
			b.entries = append(b.entries, tsqlEntry{kind: segmentStatements, statement: statement, line: statement.at.line})
		}
	}
	var body []uir.Statement
	for b.next < len(b.entries) {
		if b.peek(segmentEnd) {
			b.diagnoseMarker("END closes no BEGIN")
			continue
		}
		body = append(body, b.one()...)
	}
	return body
}

func (b *tsqlBody) peek(kind segmentKind) bool {
	return b.next < len(b.entries) && b.entries[b.next].kind == kind
}

func (b *tsqlBody) diagnoseMarker(message string) {
	entry := b.entries[b.next]
	b.next++
	b.f.diagnose(entry.line, strings.TrimSpace(lineAround(b.f.src, entry.line)), message)
}

// one lowers the next statement with everything it governs: a BEGIN its
// block, a TRY its CATCH, an IF or WHILE the statement after it. At an END or
// the body's end it lowers nothing.
func (b *tsqlBody) one() []uir.Statement {
	if b.next == len(b.entries) || b.peek(segmentEnd) {
		return nil
	}
	entry := b.entries[b.next]
	b.next++
	switch entry.kind {
	case segmentBegin, segmentBeginCatch:
		return b.block()
	case segmentBeginTry:
		try := uir.TryStmt{Body: blockOf(b.block())}
		if b.peek(segmentBeginCatch) {
			b.next++
			try.Catch = blockOf(b.block())
		}
		return []uir.Statement{try}
	case segmentElse:
		b.next--
		b.diagnoseMarker("ELSE follows no IF")
		return nil
	}
	read := entry.statement
	if read.err != nil {
		b.f.diagnose(read.at.line, read.at.text, read.err.Error())
		return nil
	}
	if read.cond == nil {
		return read.stmts
	}
	return append(read.stmts, b.controlled(*read.cond))
}

// block lowers the statements up to the END that closes the BEGIN just read.
func (b *tsqlBody) block() []uir.Statement {
	var stmts []uir.Statement
	for b.next < len(b.entries) && !b.peek(segmentEnd) {
		stmts = append(stmts, b.one()...)
	}
	if b.next < len(b.entries) {
		b.next++
	}
	return stmts
}

// controlled is the IF or WHILE cond opens, over the statement it governs.
func (b *tsqlBody) controlled(cond condition) uir.Statement {
	var then []uir.Statement
	if !cond.jumps {
		then = b.one()
	}
	guard := conditionExpr(cond.text)
	if cond.loop {
		return uir.WhileStmt{Condition: guard, Body: blockOf(then)}
	}
	stmt := uir.NewIf(guard.Expr).WithThen(blockOf(then))
	if b.peek(segmentElse) {
		b.next++
		stmt.WithElse(blockOf(b.one()))
	}
	return stmt.Build()
}

func blockOf(stmts []uir.Statement) uir.BlockStmt {
	return uir.NewBlock().WithStatements(stmts...).Build()
}

// lineAround is the text of the 1-based line of text.
func lineAround(text string, line int) string {
	lines := strings.SplitN(text, "\n", line+1)
	if line > len(lines) {
		return ""
	}
	return lines[line-1]
}
