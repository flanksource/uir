package query

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	"gorm.io/gorm"
)

// postingRequest selects postings: of the symbols ids, with one of roles, and, when roots is set, only
// in the documents of the roots whose key it admits.
type postingRequest struct {
	ids   []string
	roles []string
	roots func(rootKey string) bool
}

// postings returns the postings of the symbols with one of the roles whose documents are active in a
// scope, one entry per scope that activates the document, ordered by scope, path, and symbol.
func (index *indexContext) postings(ctx context.Context, symbolIDs []string, roles ...string) ([]scopedPosting, error) {
	return index.postingsIn(ctx, postingRequest{ids: symbolIDs, roles: roles})
}

// postingsIn reads the postings a request selects. Postings are stored by symbol handle, role, root
// ordinal, and document ordinal. A symbol is defined only in the root of its own module, which its
// handle numbers, so definition postings are read in that root alone; the other roles are read for
// every admitted root at once, one statement per batch of handles, and kept where the document is
// active in a scope.
func (index *indexContext) postingsIn(ctx context.Context, request postingRequest) ([]scopedPosting, error) {
	if len(request.ids) == 0 {
		return nil, nil
	}
	handles, err := index.handlesOf(ctx, request.ids)
	if err != nil {
		return nil, err
	}
	codes := make([]storage.PostingRole, len(request.roles))
	for i, role := range request.roles {
		if codes[i], err = storage.ParsePostingRole(role); err != nil {
			return nil, err
		}
	}
	var found []scopedPosting
	if slices.Equal(codes, []storage.PostingRole{storage.RoleDefinition}) {
		found, err = index.definitionPostings(ctx, handles, request.roots)
	} else {
		found, err = index.usePostings(ctx, handles, codes, request.roots)
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(found, func(i, j int) bool {
		left, right := found[i], found[j]
		return cmp.Or(cmp.Compare(left.scope, right.scope), strings.Compare(left.path, right.path), strings.Compare(left.symbol, right.symbol)) < 0
	})
	return found, nil
}

// definitionPostings reads each handle's definition postings in the root of the module its handle
// numbers, skipping a handle every scope of that root is known not to define.
func (index *indexContext) definitionPostings(ctx context.Context, handles []int64, admits func(string) bool) ([]scopedPosting, error) {
	byRoot := map[int][]int64{}
	for _, handle := range handles {
		fields, err := symbolhandle.Unpack(handle)
		if err != nil {
			return nil, err
		}
		key, err := index.cache.moduleKey(ctx, index.database, int32(fields.Module))
		if err != nil {
			return nil, fmt.Errorf("handle %d: %w", handle, err)
		}
		position, selected := index.rootKeys[key]
		if !selected || admits != nil && !admits(key) || index.knownUndefined(index.roots[position], handle) {
			continue
		}
		byRoot[position] = append(byRoot[position], handle)
	}
	var found []scopedPosting
	for _, position := range slices.Sorted(maps.Keys(byRoot)) {
		root := index.roots[position]
		rows, err := rootPostings(ctx, index.database, byRoot[position], []storage.PostingRole{storage.RoleDefinition}, root)
		if err != nil {
			return nil, err
		}
		if found, err = index.scoped(found, root, rows); err != nil {
			return nil, err
		}
	}
	return found, nil
}

// usePostings reads the handles' postings of the roles in every admitted root of the scopes, one
// statement per batch of handles over the (symbol_handle, role, root_ordinal) prefix of the posting
// index, and keeps those whose document a scope activates.
func (index *indexContext) usePostings(ctx context.Context, handles []int64, roles []storage.PostingRole, admits func(string) bool) ([]scopedPosting, error) {
	byOrdinal := map[int32]int{}
	for position, root := range index.roots {
		if len(root.ordinals) > 0 && (admits == nil || admits(root.key)) {
			byOrdinal[root.ordinal] = position
		}
	}
	if len(byOrdinal) == 0 {
		return nil, nil
	}
	ordinals := slices.Sorted(maps.Keys(byOrdinal))
	var found []scopedPosting
	for start := 0; start < len(handles); start += lookupBatch {
		var rows []storage.SymbolPosting
		batch := handles[start:min(start+lookupBatch, len(handles))]
		if err := index.database.WithContext(ctx).Model(&storage.SymbolPosting{}).Select("document_ordinal", "root_ordinal", "symbol_handle").
			Where("symbol_handle IN ? AND role IN ? AND root_ordinal IN ?", batch, roles, ordinals).Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load %v postings of %d symbols in %d roots: %w", roles, len(batch), len(ordinals), err)
		}
		byRoot := map[int][]storage.SymbolPosting{}
		for _, row := range rows {
			position := byOrdinal[row.RootOrdinal]
			byRoot[position] = append(byRoot[position], row)
		}
		for _, position := range slices.Sorted(maps.Keys(byRoot)) {
			var err error
			if found, err = index.scoped(found, index.roots[position], byRoot[position]); err != nil {
				return nil, err
			}
		}
	}
	return found, nil
}

