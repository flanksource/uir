package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
)

// implementsKey is one handle an implements lookup asked about, on the side it names.
type implementsKey struct {
	side   storage.ImplementsSide
	handle int64
}

// implementsOf is the implements facts of one scope's snapshot whose side is one of handles. A
// snapshot's facts are read once per handle and side and kept by the pipeline; a snapshot published
// before implements facts were snapshot-level is an error asking for a reindex.
func (index *indexContext) implementsOf(ctx context.Context, scope int, side storage.ImplementsSide, handles []int64) ([]storage.ImplementsPair, error) {
	facts := index.scopes[scope].facts
	var unknown []int64
	var found []storage.ImplementsPair
	facts.mu.Lock()
	for _, handle := range handles {
		if pairs, seen := facts.implements[implementsKey{side, handle}]; seen {
			found = append(found, pairs...)
		} else {
			unknown = append(unknown, handle)
		}
	}
	facts.mu.Unlock()
	if len(unknown) == 0 {
		return found, nil
	}
	pairs, err := storage.EffectiveImplements(ctx, index.database, index.scopes[scope].snapshot.ID, storage.ImplementsFilter{By: side, Handles: unknown})
	if err != nil {
		return nil, err
	}
	grouped := map[int64][]storage.ImplementsPair{}
	for _, pair := range pairs {
		handle := pair.Interface
		if side == storage.ImplementsByType {
			handle = pair.Type
		}
		grouped[handle] = append(grouped[handle], pair)
	}
	facts.mu.Lock()
	defer facts.mu.Unlock()
	for _, handle := range unknown {
		facts.implements[implementsKey{side, handle}] = grouped[handle]
	}
	return append(found, pairs...), nil
}

// implementedAnywhere is the subset of interface handles that a type of at least one scope implements.
func (index *indexContext) implementedAnywhere(ctx context.Context, interfaces []int64) (map[int64]bool, error) {
	implemented := map[int64]bool{}
	if len(interfaces) == 0 {
		return implemented, nil
	}
	for position := range index.scopes {
		pairs, err := index.implementsOf(ctx, position, storage.ImplementsByInterface, interfaces)
		if err != nil {
			return nil, err
		}
		for _, pair := range pairs {
			implemented[pair.Interface] = true
		}
	}
	return implemented, nil
}

// symbolIDs maps handles to their symbol ids, sorted and distinct, reading the rows the pipeline has
// not seen; a handle without a row is an error.
func (index *indexContext) symbolIDs(ctx context.Context, handles []int64) ([]string, error) {
	var ids []string
	var missing []int64
	for _, handle := range handles {
		if id, known := index.symbols.id(handle); known {
			ids = append(ids, id)
		} else {
			missing = append(missing, handle)
		}
	}
	slices.Sort(missing)
	missing = slices.Compact(missing)
	for start := 0; start < len(missing); start += lookupBatch {
		batch := missing[start:min(start+lookupBatch, len(missing))]
		var rows []storage.Symbol
		if err := index.database.WithContext(ctx).Where("handle IN ?", batch).Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load symbols by handle: %w", err)
		}
		if len(rows) != len(batch) {
			return nil, fmt.Errorf("loaded %d of %d symbols by handle", len(rows), len(batch))
		}
		if err := index.remember(rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}
