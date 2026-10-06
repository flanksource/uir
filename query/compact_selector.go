package query

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	"gorm.io/gorm"
)

// compiledSelector is a typed selector with its globs compiled. within, when set, is a scope predicate
// the selected symbols must also satisfy, which narrows the roots and handle ranges read.
type compiledSelector struct {
	Selector
	glob, moduleGlob, ownerGlob selectorGlob
	within                      *scopePredicate
}

func compileSelector(selector Selector, within *scopePredicate) (compiledSelector, error) {
	compiled := compiledSelector{Selector: selector, within: within}
	var err error
	if compiled.glob, err = compileSelectorGlob(selector.Pattern); err != nil {
		return compiledSelector{}, err
	}
	if selector.ModulePattern != "" {
		if compiled.moduleGlob, err = compileSelectorGlob(selector.ModulePattern); err != nil {
			return compiledSelector{}, err
		}
	}
	if selector.Owner != "" {
		if compiled.ownerGlob, err = compileSelectorGlob(selector.Owner); err != nil {
			return compiledSelector{}, err
		}
	}
	return compiled, nil
}

func (index *compactIndex) resolveSelector(ctx context.Context, selector Selector) (compactValue, error) {
	return index.resolveSelectorWithin(ctx, selector, nil)
}

// resolveSelectorWithin resolves a selector, keeping only the symbols within admits when it is set.
func (index *compactIndex) resolveSelectorWithin(ctx context.Context, selector Selector, within *scopePredicate) (compactValue, error) {
	if selector.Kind == "module" || selector.Kind == "package" {
		glob, err := compileSelectorGlob(selector.Pattern)
		if err != nil {
			return compactValue{}, err
		}
		return index.resolveTreeSelector(ctx, selector.Kind, selector.Pattern, glob)
	}
	compiled, err := compileSelector(selector, within)
	if err != nil {
		return compactValue{}, err
	}
	candidates, err := index.selectorCandidates(ctx, compiled)
	if err != nil {
		return compactValue{}, err
	}
	matched := make([]storage.Symbol, 0, len(candidates))
	for _, row := range candidates {
		selected, err := index.selects(ctx, compiled, row)
		if err != nil {
			return compactValue{}, err
		}
		if selected {
			matched = append(matched, row)
		}
	}
	matches, active, err := index.selectorDeclarations(ctx, compiled, matched)
	if err != nil {
		return compactValue{}, err
	}
	symbols, err := index.convertSymbols(ctx, slices.DeleteFunc(matched, func(row storage.Symbol) bool { return !active[row.ID] }))
	if err != nil {
		return compactValue{}, err
	}
	slices.SortFunc(matches, func(a, b ModuleMatch) int { return strings.Compare(a.SymbolID, b.SymbolID) })
	return compactValue{symbols: symbols, matches: matches}, nil
}

// selects reports whether a candidate row matches the selector's pattern.
func (index *compactIndex) selects(ctx context.Context, selector compiledSelector, row storage.Symbol) (bool, error) {
	if selector.Kind == "path" {
		name, err := index.queryName(ctx, row)
		if err != nil {
			return false, err
		}
		return pathMatchesSymbol(selector.glob, selector.Pattern, ModuleSymbol{ModuleKey: row.ModuleKey, PackagePath: row.PackagePath, QueryName: name}), nil
	}
	key, inside, err := index.selectorKey(ctx, selector, row)
	return inside && selector.glob.matches(key), err
}

// selectorFilter narrows a symbols query by the kinds a selector selects and, for a plain name, by
// that name through the search index.
func selectorFilter(query *gorm.DB, selector Selector, registry symbolhandle.Kinds) (*gorm.DB, error) {
	kinds, err := selectorKinds(selector, registry)
	if err != nil {
		return nil, err
	}
	if !symbolSelectors[selector.Kind] {
		return query, nil
	}
	query = query.Where("kind IN ?", kinds)
	if !strings.ContainsAny(selector.Pattern, "*?\\./") {
		query = query.Where("search_name = ? AND name = ?", storage.SearchName(selector.Pattern), selector.Pattern)
	}
	return query, nil
}

