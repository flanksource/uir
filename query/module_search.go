package query

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
)

// searchKinds ranks search results: types first, then functions, methods, members, and values.
var searchKinds = []string{"type", "func", "method", "field", "const", "var", "package"}

// searchHit is a scanned symbol declared in scope: its definition postings and the ordinal of its
// search name within its kind's scan order.
type searchHit struct {
	symbol   storage.Symbol
	group    int
	postings []scopedPosting
}

// search range-scans symbols.search_name for a prefix, one kind at a time in rank order, keeps the
// symbols declared in an active document of the scope, and stops once the limit is filled. Rows rank
// by kind, search name (in the database's scan order), module, package, owner, and name, and are
// positioned by reading only the defining documents of the rows within the limit. "Owner.Name"
// resolves owners by exact search name and scans their children by the name prefix. When the scan
// stops early, Total counts the rows found up to that point.
func (index *indexContext) search(ctx context.Context, input string, limit int, result *ModuleQueryResult) error {
	owner, name, qualified := strings.Cut(input, ".")
	if qualified && strings.Contains(name, ".") {
		return fmt.Errorf("search %q: qualify with at most one dot, as Owner.Name", input)
	}
	prefix := storage.SearchName(name)
	if !qualified {
		prefix = storage.SearchName(owner)
	}
	if (!qualified && prefix == "") || (qualified && storage.SearchName(owner) == "") {
		return fmt.Errorf("search %q: the name must contain at least one of [a-z0-9]", input)
	}
	hits, stopped, err := index.searchDeclared(ctx, searchScan{owner: owner, prefix: prefix, qualified: qualified}, limit)
	if err != nil {
		return err
	}
	rows := make([]storage.Symbol, 0, len(hits))
	byID := make(map[string]searchHit, len(hits))
	for _, hit := range hits {
		rows = append(rows, hit.symbol)
		byID[hit.symbol.ID] = hit
		result.Total += len(hit.postings)
	}
	symbols, err := index.moduleSymbols(ctx, rows)
	if err != nil {
		return err
	}
	slices.SortFunc(symbols, func(left, right ModuleSymbol) int {
		return cmp.Or(
			cmp.Compare(slices.Index(searchKinds, left.Kind), slices.Index(searchKinds, right.Kind)),
			cmp.Compare(byID[left.ID].group, byID[right.ID].group),
			strings.Compare(left.ModuleKey, right.ModuleKey), strings.Compare(left.PackagePath, right.PackagePath),
			strings.Compare(left.Owner, right.Owner), strings.Compare(left.Name, right.Name), strings.Compare(left.ID, right.ID),
		)
	})
	stage := fmt.Sprintf("prefix %q: %d declared symbols", prefix, len(symbols))
	if stopped {
		stage += ", scan stopped at the limit"
	}
	result.Stages = append(result.Stages, ResolutionStage{Name: "search", Value: stage})
	for _, symbol := range symbols {
		rows, err := index.searchRows(byID[symbol.ID].postings)
		if err != nil {
			return err
		}
		result.Matches = append(result.Matches, rows...)
		if len(result.Matches) >= limit {
			break
		}
	}
	return nil
}

// searchScan selects the symbols whose search_name starts with prefix, as children of the owners
// whose search name is owner's when the input is qualified.
type searchScan struct {
	owner, prefix string
	qualified     bool
}

// searchDeclared scans each kind in rank order by (search_name, id) in batches, keeping the symbols
// with a definition posting in scope. Once the declared rows fill the limit it finishes the current
// search name, so every symbol that could rank within the limit is read, and reports that it stopped.
func (index *indexContext) searchDeclared(ctx context.Context, scan searchScan, limit int) ([]searchHit, bool, error) {
	var hits []searchHit
	rows, group := 0, 0
	for _, kind := range searchKinds {
		boundary, previous := "", ""
		var after *storage.Symbol
		for {
			batch, err := index.searchBatch(ctx, scan, kind, after)
			if err != nil {
				return nil, false, err
			}
			postings, err := index.postings(ctx, symbolIDs(batch), "definition")
			if err != nil {
				return nil, false, err
			}
			declared := map[string][]scopedPosting{}
			for _, posting := range postings {
				declared[posting.symbol] = append(declared[posting.symbol], posting)
			}
			for _, symbol := range batch {
				if !strings.HasPrefix(symbol.SearchName, scan.prefix) {
					continue
				}
				if boundary != "" && symbol.SearchName != boundary {
					return hits, true, nil
				}
				if len(declared[symbol.ID]) == 0 {
					continue
				}
				if symbol.SearchName != previous {
					group, previous = group+1, symbol.SearchName
				}
				hits = append(hits, searchHit{symbol: symbol, group: group, postings: declared[symbol.ID]})
				if rows += len(declared[symbol.ID]); rows >= limit && boundary == "" {
					boundary = symbol.SearchName
				}
			}
			if len(batch) < lookupBatch {
				break
			}
			after = &batch[len(batch)-1]
		}
		if boundary != "" {
			return hits, true, nil
		}
	}
	return hits, false, nil
}

// searchBatch reads the next lookupBatch symbols of one kind after the (search_name, id) key of
// after. The range ends at the prefix's upper bound within the [a-z0-9] alphabet (none for a prefix
// of only z); searchDeclared still keeps only the rows that start with the prefix, so a collation that
// orders the range differently can only over-fetch.
func (index *indexContext) searchBatch(ctx context.Context, scan searchScan, kind string, after *storage.Symbol) ([]storage.Symbol, error) {
	query := index.database.WithContext(ctx).Model(&storage.Symbol{}).Where("kind = ?", kind)
	if scan.prefix != "" {
		upper, bounded, err := storage.SearchPrefixUpperBound(scan.prefix)
		if err != nil {
			return nil, err
		}
		query = query.Where("search_name >= ?", scan.prefix)
		if bounded {
			query = query.Where("search_name < ?", upper)
		}
	}
	if scan.qualified {
		query = query.Where("owner_id IN (?)", index.database.Model(&storage.Symbol{}).Select("id").Where("search_name = ?", storage.SearchName(scan.owner)))
	}
	if after != nil {
		query = query.Where("(search_name > ? OR (search_name = ? AND id > ?))", after.SearchName, after.SearchName, after.ID)
	}
	var batch []storage.Symbol
	if err := query.Order("search_name").Order("id").Limit(lookupBatch).Find(&batch).Error; err != nil {
		return nil, fmt.Errorf("scan %s symbols with prefix %q (owner %q): %w", kind, scan.prefix, scan.owner, err)
	}
	return batch, nil
}

// searchRows positions one symbol at each of its declarations, ordered by root, checkout, and path.
func (index *indexContext) searchRows(postings []scopedPosting) ([]ModuleMatch, error) {
	rows := make([]ModuleMatch, 0, len(postings))
	for _, posting := range postings {
		document, err := index.document(posting)
		if err != nil {
			return nil, err
		}
		entry, err := index.entry(posting, document, posting.symbol)
		if err != nil {
			return nil, err
		}
		rows = append(rows, index.declarationMatch(posting, document, "symbol", "definition", entry))
	}
	sortMatches(rows)
	return rows, nil
}
