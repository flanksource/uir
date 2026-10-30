package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
)

// uses lists what the target's body uses: every resolved occurrence other than a definition inside the
// extent of one of its declarations, except the uses of the declarations nested in it, which are part
// of the target rather than used by it. Each row is about the symbol used, positioned at the
// occurrence, with the used symbol's declaration in scope identifying it.
func (index *compactIndex) uses(ctx context.Context, target ModuleSymbol) ([]ModuleMatch, error) {
	postings, err := index.postings(ctx, []string{target.ID}, "definition")
	if err != nil {
		return nil, err
	}
	if err := index.prefetch(ctx, postings); err != nil {
		return nil, err
	}
	matches := []ModuleMatch{}
	for _, posting := range postings {
		document, err := index.document(ctx, posting)
		if err != nil {
			return nil, err
		}
		entry, err := index.entry(posting, document, target.ID)
		if err != nil {
			return nil, err
		}
		nested := nestedDeclarations(document.content.Symbols, entry)
		for _, occurrence := range document.content.Occurrences {
			if occurrence.Symbol == nil || occurrence.Role == "definition" || nested[*occurrence.Symbol] || !inBody(occurrence, entry) {
				continue
			}
			matches = append(matches, index.occurrenceMatch(posting, document, "use", occurrence))
		}
	}
	return matches, index.identifyUsed(ctx, matches)
}

// inBody reports whether an occurrence lies in a declaration's body: it is enclosed by the
// declaration, or its span lies within the declaration's extent.
func inBody(occurrence storage.DocumentOccurrence, declaration storage.DocumentSymbol) bool {
	if occurrence.Enclosing != nil && *occurrence.Enclosing == *declaration.ID {
		return true
	}
	return occurrence.Bytes[0] >= declaration.ExtentBytes[0] && occurrence.Bytes[1] <= declaration.ExtentBytes[1]
}

// nestedDeclarations are the ids of the symbols a document declares within a declaration's extent,
// other than the declaration itself.
func nestedDeclarations(symbols []storage.DocumentSymbol, declaration storage.DocumentSymbol) map[string]bool {
	nested := map[string]bool{}
	for _, symbol := range symbols {
		if symbol.ID == nil || *symbol.ID == *declaration.ID {
			continue
		}
		if symbol.ExtentBytes[0] >= declaration.ExtentBytes[0] && symbol.ExtentBytes[1] <= declaration.ExtentBytes[1] {
			nested[*symbol.ID] = true
		}
	}
	return nested
}

// identifyUsed names each use row by the symbol used: the identifier, node kind and props of its
// declaration in the row's own scope, else of its first declaration in scope, else the identifier its
// symbol row spells when no selected scope declares it.
func (index *compactIndex) identifyUsed(ctx context.Context, matches []ModuleMatch) error {
	ids := map[string]bool{}
	for _, match := range matches {
		ids[match.SymbolID] = true
	}
	declarations, err := index.declarations(ctx, sortedKeys(ids), "definition")
	if err != nil {
		return err
	}
	declared := map[string][]ModuleMatch{}
	for _, declaration := range declarations {
		declared[declaration.SymbolID] = append(declared[declaration.SymbolID], declaration)
	}
	symbols, err := index.symbolsByID(ctx, sortedKeys(ids))
	if err != nil {
		return err
	}
	undeclared := map[string]ModuleSymbol{}
	for _, symbol := range symbols {
		undeclared[symbol.ID] = symbol
	}
	for position := range matches {
		match := &matches[position]
		if candidates := declared[match.SymbolID]; len(candidates) > 0 {
			declaration := candidates[max(0, slices.IndexFunc(candidates, func(candidate ModuleMatch) bool { return matchScope(candidate) == matchScope(*match) }))]
			match.Identifier, match.NodeKind, match.Props = declaration.Identifier, declaration.NodeKind, declaration.Props
			continue
		}
		symbol, found := undeclared[match.SymbolID]
		if !found {
			return fmt.Errorf("use at %s:%d names symbol %s, which has no symbol row", match.Path, *match.Line, match.SymbolID)
		}
		match.Identifier, match.NodeKind = index.identifier(symbol), symbol.Kind
	}
	return nil
}
