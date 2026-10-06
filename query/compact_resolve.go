package query

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
)

// compactIndex evaluates compact expressions over an index context. Symbol rows and query names live
// in the context's shared symbol facts; dispatch keeps the implementations of interface methods found
// in this query's scopes.
type compactIndex struct {
	*indexContext
	dispatch map[string][]ModuleSymbol
}

func newCompactIndex(index *indexContext) *compactIndex {
	return &compactIndex{indexContext: index, dispatch: map[string][]ModuleSymbol{}}
}

func (index *compactIndex) resolve(ctx context.Context, pattern string) ([]ModuleSymbol, error) {
	literal, exact := globLiteral(pattern)
	query := index.database.WithContext(ctx).Model(&storage.Symbol{})
	if exact {
		lastDot, lastSlash := strings.LastIndex(literal, "."), strings.LastIndex(literal, "/")
		switch {
		case lastDot > lastSlash:
			name := literal[lastDot+1:]
			query = query.Where("search_name = ? AND name = ?", storage.SearchName(name), name)
		case lastSlash >= 0:
			query = query.Where("kind = ? AND package_path = ?", "package", literal)
		default:
			query = query.Where("search_name = ? AND name = ?", storage.SearchName(literal), literal)
		}
	} else if !strings.Contains(pattern, `\`) {
		lastDot, lastSlash := strings.LastIndex(pattern, "."), strings.LastIndex(pattern, "/")
		if lastDot > lastSlash && lastSlash >= 0 && !strings.ContainsAny(pattern[:lastDot], "*?") {
			query = query.Where("package_path = ?", pattern[:lastDot])
		}
		lastPart := pattern[max(lastDot, lastSlash)+1:]
		if firstGlob := strings.IndexAny(lastPart, "*?"); firstGlob > 0 {
			if prefix := storage.SearchName(lastPart[:firstGlob]); prefix != "" {
				query = query.Where("search_name LIKE ?", prefix+"%")
			}
		}
	}
	var glob selectorGlob
	if !exact {
		var err error
		glob, err = compileSelectorGlob(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile symbol glob %q: %w", pattern, err)
		}
	}
	var rows []storage.Symbol
	if err := query.Order("id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("resolve %q: %w", pattern, err)
	}
	if err := index.prefetchOwners(ctx, rows); err != nil {
		return nil, err
	}
	matched := make([]storage.Symbol, 0, len(rows))
	for _, row := range rows {
		name, err := index.queryName(ctx, row)
		if err != nil {
			return nil, err
		}
		if symbolPatternMatches(pattern, name, glob) {
			matched = append(matched, row)
		}
	}
	return index.activeSymbols(ctx, matched)
}

// symbolPatternMatches reports whether a bare symbol pattern selects a qualified name: an exact
// pattern, its escapes removed, by its full path or a dotted suffix, and a glob by the glob.
func symbolPatternMatches(pattern, qualified string, glob selectorGlob) bool {
	literal, exact := globLiteral(pattern)
	legacyChildren := strings.HasSuffix(pattern, ".*") && strings.Count(pattern, "*") == 1 && !strings.ContainsAny(pattern, `?\`)
	if strings.Contains(pattern, "/") {
		if legacyChildren {
			return directChild(qualified, strings.TrimSuffix(pattern, ".*"))
		}
		if !exact {
			return glob.matches(qualified)
		}
		return qualified == literal
	}
	short := qualified[strings.LastIndex(qualified, "/")+1:]
	if legacyChildren {
		prefix := strings.TrimSuffix(pattern, ".*")
		return directChild(short, prefix) || directChildSuffix(short, prefix)
	}
	if !exact {
		for {
			if glob.matches(short) {
				return true
			}
			dot := strings.IndexByte(short, '.')
			if dot < 0 {
				return false
			}
			short = short[dot+1:]
		}
	}
	return short == literal || strings.HasSuffix(short, "."+literal)
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
	if name, found := index.symbols.name(row.ID); found {
		return name, nil
	}
	if err := index.remember([]storage.Symbol{row}); err != nil {
		return "", err
	}
	if row.Kind == "package" {
		index.symbols.keepName(row.ID, row.PackagePath)
		return row.PackagePath, nil
	}
	parts := []string{row.Name}
	ownerID := row.OwnerID
	for depth := 0; ownerID != nil; depth++ {
		if depth == 32 {
			return "", fmt.Errorf("symbol %s has an owner chain deeper than 32", row.ID)
		}
		owner, found := index.symbols.row(*ownerID)
		if !found {
			if err := index.database.WithContext(ctx).Where("id = ?", *ownerID).First(&owner).Error; err != nil {
				return "", fmt.Errorf("load owner %s of symbol %s: %w", *ownerID, row.ID, err)
			}
			if err := index.remember([]storage.Symbol{owner}); err != nil {
				return "", err
			}
		}
		parts = append(parts, owner.Name)
		ownerID = owner.OwnerID
	}
	slices.Reverse(parts)
	name := row.PackagePath + "." + strings.Join(parts, ".")
	index.symbols.keepName(row.ID, name)
	return name, nil
}

// prefetchOwners loads, in IN batches, every owner up the rows' owner chains that the pipeline has not
// seen, so naming the rows reads no owner one at a time.
func (index *compactIndex) prefetchOwners(ctx context.Context, rows []storage.Symbol) error {
	for depth := 0; len(rows) > 0; depth++ {
		if depth == 32 {
			return fmt.Errorf("symbol %s has an owner chain deeper than 32", rows[0].ID)
		}
		if err := index.remember(rows); err != nil {
			return err
		}
		missing := map[string]bool{}
		for _, row := range rows {
			if row.OwnerID != nil {
				if _, found := index.symbols.row(*row.OwnerID); !found {
					missing[*row.OwnerID] = true
				}
			}
		}
		owners := sortedKeys(missing)
		rows = rows[:0:0]
		for start := 0; start < len(owners); start += lookupBatch {
			var found []storage.Symbol
			if err := index.database.WithContext(ctx).Where("id IN ?", owners[start:min(start+lookupBatch, len(owners))]).Find(&found).Error; err != nil {
				return fmt.Errorf("load symbol owners: %w", err)
			}
			rows = append(rows, found...)
		}
		if len(rows) != len(owners) {
			return fmt.Errorf("loaded %d of %d symbol owners", len(rows), len(owners))
		}
	}
	return nil
}

// activeSymbols keeps the rows that a scope's active documents define, reference, or implement. The
// rows a scope defines come from its snapshot's defined-symbol set (symbol deltas), which equals its
// definition postings. A symbol no scope defines is still active through a reference or implements
// posting, such as a dependency's function the scope only calls or an interface it only implements,
// so those two roles are still read from postings, for the remaining rows only.
func (index *compactIndex) activeSymbols(ctx context.Context, rows []storage.Symbol) ([]ModuleSymbol, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	if err := index.remember(rows); err != nil {
		return nil, err
	}
	handles := make([]int64, len(rows))
	for i, row := range rows {
		handles[i] = row.Handle
	}
	defined, err := index.definedAnywhere(ctx, handles)
	if err != nil {
		return nil, err
	}
	var undefined []string
	for _, row := range rows {
		if !defined[row.Handle] {
			undefined = append(undefined, row.ID)
		}
	}
	postings, err := index.postings(ctx, undefined, "reference", "implements")
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for _, posting := range postings {
		used[posting.symbol] = true
	}
	rows = slices.DeleteFunc(rows, func(row storage.Symbol) bool { return !defined[row.Handle] && !used[row.ID] })
	return index.convertSymbols(ctx, rows)
}

func (index *compactIndex) convertSymbols(ctx context.Context, rows []storage.Symbol) ([]ModuleSymbol, error) {
	if err := index.remember(rows); err != nil {
		return nil, err
	}
	if err := index.prefetchOwners(ctx, rows); err != nil {
		return nil, err
	}
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
	byID, err := index.symbolRowsByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	return index.convertSymbols(ctx, slices.Collect(maps.Values(byID)))
}

// symbolRowsByID loads the rows of distinct symbol ids, reading only those the pipeline has not kept;
// an unknown id is an error.
func (index *compactIndex) symbolRowsByID(ctx context.Context, ids []string) (map[string]storage.Symbol, error) {
	rows := make(map[string]storage.Symbol, len(ids))
	var missing []string
	for _, id := range ids {
		if row, found := index.symbols.row(id); found {
			rows[id] = row
		} else {
			missing = append(missing, id)
		}
	}
	for start := 0; start < len(missing); start += lookupBatch {
		var found []storage.Symbol
		if err := index.database.WithContext(ctx).Where("id IN ?", missing[start:min(start+lookupBatch, len(missing))]).Find(&found).Error; err != nil {
			return nil, fmt.Errorf("load symbols by id: %w", err)
		}
		if err := index.remember(found); err != nil {
			return nil, err
		}
		for _, row := range found {
			rows[row.ID] = row
		}
	}
	if len(rows) != len(ids) {
		return nil, fmt.Errorf("loaded %d symbols for %d ids", len(rows), len(ids))
	}
	return rows, nil
}
