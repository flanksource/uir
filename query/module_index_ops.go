package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
)

// declarations reads the symbols' definition postings in scope and returns one row per declaration.
func (index *indexContext) declarations(ctx context.Context, symbolIDs []string, kind string) ([]ModuleMatch, error) {
	postings, err := index.postings(ctx, symbolIDs, "definition")
	if err != nil {
		return nil, err
	}
	matches := []ModuleMatch{}
	for _, posting := range postings {
		document, err := index.document(posting)
		if err != nil {
			return nil, err
		}
		entry, err := index.entry(posting, document, posting.symbol)
		if err != nil {
			return nil, err
		}
		matches = append(matches, index.declarationMatch(posting, document, kind, "definition", entry))
	}
	return matches, nil
}

// candidates lists every declaration of several resolved symbols, plus one unpositioned row for a
// symbol that is only referenced in scope, so the caller can narrow the selector.
func (index *indexContext) candidates(ctx context.Context, symbols []ModuleSymbol) ([]ModuleMatch, error) {
	ids := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		ids = append(ids, symbol.ID)
	}
	matches, err := index.declarations(ctx, ids, "candidate")
	if err != nil {
		return nil, err
	}
	for _, symbol := range symbols {
		if !slices.ContainsFunc(matches, func(match ModuleMatch) bool { return match.SymbolID == symbol.ID }) {
			matches = append(matches, ModuleMatch{Kind: "candidate", SymbolID: symbol.ID, Identifier: symbol.identifier()})
		}
	}
	return matches, nil
}

// occurrences reads the reference postings of the symbols and returns a row for every occurrence of
// one of them that keep accepts, named by its enclosing declaration.
func (index *indexContext) occurrences(ctx context.Context, target string, symbols map[string]bool, kind string, keep func(storage.DocumentOccurrence) bool) ([]ModuleMatch, error) {
	ids := make([]string, 0, len(symbols))
	for id := range symbols {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	postings, err := index.postings(ctx, ids, "reference")
	if err != nil {
		return nil, err
	}
	matches := []ModuleMatch{}
	for _, posting := range postings {
		document, err := index.document(posting)
		if err != nil {
			return nil, err
		}
		for _, occurrence := range document.content.Occurrences {
			if occurrence.Symbol == nil || *occurrence.Symbol != posting.symbol || !keep(occurrence) {
				continue
			}
			match := index.occurrenceMatch(posting, document, kind, occurrence)
			match.Dispatch = posting.symbol != target
			matches = append(matches, match)
		}
	}
	return matches, nil
}

// implementations lists the declared types whose implements entries name the target interface.
func (index *indexContext) implementations(ctx context.Context, target ModuleSymbol) ([]ModuleMatch, error) {
	if target.Kind != "type" {
		return nil, fmt.Errorf("implementations requires a type, %s is a %s", target.Name, target.Kind)
	}
	postings, err := index.postings(ctx, []string{target.ID}, "implements")
	if err != nil {
		return nil, err
	}
	matches := []ModuleMatch{}
	for _, posting := range postings {
		document, err := index.document(posting)
		if err != nil {
			return nil, err
		}
		for _, entry := range document.content.Symbols {
			if entry.ID != nil && slices.Contains(entry.Implements, target.ID) {
				matches = append(matches, index.declarationMatch(posting, document, "implementation", "implements", entry))
			}
		}
	}
	return matches, nil
}

// callers lists the call occurrences of the target and, with dispatch, of the interface methods the
// target's receiver type implements in each scope.
func (index *indexContext) callers(ctx context.Context, target ModuleSymbol, dispatch bool, result *ModuleQueryResult) error {
	targets := map[string]bool{target.ID: true}
	if dispatch {
		methods, err := index.dispatchMethods(ctx, target)
		if err != nil {
			return err
		}
		for _, method := range methods {
			targets[method] = true
		}
		noun := "interface methods"
		if len(methods) == 1 {
			noun = "interface method"
		}
		result.Stages = append(result.Stages, ResolutionStage{Name: "dispatch", Value: fmt.Sprintf("%d %s", len(methods), noun)})
	}
	var err error
	result.Matches, err = index.occurrences(ctx, target.ID, targets, "caller", func(occurrence storage.DocumentOccurrence) bool {
		return occurrence.Role == "call"
	})
	return err
}

// dispatchMethods maps a concrete method T.M to I.M for every interface I that T's declarations in
// scope implement. Interfaces outside the import closure of T are not recorded, so not proven.
func (index *indexContext) dispatchMethods(ctx context.Context, target ModuleSymbol) ([]string, error) {
	if target.Kind != "method" || target.OwnerID == "" {
		return nil, fmt.Errorf("dispatch applies to methods, %s is a %s", target.Name, target.Kind)
	}
	postings, err := index.postings(ctx, []string{target.OwnerID}, "definition")
	if err != nil {
		return nil, err
	}
	if len(postings) == 0 {
		return nil, fmt.Errorf("including dispatch needs the declaration of %s in scope; select the root that declares it", target.Owner)
	}
	var interfaces []string
	for _, posting := range postings {
		document, err := index.document(posting)
		if err != nil {
			return nil, err
		}
		entry, err := index.entry(posting, document, target.OwnerID)
		if err != nil {
			return nil, err
		}
		interfaces = append(interfaces, entry.Implements...)
	}
	slices.Sort(interfaces)
	interfaces = slices.Compact(interfaces)
	var methods []string
	for start := 0; start < len(interfaces); start += lookupBatch {
		var rows []storage.Symbol
		batch := interfaces[start:min(start+lookupBatch, len(interfaces))]
		if err := index.database.WithContext(ctx).Where("owner_id IN ? AND kind = ? AND search_name = ? AND name = ?", batch, "method", storage.SearchName(target.Name), target.Name).Order("id").Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load interface methods named %s: %w", target.Name, err)
		}
		for _, row := range rows {
			same, err := sameParameters(row.ParameterTypes, target.ParameterTypes)
			if err != nil {
				return nil, err
			}
			if same {
				methods = append(methods, row.ID)
			}
		}
	}
	return methods, nil
}

// callees lists the call occurrences whose enclosing declaration is the target, resolved or not.
func (index *indexContext) callees(ctx context.Context, target ModuleSymbol) ([]ModuleMatch, error) {
	postings, err := index.postings(ctx, []string{target.ID}, "definition")
	if err != nil {
		return nil, err
	}
	matches := []ModuleMatch{}
	for _, posting := range postings {
		document, err := index.document(posting)
		if err != nil {
			return nil, err
		}
		for _, occurrence := range document.content.Occurrences {
			if !occurrence.IsCall() || occurrence.Enclosing == nil || *occurrence.Enclosing != target.ID {
				continue
			}
			match := index.occurrenceMatch(posting, document, "callee", occurrence)
			match.Identifier = *occurrence.Target
			matches = append(matches, match)
		}
	}
	return matches, nil
}
