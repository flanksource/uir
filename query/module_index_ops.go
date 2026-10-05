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
	if err := index.prefetch(ctx, postings); err != nil {
		return nil, err
	}
	matches := []ModuleMatch{}
	for _, posting := range postings {
		document, err := index.document(ctx, posting)
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
			matches = append(matches, ModuleMatch{Kind: "candidate", SymbolID: symbol.ID, Identifier: index.identifier(symbol)})
		}
	}
	return matches, nil
}

// occurrenceQuery selects occurrence rows of kind: the occurrences keep accepts of the symbols in
// targets, each of which stands for the target it maps to, in the documents of the roots roots admits,
// or of every root when roots is nil. A row of a symbol that stands for another target is a dispatch
// row.
type occurrenceQuery struct {
	targets map[string]string
	kind    string
	keep    func(storage.DocumentOccurrence) bool
	roots   func(rootKey string) bool
}

// singleTarget maps each symbol to target, which a call to it stands for.
func singleTarget(target string, symbols ...string) map[string]string {
	targets := map[string]string{target: target}
	for _, symbol := range symbols {
		targets[symbol] = target
	}
	return targets
}

// occurrences reads the reference postings of the query's symbols and returns a row for every
// occurrence of one of them that keep accepts, named by its enclosing declaration.
func (index *indexContext) occurrences(ctx context.Context, request occurrenceQuery) ([]ModuleMatch, error) {
	postings, err := index.postingsIn(ctx, postingRequest{ids: sortedKeys(keysOf(request.targets)), roles: []string{"reference"}, roots: request.roots})
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
		for _, occurrence := range document.content.Occurrences {
			if occurrence.Symbol == nil || *occurrence.Symbol != posting.symbol || !request.keep(occurrence) {
				continue
			}
			match := index.occurrenceMatch(posting, document, request.kind, occurrence)
			match.Dispatch = posting.symbol != request.targets[posting.symbol]
			matches = append(matches, match)
		}
	}
	return matches, nil
}

func keysOf[V any](values map[string]V) map[string]bool {
	keys := make(map[string]bool, len(values))
	for key := range values {
		keys[key] = true
	}
	return keys
}

// declaredBy lists, as rows of kind and role, the declarations in the documents of the postings whose
// entry has field naming the target.
func (index *indexContext) declaredBy(ctx context.Context, postings []scopedPosting, kind, role string, names func(storage.DocumentSymbol) []string, target string) ([]ModuleMatch, error) {
	if err := index.prefetch(ctx, postings); err != nil {
		return nil, err
	}
	var matches []ModuleMatch
	for _, posting := range postings {
		document, err := index.document(ctx, posting)
		if err != nil {
			return nil, err
		}
		for _, entry := range document.content.Symbols {
			if entry.ID != nil && slices.Contains(names(entry), target) {
				matches = append(matches, index.declarationMatch(posting, document, kind, role, entry))
			}
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
	matches, err := index.declaredBy(ctx, postings, "implementation", "implements", func(entry storage.DocumentSymbol) []string { return entry.Implements }, target.ID)
	if matches == nil && err == nil {
		matches = []ModuleMatch{}
	}
	return matches, err
}

func (index *indexContext) embedders(ctx context.Context, target ModuleSymbol) ([]ModuleMatch, error) {
	if target.Kind != "type" {
		return nil, fmt.Errorf("inheritance requires a type, %s is a %s", target.Name, target.Kind)
	}
	postings, err := index.postings(ctx, []string{target.ID}, "embeds")
	if err != nil {
		return nil, err
	}
	return index.declaredBy(ctx, postings, "inheritance", "embeds", func(entry storage.DocumentSymbol) []string { return entry.Embeds }, target.ID)
}

func (index *indexContext) requireEmbeddingFacts(ctx context.Context) error {
	all := index.scopeDocumentPostings()
	if err := index.prefetch(ctx, all); err != nil {
		return err
	}
	for _, posting := range all {
		document, err := index.document(ctx, posting)
		if err != nil {
			return err
		}
		if document.content.Version < 3 {
			return fmt.Errorf("snapshot %s lacks embedding facts; reindex its checkout", index.scopes[posting.scope].snapshot.ID)
		}
	}
	return nil
}

// callers lists the call occurrences of the target and, with dispatch, of the interface methods the
// target's receiver type implements in each scope, in the roots roots admits (every root when nil).
func (index *indexContext) callers(ctx context.Context, target ModuleSymbol, dispatch bool, roots func(string) bool, result *ModuleQueryResult) ([]ModuleMatch, error) {
	targets := singleTarget(target.ID)
	if dispatch {
		methods, err := index.dispatchMethods(ctx, target)
		if err != nil {
			return nil, err
		}
		targets = singleTarget(target.ID, methods...)
		noun := "interface methods"
		if len(methods) == 1 {
			noun = "interface method"
		}
		result.Stages = append(result.Stages, ResolutionStage{Name: "dispatch", Value: fmt.Sprintf("%d %s", len(methods), noun)})
	}
	return index.occurrences(ctx, occurrenceQuery{targets: targets, kind: "caller", keep: isCall, roots: roots})
}

func isCall(occurrence storage.DocumentOccurrence) bool { return occurrence.Role == "call" }

// dispatchMethods maps a concrete method T.M to I.M for every interface I that T's declarations in
// scope implement. Interfaces outside the import closure of T are not recorded, so not proven.
func (index *indexContext) dispatchMethods(ctx context.Context, target ModuleSymbol) ([]string, error) {
	if target.Kind != "method" || target.OwnerID == "" {
		return nil, fmt.Errorf("dispatch applies to methods, %s is a %s", target.Name, target.Kind)
	}
	interfaces, declared, err := index.ownerInterfaces(ctx, target.OwnerID)
	if err != nil {
		return nil, err
	}
	if !declared {
		return nil, fmt.Errorf("including dispatch needs the declaration of %s in scope; select the root that declares it", target.Owner)
	}
	var methods []string
	for start := 0; start < len(interfaces); start += lookupBatch {
		var rows []storage.Symbol
		batch := interfaces[start:min(start+lookupBatch, len(interfaces))]
		if err := index.database.WithContext(ctx).Where("owner_id IN ? AND kind = ? AND search_name = ? AND name = ?", batch, "method", storage.SearchName(target.Name), target.Name).Order("id").Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load interface methods named %s: %w", target.Name, err)
		}
		if err := index.remember(rows); err != nil {
			return nil, err
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

// ownerInterfaces are the interfaces the declarations in scope of the owner implement, sorted and
// distinct, and whether the owner is declared in scope at all.
func (index *indexContext) ownerInterfaces(ctx context.Context, owner string) ([]string, bool, error) {
	postings, err := index.postings(ctx, []string{owner}, "definition")
	if err != nil {
		return nil, false, err
	}
	if err := index.prefetch(ctx, postings); err != nil {
		return nil, false, err
	}
	var interfaces []string
	for _, posting := range postings {
		document, err := index.document(ctx, posting)
		if err != nil {
			return nil, false, err
		}
		entry, err := index.entry(posting, document, owner)
		if err != nil {
			return nil, false, err
		}
		interfaces = append(interfaces, entry.Implements...)
	}
	slices.Sort(interfaces)
	return slices.Compact(interfaces), len(postings) > 0, nil
}

// callees lists the call occurrences whose enclosing declaration is the target, resolved or not.
func (index *indexContext) callees(ctx context.Context, target ModuleSymbol) ([]ModuleMatch, error) {
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
