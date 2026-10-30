package sql

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/flanksource/sqlglot-go/sqlglot"
	"github.com/flanksource/uir"
)

// procedureHeader is a CREATE/ALTER PROCEDURE or FUNCTION header up to the AS
// its body follows. A parameter written `@p AS int` ends it early; the body is
// then cut short and reported unparsed, never guessed at.
var procedureHeader = regexp.MustCompile(`(?is)\A\s*(?:CREATE|ALTER)\s+(?:OR\s+ALTER\s+)?(?:PROC|PROCEDURE|FUNCTION)\b.*?\bAS\b`)

// executeAsOpen is the EXECUTE of a header's `WITH EXECUTE AS <principal>`,
// ending where its AS starts; executeAsPrincipal is the principal after that
// AS, and the AS the body follows.
var (
	executeAsOpen      = regexp.MustCompile(`(?is)\bEXEC(?:UTE)?\s+\z`)
	executeAsPrincipal = regexp.MustCompile(`(?is)\A\s*(?:N?'[^']*'|\w+)(?:\s*,\s*\w+)*\s+AS\b`)
)

// headerStub is the body a header is parsed with: sqlglot-go reads a routine's
// signature only from a whole definition, and every procedure and function
// kind accepts this one.
const headerStub = "\nBEGIN RETURN END"

// lowerTSQL lowers routine definitions and groups anonymous statements across
// GO batches in one script node.
func lowerTSQL(f *fileLowering) error {
	batches, err := sqlglot.SplitBatches(f.src, "tsql")
	if err != nil {
		return err
	}
	type sourceBatch struct {
		text      string
		firstLine int
	}
	var anonymous []sourceBatch
	seenRoutine := false
	cursor := 0
	for _, batch := range batches {
		at := strings.Index(f.src[cursor:], batch)
		if at < 0 {
			return fmt.Errorf("batch %q is not in the source", batch)
		}
		offset := cursor + at
		cursor = offset + len(batch)
		text := blankComments(batch)
		firstLine := lineAt(f.src, offset)
		if procedureBodyStart(text) == 0 {
			if strings.TrimSpace(text) != "" {
				anonymous = append(anonymous, sourceBatch{text: text, firstLine: firstLine})
			}
			continue
		}
		seenRoutine = true
		f.lowerTSQLBatch(text, firstLine)
	}
	if len(anonymous) == 0 {
		return nil
	}
	diagnosticsBefore := len(f.file.Diagnostics)
	var statements []uir.Statement
	for _, batch := range anonymous {
		body := &tsqlBody{f: f, text: batch.text, firstLine: batch.firstLine,
			opts: statementOptions{dialect: "tsql", defaultSchema: f.dialect.defaultSchema}}
		statements = append(statements, body.lower(0)...)
	}
	if !seenRoutine || len(statements) > 0 || len(f.file.Diagnostics) > diagnosticsBefore {
		line := anonymous[0].firstLine + lineAt(anonymous[0].text, firstNonSpace(anonymous[0].text, 0)) - 1
		f.file.Routines = append(f.file.Routines, f.scriptNode(line, statements))
	}
	return nil
}

// lowerTSQLBatch lowers the routine one comment-blanked batch defines, which
// starts on firstLine of the file.
func (f *fileLowering) lowerTSQLBatch(text string, firstLine int) {
	start := procedureBodyStart(text)
	if start == 0 {
		return
	}
	line := firstLine + lineAt(text, firstNonSpace(text, 0)) - 1
	header := strings.TrimSpace(text[:start])
	tree, err := sqlglot.ParseOne(header+headerStub, "tsql")
	if err != nil {
		f.diagnose(line, header, "routine header: "+err.Error())
		return
	}
	routine, err := sqlglot.RoutineOf(tree)
	if err != nil {
		f.diagnose(line, header, err.Error())
		return
	}
	body := &tsqlBody{f: f, text: text, firstLine: firstLine,
		opts: statementOptions{dialect: "tsql", defaultSchema: f.dialect.defaultSchema}}
	method, err := f.routineNode(routine, line, body.lower(start))
	if err != nil {
		f.diagnose(line, header, err.Error())
		return
	}
	f.file.Routines = append(f.file.Routines, method)
}

// procedureBodyStart is the offset a definition's body starts at: after its
// header, an `EXECUTE AS` option not taken for the AS that ends it. A
// definition with no header is 0.
func procedureBodyStart(text string) int {
	header := procedureHeader.FindStringIndex(text)
	if header == nil {
		return 0
	}
	start := header[1]
	if executeAsOpen.MatchString(text[:start-len("AS")]) {
		if principal := executeAsPrincipal.FindStringIndex(text[start:]); principal != nil {
			start += principal[1]
		}
	}
	return start
}

// blankComments replaces every comment outside a string literal with spaces,
// keeping its newlines, so offsets and lines in the result are those of sql.
func blankComments(sql string) string {
	out := []byte(sql)
	inString := false
	for i := 0; i < len(out); i++ {
		if out[i] == '\'' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		end := commentEnd(sql, i)
		for j := i; j < end; j++ {
			if out[j] != '\n' {
				out[j] = ' '
			}
		}
		if end > i {
			i = end - 1
		}
	}
	return string(out)
}

func commentEnd(query string, i int) int {
	switch {
	case strings.HasPrefix(query[i:], "--"):
		if end := strings.IndexByte(query[i:], '\n'); end >= 0 {
			return i + end
		}
		return len(query)
	case strings.HasPrefix(query[i:], "/*"):
		if end := strings.Index(query[i+2:], "*/"); end >= 0 {
			return i + 2 + end + 2
		}
		return len(query)
	}
	return i
}

func firstNonSpace(text string, offset int) int {
	for offset < len(text) && isSpace(text[offset]) {
		offset++
	}
	return offset
}