// selectorCandidates loads, ordered by id, the symbols of the selector's kind in the handle ranges of
// the admitted roots that the root's scope defines. A selector matches only symbols defined in their
// own module's root, so dependencies, other roots, and symbols no selected snapshot defines are never
// read or named.
func (index *compactIndex) selectorCandidates(ctx context.Context, selector compiledSelector) ([]storage.Symbol, error) {
	if selector.Kind == "kind" {
		if err := index.register(ctx, selector.SymbolKind); err != nil {
			return nil, err
		}
	}
	filtered, err := selectorFilter(index.database.WithContext(ctx).Model(&storage.Symbol{}), selector.Selector, index.kinds)
	if err != nil {
		return nil, err
	}
	scopes := index.selectorScopes(selector)
	if len(scopes) == 0 {
		return nil, nil
	}
	ranges, err := index.selectorRanges(ctx, selector, slices.Sorted(maps.Keys(scopes)))
	if err != nil {
		return nil, err
	}
	ranges = mergeRanges(ranges)
	filtered = filtered.Session(&gorm.Session{})
	var candidates []storage.Symbol
	for start := 0; start < len(ranges); start += rangeBatch {
		var rows []storage.Symbol
		clause, arguments := rangeClause(ranges[start:min(start+rangeBatch, len(ranges))])
		if err := filtered.Where(clause, arguments...).Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("resolve %s selector: %w", selector.Kind, err)
		}
		candidates = append(candidates, rows...)
	}
	if selector.within != nil {
		candidates = slices.DeleteFunc(candidates, func(row storage.Symbol) bool { return !selector.within.admitsPackage(row.PackagePath, row.ModuleKey) })
	}
	if err := index.remember(candidates); err != nil {
		return nil, err
	}
	if candidates, err = index.definedInRoot(ctx, scopes, candidates); err != nil {
		return nil, err
	}
	slices.SortFunc(candidates, func(left, right storage.Symbol) int { return strings.Compare(left.ID, right.ID) })
	return candidates, index.prefetchOwners(ctx, candidates)
}

// definedInRoot keeps the candidates that a scope of their own module's root defines.
func (index *compactIndex) definedInRoot(ctx context.Context, scopes map[string][]int, candidates []storage.Symbol) ([]storage.Symbol, error) {
	defined := map[int64]bool{}
	for root, positions := range scopes {
		handles := make([]int64, 0, len(candidates))
		for _, row := range candidates {
			if row.ModuleKey == root {
				handles = append(handles, row.Handle)
			}
		}
		if len(handles) == 0 {
			continue
		}
		for _, position := range positions {
			found, err := index.definedBy(ctx, position, handles)
			if err != nil {
				return nil, err
			}
			maps.Copy(defined, found)
		}
	}
	return slices.DeleteFunc(candidates, func(row storage.Symbol) bool { return !defined[row.Handle] }), nil
}

// selectorKey is the spelling a selector's pattern is matched against: the package path, the module
// key, the name, or the qualified name when the pattern has a package or owner, and with a module
// pattern the package path relative to the module; false for a package outside its module. An
// Entity:Field reference matches the name, and is false for a symbol whose direct owner's name its
// owner glob rejects.
func (index *compactIndex) selectorKey(ctx context.Context, selector compiledSelector, row storage.Symbol) (string, bool, error) {
	if selector.Owner != "" {
		if row.OwnerID == nil {
			return "", false, nil
		}
		owner, found := index.symbols.row(*row.OwnerID)
		if !found {
			return "", false, fmt.Errorf("owner %s of symbol %s was not prefetched", *row.OwnerID, row.ID)
		}
		return row.Name, selector.ownerGlob.matches(owner.Name), nil
	}
	key := row.PackagePath
	switch selector.Kind {
	case "mod":
		key = row.ModuleKey
	case "all":
		key = row.Name
	case "func", "method", "field", "struct", "type", "var", "kind":
		key = row.Name
		if strings.ContainsAny(selector.Pattern, "./") {
			name, err := index.queryName(ctx, row)
			if err != nil {
				return "", false, err
			}
			key = name
			if !strings.Contains(selector.Pattern, "/") {
				key = strings.TrimPrefix(key, row.PackagePath+".")
			}
		}
	}
	if selector.ModulePattern == "" {
		return key, true, nil
	}
	if !selector.moduleGlob.matches(row.ModuleKey) {
		return "", false, nil
	}
	relative, inside := relativePackage(row.PackagePath, row.ModuleKey)
	return relative, inside, nil
}

