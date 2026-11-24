package lint

import (
	"context"
	"strings"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
)

func (rule *Rule) compileQuery() error {
	language, err := query.DetectLanguage(skipQueryComments(rule.Query), query.LanguageAuto)
	if err != nil {
		return err
	}
	if language == query.LanguageSQL {
		requirements, err := query.AnalyzeSQLRows(rule.Query)
		rule.mode = requirements.Mode
		return err
	}
	rule.mode = storage.AnalysisTyped
	rule.expression, err = query.ParseCompactRows(rule.Query)
	return err
}

func skipQueryComments(input string) string {
	for {
		text := strings.TrimSpace(input)
		switch {
		case strings.HasPrefix(text, "--"):
			_, input, _ = strings.Cut(text, "\n")
		case strings.HasPrefix(text, "/*"):
			_, rest, closed := strings.Cut(text[2:], "*/")
			if !closed {
				return text
			}
			input = rest
		default:
			return text
		}
	}
}

func (rule Rule) visit(ctx context.Context, pipeline *query.Pipeline, scope query.ModuleScopeOptions, visit func(query.QueryRow) error) (query.ModuleQueryResult, error) {
	if rule.expression != nil {
		return pipeline.VisitCompact(ctx, rule.expression, scope, visit)
	}
	return pipeline.VisitSQL(ctx, rule.Query, scope, visit)
}
