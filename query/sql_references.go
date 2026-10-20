package query

import (
	"context"
	"slices"

	"github.com/flanksource/uir/storage"
)

// Reference selectors range over identities used in the selected documents,
// including dependencies whose declarations are outside the selected snapshots.
func (index *compactIndex) referencedSymbols(ctx context.Context, selector Selector) (compactValue, error) {
	compiled, err := compileSelector(selector, nil)
	if err != nil {
		return compactValue{}, err
	}
	kinds, err := selectorKinds(selector, index.kinds)
	if err != nil {
		return compactValue{}, err
	}
	postings := index.scopeDocumentPostings()
	if err := index.prefetch(ctx, postings); err != nil {
		return compactValue{}, err
	}
	ids := map[string]bool{}
	for _, posting := range postings {
		document, err := index.document(ctx, posting)
		if err != nil {
			return compactValue{}, err
		}
		for _, occurrence := range document.content.Occurrences {
			if occurrence.Symbol != nil && occurrence.Role != "definition" {
				ids[*occurrence.Symbol] = true
			}
		}
	}
	rows, err := index.symbolRowsByID(ctx, sortedKeys(ids))
	if err != nil {
		return compactValue{}, err
	}
	var selected []storage.Symbol
	for _, id := range sortedKeys(ids) {
		row := rows[id]
		if len(kinds) > 0 && !slices.Contains(kinds, row.Kind) {
			continue
		}
		matches, err := index.selects(ctx, compiled, row)
		if err != nil {
			return compactValue{}, err
		}
		if matches {
			selected = append(selected, row)
		}
	}
	symbols, err := index.convertSymbols(ctx, selected)
	return compactValue{symbols: symbols}, err
}
