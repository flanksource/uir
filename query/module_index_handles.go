package query

import (
	"context"
	"slices"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
)

// remember records the handles of fully loaded symbol rows, and the rows.
func (index *indexContext) remember(rows []storage.Symbol) error {
	return index.symbols.keepRows(rows)
}

// handlesOf maps symbol ids to their sorted, distinct handles, reading only the ids the pipeline has
// not seen yet; an unknown id is an error.
func (index *indexContext) handlesOf(ctx context.Context, ids []string) ([]int64, error) {
	handles := make([]int64, 0, len(ids))
	var missing []string
	for _, id := range ids {
		if handle, known := index.symbols.handle(id); known {
			handles = append(handles, handle)
		} else {
			missing = append(missing, id)
		}
	}
	slices.Sort(missing)
	loaded, err := storage.SymbolHandles(ctx, index.database, slices.Compact(missing))
	if err != nil {
		return nil, err
	}
	if err := index.symbols.keepHandles(loaded); err != nil {
		return nil, err
	}
	for _, handle := range loaded {
		handles = append(handles, handle)
	}
	slices.Sort(handles)
	return slices.Compact(handles), nil
}

// definedBy is the subset of handles that one scope's snapshot defines, from its symbol deltas. A
// handle is defined exactly when a document active in the snapshot declares it, which is when the
// snapshot has a definition posting for it. Membership is kept per snapshot by the pipeline, so a
// handle is read at most once per snapshot.
func (index *indexContext) definedBy(ctx context.Context, scope int, handles []int64) (map[int64]bool, error) {
	facts := index.scopes[scope].facts
	var unknown []int64
	facts.mu.Lock()
	for _, handle := range handles {
		if _, seen := facts.defined[handle]; !seen {
			unknown = append(unknown, handle)
		}
	}
	facts.mu.Unlock()
	var found []int64
	if len(unknown) > 0 {
		var err error
		if found, err = storage.DefinedSymbols(ctx, index.database, index.scopes[scope].snapshot.ID, unknown); err != nil {
			return nil, err
		}
	}
	facts.mu.Lock()
	defer facts.mu.Unlock()
	for _, handle := range unknown {
		facts.defined[handle] = false
	}
	for _, handle := range found {
		facts.defined[handle] = true
	}
	defined := map[int64]bool{}
	for _, handle := range handles {
		if facts.defined[handle] {
			defined[handle] = true
		}
	}
	return defined, nil
}

// knownUndefined reports whether every scope of the root is already known not to define the handle,
// which rules out a definition posting in the root's active documents without reading postings.
func (index *indexContext) knownUndefined(root rootDocuments, handle int64) bool {
	for _, position := range root.scopes {
		facts := index.scopes[position].facts
		facts.mu.Lock()
		defined, known := facts.defined[handle]
		facts.mu.Unlock()
		if !known || defined {
			return false
		}
	}
	return true
}

// definedAnywhere is the subset of handles that at least one scope's snapshot defines.
func (index *indexContext) definedAnywhere(ctx context.Context, handles []int64) (map[int64]bool, error) {
	defined := map[int64]bool{}
	seen := map[uuid.UUID]bool{}
	for position, scope := range index.scopes {
		if seen[scope.snapshot.ID] {
			continue
		}
		seen[scope.snapshot.ID] = true
		found, err := index.definedBy(ctx, position, handles)
		if err != nil {
			return nil, err
		}
		for handle := range found {
			defined[handle] = true
		}
	}
	return defined, nil
}
