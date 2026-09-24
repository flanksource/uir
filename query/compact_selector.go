package query

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
)

func (index *compactIndex) resolveSelector(ctx context.Context, selector Selector) (compactValue, error) {
	glob, err := compileSelectorGlob(selector.Pattern)
	if err != nil {
		return compactValue{}, err
	}
	var moduleGlob selectorGlob
	if selector.ModulePattern != "" {
		moduleGlob, err = compileSelectorGlob(selector.ModulePattern)
		if err != nil {
			return compactValue{}, err
		}
	}
	query := index.database.WithContext(ctx).Model(&storage.Symbol{})
	exact := !strings.ContainsAny(selector.Pattern, "*?\\")
	switch selector.Kind {
	case "func":
		query = query.Where("kind IN ?", []string{"func", "method"})
		if exact && !strings.ContainsAny(selector.Pattern, "./") {
			query = query.Where("name = ?", selector.Pattern)
		}
	case "field":
		query = query.Where("kind = ?", "field")
		if exact && !strings.ContainsAny(selector.Pattern, "./") {
			query = query.Where("name = ?", selector.Pattern)
		}
	case "struct":
		query = query.Where("kind = ?", "type")
		if exact && !strings.ContainsAny(selector.Pattern, "./") {
			query = query.Where("name = ?", selector.Pattern)
		}
	case "pkg", "mod":
		if exact && selector.ModulePattern == "" {
			column := "package_path"
			if selector.Kind == "mod" {
				column = "module_key"
			}
			query = query.Where(column+" = ?", selector.Pattern)
		}
	default:
		return compactValue{}, fmt.Errorf("unknown selector kind %q", selector.Kind)
	}
	var candidates []storage.Symbol
	if err := query.Order("id").Find(&candidates).Error; err != nil {
		return compactValue{}, fmt.Errorf("resolve %s selector: %w", selector.Kind, err)
	}
	matched := make([]storage.Symbol, 0, len(candidates))
	for _, row := range candidates {
		key := row.PackagePath
		switch selector.Kind {
		case "mod":
			key = row.ModuleKey
		case "func", "field", "struct":
			key = row.Name
			if strings.Contains(selector.Pattern, ".") || strings.Contains(selector.Pattern, "/") {
				key, err = index.queryName(ctx, row)
				if err != nil {
					return compactValue{}, err
				}
				if !strings.Contains(selector.Pattern, "/") {
					key = strings.TrimPrefix(key, row.PackagePath+".")
				}
			}
		}
		if selector.ModulePattern != "" {
			if !moduleGlob.matches(row.ModuleKey) || !strings.HasPrefix(row.PackagePath, row.ModuleKey) {
				continue
			}
			switch row.PackagePath {
			case row.ModuleKey:
				key = "."
			default:
				if !strings.HasPrefix(row.PackagePath, row.ModuleKey+"/") {
					continue
				}
				key = strings.TrimPrefix(row.PackagePath, row.ModuleKey+"/")
			}
		}
		if glob.matches(key) {
			matched = append(matched, row)
		}
	}
	postings, err := index.postings(ctx, symbolIDs(matched), "definition")
	if err != nil {
		return compactValue{}, err
	}
	byID := make(map[string]storage.Symbol, len(matched))
	for _, row := range matched {
		byID[row.ID] = row
	}
	active := map[string]bool{}
	var matches []ModuleMatch
	for _, posting := range postings {
		row := byID[posting.symbol]
		root := index.scopes[posting.scope].root.RootKey
		if row.ModuleKey != root || selector.ModulePattern != "" && !moduleGlob.matches(root) {
			continue
		}
		document, err := index.document(posting)
		if err != nil {
			return compactValue{}, err
		}
		entry, err := index.entry(posting, document, row.ID)
		if err != nil {
			return compactValue{}, err
		}
		if selector.Kind == "struct" {
			form, err := storage.TypeForm(document.content.Version, entry)
			if err != nil {
				return compactValue{}, fmt.Errorf("snapshot %s document %s: %w", index.scopes[posting.scope].snapshot.ID, posting.document, err)
			}
			if form != "struct" {
				continue
			}
		}
		active[row.ID] = true
		matches = append(matches, index.declarationMatch(posting, document, "symbol", "definition", entry))
	}
	selected := make([]storage.Symbol, 0, len(active))
	for _, row := range matched {
		if active[row.ID] {
			selected = append(selected, row)
		}
	}
	symbols, err := index.convertSymbols(ctx, selected)
	if err != nil {
		return compactValue{}, err
	}
	slices.SortFunc(matches, func(a, b ModuleMatch) int { return strings.Compare(a.SymbolID, b.SymbolID) })
	return compactValue{symbols: symbols, matches: matches}, nil
}
