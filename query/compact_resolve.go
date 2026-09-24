package query

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
)

type compactIndex struct {
	*indexContext
	names map[string]string
	rows  map[string]storage.Symbol
	dispatch map[string][]ModuleSymbol
}

func newCompactIndex(index *indexContext) *compactIndex {
	return &compactIndex{indexContext: index, names: map[string]string{}, rows: map[string]storage.Symbol{}, dispatch: map[string][]ModuleSymbol{}}
}

func (index *compactIndex) resolve(ctx context.Context, pattern string) ([]ModuleSymbol, error) {
	wildcard := strings.HasSuffix(pattern, ".*")
	query := index.database.WithContext(ctx).Model(&storage.Symbol{})
	if !wildcard {
		lastDot, lastSlash := strings.LastIndex(pattern, "."), strings.LastIndex(pattern, "/")
		switch {
		case lastDot > lastSlash:
			name := pattern[lastDot+1:]
			query = query.Where("search_name = ? AND name = ?", storage.SearchName(name), name)
		case lastSlash >= 0:
			query = query.Where("kind = ? AND package_path = ?", "package", pattern)
		default:
			query = query.Where("search_name = ? AND name = ?", storage.SearchName(pattern), pattern)
		}
	}
	var rows []storage.Symbol
	if err := query.Order("id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("resolve %q: %w", pattern, err)
	}
	matched := make([]storage.Symbol, 0, len(rows))
	for _, row := range rows {
		name, err := index.queryName(ctx, row)
		if err != nil {
			return nil, err
		}
		if symbolPatternMatches(pattern, name) {
			matched = append(matched, row)
		}
	}
	return index.activeSymbols(ctx, matched)
}

func symbolPatternMatches(pattern, qualified string) bool {
	if strings.Contains(pattern, "/") {
		if strings.HasSuffix(pattern, ".*") {
			return directChild(qualified, strings.TrimSuffix(pattern, ".*"))
		}
		return qualified == pattern
	}
	short := qualified[strings.LastIndex(qualified, "/")+1:]
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, ".*")
		return directChild(short, prefix) || directChildSuffix(short, prefix)
	}
	return short == pattern || strings.HasSuffix(short, "."+pattern)
}

func directChild(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix+".") {
		return false
	}
	child := strings.TrimPrefix(name, prefix+".")
	return child != "" && !strings.Contains(child, ".")
}

func directChildSuffix(name, prefix string) bool {
	for dot := strings.IndexByte(name, '.'); dot >= 0; {
		if directChild(name[dot+1:], prefix) {
			return true
		}
		next := strings.IndexByte(name[dot+1:], '.')
		if next < 0 {
			return false
		}
		dot += next + 1
	}
	return false
}

func (index *compactIndex) queryName(ctx context.Context, row storage.Symbol) (string, error) {
	if name, found := index.names[row.ID]; found {
		return name, nil
	}
	index.rows[row.ID] = row
	if row.Kind == "package" {
		index.names[row.ID] = row.PackagePath
		return row.PackagePath, nil
	}
	parts := []string{row.Name}
	ownerID := row.OwnerID
	for depth := 0; ownerID != nil; depth++ {
		if depth == 32 {
			return "", fmt.Errorf("symbol %s has an owner chain deeper than 32", row.ID)
		}
		owner, found := index.rows[*ownerID]
		if !found {
			if err := index.database.WithContext(ctx).Where("id = ?", *ownerID).First(&owner).Error; err != nil {
				return "", fmt.Errorf("load owner %s of symbol %s: %w", *ownerID, row.ID, err)
			}
			index.rows[*ownerID] = owner
		}
		parts = append(parts, owner.Name)
		ownerID = owner.OwnerID
	}
	slices.Reverse(parts)
	name := row.PackagePath + "." + strings.Join(parts, ".")
	index.names[row.ID] = name
	return name, nil
}

func (index *compactIndex) activeSymbols(ctx context.Context, rows []storage.Symbol) ([]ModuleSymbol, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	postings, err := index.postings(ctx, symbolIDs(rows), "definition", "reference", "implements")
	if err != nil {
		return nil, err
	}
	active := map[string]bool{}
	for _, posting := range postings {
		active[posting.symbol] = true
	}
	rows = slices.DeleteFunc(rows, func(row storage.Symbol) bool { return !active[row.ID] })
	return index.convertSymbols(ctx, rows)
}

func (index *compactIndex) convertSymbols(ctx context.Context, rows []storage.Symbol) ([]ModuleSymbol, error) {
	symbols, err := index.moduleSymbols(ctx, rows)
	if err != nil {
		return nil, err
	}
	for position, row := range rows {
		symbols[position].QueryName, err = index.queryName(ctx, row)
		if err != nil {
			return nil, err
		}
	}
	slices.SortFunc(symbols, func(a, b ModuleSymbol) int {
		if a.QueryName != b.QueryName {
			return strings.Compare(a.QueryName, b.QueryName)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return symbols, nil
}

func (index *compactIndex) symbolsByID(ctx context.Context, ids []string) ([]ModuleSymbol, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows := make([]storage.Symbol, 0, len(ids))
	for start := 0; start < len(ids); start += lookupBatch {
		var found []storage.Symbol
		if err := index.database.WithContext(ctx).Where("id IN ?", ids[start:min(start+lookupBatch, len(ids))]).Find(&found).Error; err != nil {
			return nil, fmt.Errorf("load symbols by id: %w", err)
		}
		rows = append(rows, found...)
	}
	if len(rows) != len(ids) {
		return nil, fmt.Errorf("loaded %d symbols for %d ids", len(rows), len(ids))
	}
	return index.convertSymbols(ctx, rows)
}