func (index *compactIndex) resolveTreeSelector(ctx context.Context, kind, pattern string, glob selectorGlob) (compactValue, error) {
	if kind != "module" {
		if err := index.prefetch(ctx, index.scopeDocumentPostings()); err != nil {
			return compactValue{}, err
		}
	}
	value := compactValue{}
	seen := map[string]bool{}
	for position, scope := range index.scopes {
		paths := map[string]bool{}
		if kind == "module" {
			paths[scope.root.RootKey] = true
		} else {
			for id := range scope.documents {
				document, err := index.document(ctx, scopedPosting{scope: position, document: id})
				if err != nil {
					return compactValue{}, err
				}
				paths[document.content.PackagePath] = true
			}
		}
		for path := range paths {
			if pattern != "*" && !glob.matches(path) {
				continue
			}
			id := kind + ":" + path
			if !seen[id] {
				value.symbols = append(value.symbols, ModuleSymbol{ID: id, ModuleKey: scope.root.RootKey, PackagePath: path, Kind: kind, Name: path, QueryName: path, Visibility: "exported"})
				seen[id] = true
			}
			identifier := uir.Identifier{Module: scope.root.RootKey, Package: path, NodeType: uir.NodeTypePackage}
			if kind == "module" {
				identifier = uir.Identifier{Module: path, NodeType: uir.NodeTypeModule}
			}
			value.matches = append(value.matches, ModuleMatch{Kind: kind, NodeKind: kind, RootKey: scope.root.RootKey, Location: scope.location.CanonicalPath, SnapshotID: scope.snapshot.ID.String(), PackagePath: path, Path: path, SymbolID: id, Identifier: identifier, Role: "definition"})
		}
	}
	sortMatches(value.matches)
	slices.SortFunc(value.symbols, func(a, b ModuleSymbol) int { return strings.Compare(a.QueryName, b.QueryName) })
	return value, nil
}

// selectorDeclarations positions each matched symbol at its declarations in the scopes of its own
// module's root, keeping for a struct selector only declarations whose type form is a struct, and
// reports which symbols kept one.
func (index *compactIndex) selectorDeclarations(ctx context.Context, selector compiledSelector, matched []storage.Symbol) ([]ModuleMatch, map[string]bool, error) {
	postings, err := index.postings(ctx, symbolIDs(matched), "definition")
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[string]storage.Symbol, len(matched))
	for _, row := range matched {
		byID[row.ID] = row
	}
	postings = slices.DeleteFunc(postings, func(posting scopedPosting) bool {
		root := index.scopes[posting.scope].root.RootKey
		return byID[posting.symbol].ModuleKey != root || selector.ModulePattern != "" && !selector.moduleGlob.matches(root)
	})
	if err := index.prefetch(ctx, postings); err != nil {
		return nil, nil, err
	}
	active := map[string]bool{}
	var matches []ModuleMatch
	for _, posting := range postings {
		row := byID[posting.symbol]
		document, err := index.document(ctx, posting)
		if err != nil {
			return nil, nil, err
		}
		entry, err := index.entry(posting, document, row.ID)
		if err != nil {
			return nil, nil, err
		}
		if selector.Kind == "struct" {
			form, err := storage.TypeForm(document.content.Version, entry)
			if err != nil {
				return nil, nil, fmt.Errorf("snapshot %s document %s: %w", index.scopes[posting.scope].snapshot.ID, posting.document, err)
			}
			if form != "struct" {
				continue
			}
		}
		active[row.ID] = true
		matches = append(matches, index.declarationMatch(posting, document, "symbol", "definition", entry))
	}
	return matches, active, nil
}
