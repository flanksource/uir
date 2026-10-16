package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/alecthomas/participle/v2"
	"github.com/flanksource/uir/query/sqlsyntax"
	"github.com/flanksource/uir/storage"
)

type Language string

const (
	LanguageAuto    Language = "auto"
	LanguageCompact Language = "compact"
	LanguageSQL     Language = "sql"
)

type QueryColumn struct {
	Name  string `json:"name"`
	Field string `json:"field"`
	Type  string `json:"type"`
}

func DetectLanguage(input string, requested Language) (Language, error) {
	switch requested {
	case LanguageCompact, LanguageSQL:
		return requested, nil
	case "", LanguageAuto:
		text := strings.TrimSpace(input)
		end := strings.IndexFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
		if end < 0 {
			end = len(text)
		}
		if strings.EqualFold(text[:end], "SELECT") {
			return LanguageSQL, nil
		}
		return LanguageCompact, nil
	default:
		return "", &InvalidQueryError{Message: fmt.Sprintf("unknown query language %q", requested), Hint: "Choose auto, compact, or sql."}
	}
}

func (pipeline *Pipeline) runSQL(ctx context.Context, input string, options ModuleScopeOptions) (ModuleQueryResult, error) {
	if pipeline == nil || pipeline.database == nil {
		return ModuleQueryResult{}, errors.New("UIR query database is required")
	}
	parsed, err := sqlsyntax.Parse(input)
	if err != nil {
		invalid := &InvalidQueryError{Message: err.Error(), Hint: "Use SELECT fields FROM selector or navigation(source), followed by optional WHERE, ORDER BY, and LIMIT.", Cause: err}
		var syntax participle.Error
		if errors.As(err, &syntax) {
			invalid.Line = syntax.Position().Line
			invalid.Column = syntax.Position().Column
		}
		return ModuleQueryResult{}, invalid
	}
	limit := options.Limit
	if parsed.Limit != nil {
		limit = *parsed.Limit
	}
	if limit == 0 && parsed.Limit == nil {
		limit = defaultLimit
	}
	if limit < 1 || limit > maximumLimit {
		return ModuleQueryResult{}, sqlError(parsed.Pos, "LIMIT must be between 1 and %d", maximumLimit)
	}
	kinds, err := storage.LoadSymbolKinds(ctx, pipeline.database)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	source, err := bindSQLSource(parsed.Source, kinds, false)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	if source.function.Terminal && (!parsed.Projection.Wildcard || parsed.Where != nil || len(parsed.Order) > 0 || parsed.Limit != nil) {
		return ModuleQueryResult{}, sqlError(parsed.Source.Pos, "%s requires SELECT * and no row clauses", source.function.Name)
	}
	catalog, err := storage.LoadDomainCatalog(ctx, pipeline.database)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	if err := validateSQLCatalog(source, catalog); err != nil {
		return ModuleQueryResult{}, err
	}
	fields, err := sqlPropertyFields(source, kinds, catalog)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	rows := sqlRows{fields: fields}
	if err := rows.bind(parsed); err != nil {
		return ModuleQueryResult{}, err
	}
	session, trace := pipeline.session()
	clock := newStageClock()
	selection, err := session.moduleScopes(ctx, options, false)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	coverage, err := session.scopeCoverage(ctx, selection.scopes)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	result := ModuleQueryResult{Operation: OperationSet, Matches: []ModuleMatch{}, Coverage: coverage, Stages: []ResolutionStage{{Name: "parse", Value: "sql"}, {Name: "scope", Value: fmt.Sprintf("%d snapshots", len(selection.scopes))}, selectionCoverageStage(selection, coverage)}}
	base, err := newIndexContext(ctx, session, selection.scopes)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	index := newCompactIndex(base)
	rows.predicate, err = rows.bindPredicate(ctx, index, parsed.Where)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	clock.lap("bind")
	if source.function.Name == "graph" {
		return session.sqlGraph(ctx, index, source, options, result)
	}
	value, enclosing, err := index.evaluateSQL(ctx, source, &result)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	result.Symbols = value.symbols
	result.Path = value.path
	if source.function.Name == "path" {
		result.Operation = OperationPath
		if value.path != nil {
			records, err := index.sqlRecords(ctx, value, false)
			if err != nil {
				return ModuleQueryResult{}, err
			}
			if err := index.sqlRecordProperties(ctx, records); err != nil {
				return ModuleQueryResult{}, err
			}
			for i, record := range records {
				value.path.Calls[i] = record.match
			}
			result.Matches = value.path.Calls
			result.Total = 1
		}
		return result, nil
	}
	records, err := index.sqlRecords(ctx, value, enclosing)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	sortSQLRecords(records)
	if err := index.sqlRecordProperties(ctx, records); err != nil {
		return ModuleQueryResult{}, err
	}
	rows.project(records, limit, &result)
	clock.lap("execute")
	result.Stages = append(result.Stages, ResolutionStage{Name: "execute", Value: fmt.Sprintf("%d rows", result.Total)})
	result.Stages = append(result.Stages, clock.stages(trace)...)
	return result, nil
}
