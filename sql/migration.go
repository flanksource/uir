package sql

import (
	"cmp"
	"errors"
	"fmt"
	"strings"

	"github.com/flanksource/sqlglot-go/sqlglot"
	"github.com/flanksource/sqlglot-go/sqlglot/access"
)

type MigrationIssue struct {
	Range         sqlglot.SourceRange
	Rule, Message string
}

type MigrationAnalysis struct {
	Accesses []access.Access
	Features []sqlglot.CompatibilityFeature
	Issues   []MigrationIssue
}

func AnalyzeMigration(source, language, schema string) (MigrationAnalysis, error) {
	d := dialects[language]
	if language == "sqlite" {
		d = dialect{name: language, defaultSchema: "main"}
	}
	if d.name == "" {
		return MigrationAnalysis{}, fmt.Errorf("expected dialect tsql, postgres or sqlite, got %q", language)
	}
	result := MigrationAnalysis{}
	options := access.StatementOptions{Dialect: language, DefaultSchema: cmp.Or(schema, d.defaultSchema)}
	result.analyze(source, options, sqlglot.SourceRange{}, 0)
	return result, nil
}

func (a *MigrationAnalysis) analyze(source string, options access.StatementOptions, origin sqlglot.SourceRange, depth int) {
	if depth > 16 {
		a.coverage(origin, "dynamic SQL nesting exceeds 16")
		return
	}
	result, err := sqlglot.ParseMigrationScript(source, options.Dialect)
	if err != nil {
		var token *sqlglot.TokenError
		var input *sqlglot.ScriptInputError
		span := origin
		if errors.As(err, &token) {
			span = migrationSpan(origin, sqlglot.SourceRange{Start: token.Start, End: token.End + 1})
		}
		if errors.As(err, &input) {
			span = migrationSpan(origin, input.Range)
		}
		a.Issues = append(a.Issues, MigrationIssue{Range: span, Rule: "sql-parse", Message: err.Error()})
		return
	}
	for _, diagnostic := range result.Diagnostics {
		span := migrationSpan(origin, sqlglot.SourceRange{Start: diagnostic.Start, End: diagnostic.End + 1})
		a.Issues = append(a.Issues, MigrationIssue{Range: span, Rule: "sql-parse", Message: diagnostic.Label})
	}
	for _, statement := range result.Statements {
		a.statement(statement, source, options, origin, depth)
	}
}

func (a *MigrationAnalysis) statement(statement *sqlglot.Expression, source string, options access.StatementOptions, origin sqlglot.SourceRange, depth int) {
	pos, _ := statement.SourceRange()
	if options.Dialect == "tsql" {
		for _, feature := range sqlglot.MigrationFeatures(statement) {
			feature.Start = migrationSpan(origin, sqlglot.SourceRange{Start: feature.Start}).Start
			a.Features = append(a.Features, feature)
		}
	}
	analyzed := access.ReadMigrationStatement(statement, options)
	for _, operation := range analyzed.Accesses {
		span := pos
		if operation.Range != nil {
			span = *operation.Range
		}
		span = migrationSpan(origin, span)
		operation.Range = &span
		a.Accesses = append(a.Accesses, operation)
	}
	for _, issue := range analyzed.Issues {
		a.coverage(migrationSpan(origin, issue.Range), issue.Message)
	}
	for _, script := range analyzed.Dynamic {
		a.analyze(script.Text, options, migrationSpan(origin, script.Range), depth+1)
	}
	if options.Dialect == "postgres" {
		a.postgresMigrationBody(statement, source, options, origin, depth)
	}
}

func (a *MigrationAnalysis) postgresMigrationBody(statement *sqlglot.Expression, source string, options access.StatementOptions, origin sqlglot.SourceRange, depth int) {
	var body *sqlglot.Expression
	language := ""
	switch statement.Class {
	case "Do":
		body = statement.This()
		lang, _ := statement.Args["language"].(*sqlglot.Expression)
		language = cmp.Or(lang.Name(), "plpgsql")
	case "Create":
		kind, _ := statement.Args["kind"].(string)
		if kind != "FUNCTION" && kind != "PROCEDURE" {
			return
		}
		body, _ = statement.Args["expression"].(*sqlglot.Expression)
		language = routineLanguage(statement)
	}
	if body == nil || body.Class != "Literal" && body.Class != "Heredoc" {
		return
	}
	span, ok := body.SourceRange()
	if !ok {
		span, _ = statement.SourceRange()
	}
	at := migrationSpan(origin, span)
	if language != "sql" && language != "plpgsql" {
		a.coverage(at, fmt.Sprintf("cannot inspect a routine body in language %q", language))
		return
	}
	bodyOrigin := at
	runes := []rune(source)
	if ok && span.Start < len(runes) && runes[span.Start] == '$' && origin.End == origin.Start {
		_, tail, found := strings.Cut(string(runes[span.Start+1:]), "$")
		if found {
			start := len(runes) - len([]rune(tail)) + origin.Start
			bodyOrigin = sqlglot.SourceRange{Start: start, End: start}
		}
	}
	if language == "sql" {
		a.analyze(body.Name(), options, bodyOrigin, depth+1)
		return
	}
	text, err := tokenizePostgres(body.Name(), 1)
	if err != nil {
		a.coverage(at, err.Error())
		return
	}
	if len(text.tokens) == 0 {
		a.coverage(at, "empty PL/pgSQL body")
		return
	}
	options.IntoVariables = true
	parser := &pgBody{text: text, opts: options, audit: &plMigration{analysis: a, origin: bodyOrigin, depth: depth}}
	if _, err := parser.plpgsql(); err != nil {
		a.coverage(at, err.Error())
	}
}

func (a *MigrationAnalysis) coverage(span sqlglot.SourceRange, message string) {
	a.Issues = append(a.Issues, MigrationIssue{Range: span, Rule: "sql-analysis-incomplete", Message: message})
}

func migrationSpan(origin, span sqlglot.SourceRange) sqlglot.SourceRange {
	if origin.End > origin.Start {
		return origin
	}
	return sqlglot.SourceRange{Start: origin.Start + span.Start, End: origin.Start + span.End}
}
