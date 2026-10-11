package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
)

func (pipeline *Pipeline) sqlSelectorChoices(ctx context.Context, prefix string, parts []selectorPart, scopes []moduleScope) (map[string]bool, error) {
	name := parts[0].text
	if slices.Contains([]string{"pkg", "mod", "func", "method", "field", "type", "struct", "var", "kind"}, name) {
		return pipeline.selectorChoices(ctx, prefix, parts, scopes)
	}
	if len(parts) != 2 {
		return nil, fmt.Errorf("%s selector requires one pattern", name)
	}
	kinds, err := storage.LoadSymbolKinds(ctx, pipeline.database)
	if err != nil {
		return nil, err
	}
	registered := name
	if registered == "api" {
		registered = "endpoint"
	}
	if _, err := kinds.Lookup(registered); err != nil {
		return nil, err
	}
	return pipeline.suggestSymbols(ctx, Selector{Kind: "kind", SymbolKind: registered, Pattern: parts[1].text, NameOnly: true}, scopes, func(symbol ModuleSymbol) string {
		return name + ":" + QuoteSelectorValue(symbol.Name)
	})
}