// scoped appends a scoped posting for each scope of the root that activates a row's document.
func (index *indexContext) scoped(found []scopedPosting, root rootDocuments, rows []storage.SymbolPosting) ([]scopedPosting, error) {
	for _, row := range rows {
		document, active := root.documents[row.DocumentOrdinal]
		if !active {
			continue
		}
		symbol, known := index.symbols.id(row.SymbolHandle)
		if !known {
			return nil, fmt.Errorf("posting of document %d names handle %d, which this query did not ask for", row.DocumentOrdinal, row.SymbolHandle)
		}
		for _, position := range document.scopes {
			found = append(found, scopedPosting{scope: position, path: document.path, document: document.id, symbol: symbol})
		}
	}
	return found, nil
}

// postingHistory is how many postings per batched symbol and active document a root's posting range
// may hold, counting the documents of earlier snapshots, before probing the active documents is
// cheaper than reading the range.
const postingHistory = 4

// rootPostings intersects one root's postings of (symbols, roles) with its active documents over the
// (symbol_handle, role, root_ordinal, document_ordinal) index, reading whichever side is smaller: the
// root's posting range of the batch when it holds at most postingHistory rows per symbol and active
// document, and otherwise the active document ordinals in IN batches probing the posting key. The
// range read is bounded at one row past that, which is both the count that decides and the rows it
// returns.
func rootPostings(ctx context.Context, database *gorm.DB, handles []int64, roles []storage.PostingRole, root rootDocuments) ([]storage.SymbolPosting, error) {
	if len(root.ordinals) == 0 {
		return nil, nil
	}
	var found []storage.SymbolPosting
	for start := 0; start < len(handles); start += lookupBatch {
		symbols := handles[start:min(start+lookupBatch, len(handles))]
		selected := func() *gorm.DB {
			return database.WithContext(ctx).Model(&storage.SymbolPosting{}).Select("document_ordinal", "symbol_handle").
				Where("symbol_handle IN ? AND role IN ? AND root_ordinal = ?", symbols, roles, root.ordinal)
		}
		bound := postingHistory * (len(symbols) + len(root.ordinals))
		var rows []storage.SymbolPosting
		if err := selected().Limit(bound + 1).Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load %v postings of %d symbols in root %d: %w", roles, len(symbols), root.ordinal, err)
		}
		if len(rows) <= bound {
			for _, row := range rows {
				if _, ok := root.documents[row.DocumentOrdinal]; ok {
					found = append(found, row)
				}
			}
			continue
		}
		for offset := 0; offset < len(root.ordinals); offset += lookupBatch {
			var probed []storage.SymbolPosting
			batch := root.ordinals[offset:min(offset+lookupBatch, len(root.ordinals))]
			if err := selected().Where("document_ordinal IN ?", batch).Find(&probed).Error; err != nil {
				return nil, fmt.Errorf("probe %v postings of %d symbols in root %d: %w", roles, len(symbols), root.ordinal, err)
			}
			found = append(found, probed...)
		}
	}
	return found, nil
}
