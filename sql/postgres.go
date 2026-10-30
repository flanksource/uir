package sql

import (
	"fmt"
	"strings"

	"github.com/flanksource/sqlglot-go/sqlglot"

	"github.com/flanksource/uir"
)

// pgText is PostgreSQL source as sqlglot-go tokenizes it: by rune, its
// tokens' offsets counting runes.
type pgText struct {
	runes     []rune
	tokens    []sqlglot.Token
	firstLine int
}

func tokenizePostgres(text string, firstLine int) (pgText, error) {
	tokens, err := sqlglot.Tokenize(text, "postgres")
	if err != nil {
		return pgText{}, err
	}
	return pgText{runes: []rune(text), tokens: tokens, firstLine: firstLine}, nil
}

// line is the file line of rune offset.
func (t pgText) line(offset int) int {
	lines := t.firstLine
	for _, r := range t.runes[:offset] {
		if r == '\n' {
			lines++
		}
	}
	return lines
}

// span is the text of tokens [from, to].
func (t pgText) span(from, to int) string {
	return string(t.runes[t.tokens[from].Start : t.tokens[to].End+1])
}

// statements are the token ranges [from, to] the text's top-level semicolons
// separate.
func (t pgText) statements() [][2]int {
	var out [][2]int
	from := 0
	for i, token := range t.tokens {
		if token.Type != sqlglot.TokSEMICOLON {
			continue
		}
		if i > from {
			out = append(out, [2]int{from, i - 1})
		}
		from = i + 1
	}
	if from < len(t.tokens) {
		out = append(out, [2]int{from, len(t.tokens) - 1})
	}
	return out
}

// lowerPostgres lowers each CREATE [OR REPLACE] FUNCTION or PROCEDURE of the
// script; any other statement defines no routine.
func lowerPostgres(f *fileLowering) error {
	text, err := tokenizePostgres(f.src, 1)
	if err != nil {
		return err
	}
	for _, stmt := range text.statements() {
		if createsRoutine(text.tokens[stmt[0] : stmt[1]+1]) {
			f.lowerPostgresRoutine(text, stmt[0], stmt[1])
		}
	}
	return nil
}

func createsRoutine(tokens []sqlglot.Token) bool {
	words := []string{}
	for _, token := range tokens[:min(len(tokens), 4)] {
		words = append(words, strings.ToUpper(token.Text))
	}
	if len(words) < 2 || words[0] != "CREATE" {
		return false
	}
	if len(words) >= 4 && words[1] == "OR" && words[2] == "REPLACE" {
		words = words[2:]
	}
	return words[1] == "FUNCTION" || words[1] == "PROCEDURE"
}

// lowerPostgresRoutine lowers the routine the statement [from, to] creates,
// its body read in the routine's language.
func (f *fileLowering) lowerPostgresRoutine(text pgText, from, to int) {
	definition, line := text.span(from, to), text.line(text.tokens[from].Start)
	tree, err := sqlglot.ParseOne(definition, "postgres")
	if err != nil {
		f.diagnose(line, definition, "routine definition: "+err.Error())
		return
	}
	routine, err := sqlglot.RoutineOf(tree)
	if err != nil {
		f.diagnose(line, definition, err.Error())
		return
	}
	body, err := f.postgresBody(text, from, to, routineLanguage(tree))
	if err != nil {
		f.diagnose(line, definition, err.Error())
	}
	method, err := f.routineNode(routine, line, body)
	if err != nil {
		f.diagnose(line, definition, err.Error())
		return
	}
	f.file.Routines = append(f.file.Routines, method)
}

func routineLanguage(tree *sqlglot.Expression) string {
	for _, property := range tree.FindAll("LanguageProperty") {
		return strings.ToLower(property.This().Name())
	}
	return ""
}

// postgresBody lowers the dollar-quoted body after the definition's AS.
func (f *fileLowering) postgresBody(text pgText, from, to int, language string) ([]uir.Statement, error) {
	if language != "sql" && language != "plpgsql" {
		return nil, fmt.Errorf("a body in language %q is not lowered: only sql and plpgsql are", language)
	}
	for i := from; i < to; i++ {
		if strings.EqualFold(text.tokens[i].Text, "AS") && text.tokens[i+1].Type == sqlglot.TokHEREDOC_STRING {
			return f.lowerPostgresBody(text, text.tokens[i+1], language)
		}
	}
	return nil, fmt.Errorf("the body is not a dollar-quoted string after AS")
}

func (f *fileLowering) lowerPostgresBody(text pgText, heredoc sqlglot.Token, language string) ([]uir.Statement, error) {
	tag := 1
	for text.runes[heredoc.Start+tag] != '$' {
		tag++
	}
	start := heredoc.Start + tag + 1
	body, err := tokenizePostgres(heredoc.Text, text.line(start))
	if err != nil {
		return nil, fmt.Errorf("tokenize body: %w", err)
	}
	pg := &pgBody{f: f, text: body, opts: statementOptions{dialect: "postgres", defaultSchema: f.dialect.defaultSchema}}
	if language == "sql" {
		return pg.sqlStatements(), nil
	}
	pg.opts.intoVariables = true
	return pg.plpgsql()
}

// pgBody lowers one routine body.
type pgBody struct {
	f    *fileLowering
	text pgText
	opts statementOptions
}

// sqlStatements lowers a LANGUAGE sql body: statements separated by
// semicolons.
func (b *pgBody) sqlStatements() []uir.Statement {
	var out []uir.Statement
	for _, stmt := range b.text.statements() {
		out = append(out, b.lower(stmt[0], stmt[1], "")...)
	}
	return out
}

// lower lowers the statement [from, to], read as prefix followed by the
// tokens after from when prefix is set; a statement that cannot be lowered
// is a diagnostic.
func (b *pgBody) lower(from, to int, prefix string) []uir.Statement {
	at := site{path: b.f.path, line: b.text.line(b.text.tokens[from].Start), text: b.text.span(from, to)}
	parsed := at.text
	if prefix != "" {
		parsed = prefix + b.text.span(from+1, to)
	}
	stmts, err := lowerStatement(parsed, at, b.opts)
	if err != nil {
		b.f.diagnose(at.line, at.text, err.Error())
		return nil
	}
	return stmts
}
