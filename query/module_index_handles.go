package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
)

// symbolHandles maps the symbol ids one query touches to their handles and back. Symbol rows carry
// their handle, so only ids first seen outside a loaded row, such as an owner id, cost a lookup.
type symbolHandles struct {
	byID     map[string]int64
	byHandle map[int64]string
}

func newSymbolHandles() symbolHandles {
	return symbolHandles{byID: map[string]int64{}, byHandle: map[int64]string{}}
}

func (handles symbolHandles) add(id string, handle int64) error {
	if known, found := handles.byID[id]; found && known != handle {
		return fmt.Errorf("symbol %s has handle %d, earlier %d", id, handle, known)
	}
	if known, found := handles.byHandle[handle]; found && known != id {
		return fmt.Errorf("handle %d names symbol %s, earlier %s", handle, id, known)
	}
	handles.byID[id], handles.byHandle[handle] = handle, id
	return nil
}

// remember records the handles of fully loaded symbol rows.
func (index *indexContext) remember(rows []storage.Symbol) error {
	for _, row := range rows {
		if err := index.handles.add(row.ID, row.Handle); err != nil {
			return err
		}
	}
	return nil
}

// handlesOf maps symbol ids to their sorted, distinct handles, reading only the ids this query has not
// seen yet; an unknown id is an error.
func (index *indexContext) handlesOf(ctx context.Context, ids []string) ([]int64, error) {
	var missing []string
	for _, id := range ids {
		if _, known := index.handles.byID[id]; !known {
			missing = append(missing, id)
		}
	}
	slices.Sort(missing)
	loaded, err := storage.SymbolHandles(ctx, index.database, slices.Compact(missing))
	if err != nil {
		return nil, err
	}
	for id, handle := range loaded {
		if err := index.handles.add(id, handle); err != nil {
			return nil, err
		}
	}
	handles := make([]int64, 0, len(ids))
	for _, id := range ids {
		handles = append(handles, index.handles.byID[id])
	}
	slices.Sort(handles)
	return slices.Compact(handles), nil
}

// definedBy is the subset of handles that one scope's snapshot defines, from its symbol deltas. A
// handle is defined exactly when a document active in the snapshot declares it, which is when the
// snapshot has a definition posting for it. Membership is remembered per snapshot, so a handle is read
// at most once per query.
func (index *indexContext) definedBy(ctx context.Context, scope int, handles []int64) (map[int64]bool, error) {
	snapshot := index.scopes[scope].snapshot.ID
	known := index.defined[snapshot]
	if known == nil {
		known = map[int64]bool{}
		index.defined[snapshot] = known
	}
	var unknown []int64
	for _, handle := range handles {
		if _, seen := known[handle]; !seen {
			unknown = append(unknown, handle)
		}
	}
	if len(unknown) > 0 {
		found, err := storage.DefinedSymbols(ctx, index.database, snapshot, unknown)
		if err != nil {
			return nil, err
		}
		for _, handle := range unknown {
			known[handle] = false
		}
		for _, handle := range found {
			known[handle] = true
		}
	}
	defined := map[int64]bool{}
	for _, handle := range handles {
		if known[handle] {
			defined[handle] = true
		}
	}
	return defined, nil
}

// knownUndefined reports whether every scope of the root is already known not to define the handle,
// which rules out a definition posting in the root's active documents without reading postings.
func (index *indexContext) knownUndefined(root rootDocuments, handle int64) bool {
	for _, position := range root.scopes {
		if defined, known := index.defined[index.scopes[position].snapshot.ID][handle]; !known || defined {
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
