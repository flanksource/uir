package indexer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// createImplementsDeltas records how the snapshot's implements facts differ from its base's, as set
// and delete rows, and the facts row that marks them computed with their count. A base published
// before implements facts were snapshot-level has none, so against it every fact is a set.
func createImplementsDeltas(ctx context.Context, database *gorm.DB, snapshot storage.ModuleSnapshot, publication snapshotPublication, handles map[string]int64) error {
	current := make(map[storage.ImplementsPair]bool, len(publication.extraction.implements))
	for _, fact := range publication.extraction.implements {
		typeHandle, typed := handles[fact.typeID]
		interfaceHandle, found := handles[fact.interfaceID]
		if !typed || !found {
			return fmt.Errorf("implements fact %s → %s of snapshot %s has no symbol handle", fact.typeID, fact.interfaceID, snapshot.ID)
		}
		current[storage.ImplementsPair{Type: typeHandle, Interface: interfaceHandle}] = true
	}
	before, err := baseImplements(ctx, database, publication.base.snapshot.ID)
	if err != nil {
		return err
	}
	key := storage.ImplementsDelta{SnapshotOrdinal: snapshot.Ordinal, RootOrdinal: publication.root.Ordinal}
	if rows := implementsDeltaRows(key, before, current); len(rows) > 0 {
		if err := database.WithContext(ctx).CreateInBatches(rows, publicationBatch).Error; err != nil {
			return fmt.Errorf("create %d implements deltas for snapshot %s: %w", len(rows), snapshot.ID, err)
		}
	}
	facts := storage.ImplementsFacts{SnapshotOrdinal: snapshot.Ordinal, RootOrdinal: publication.root.Ordinal, PairCount: int64(len(current))}
	if err := database.WithContext(ctx).Create(&facts).Error; err != nil {
		return fmt.Errorf("record the implements facts of snapshot %s: %w", snapshot.ID, err)
	}
	return nil
}

// baseImplements is the base snapshot's effective implements facts: none without a base, or for a base
// that predates snapshot-level facts.
func baseImplements(ctx context.Context, database *gorm.DB, base uuid.UUID) (map[storage.ImplementsPair]bool, error) {
	before := map[storage.ImplementsPair]bool{}
	if base == uuid.Nil {
		return before, nil
	}
	pairs, err := storage.EffectiveImplements(ctx, database, base, storage.ImplementsFilter{})
	if errors.Is(err, storage.ErrNoImplementsFacts) {
		return before, nil
	}
	if err != nil {
		return nil, err
	}
	for _, pair := range pairs {
		before[pair] = true
	}
	return before, nil
}

// implementsDeltaRows sets every pair only after holds and deletes every pair only before holds,
// sorted by type and interface handle; every row carries the snapshot and root ordinals of key.
func implementsDeltaRows(key storage.ImplementsDelta, before, after map[storage.ImplementsPair]bool) []storage.ImplementsDelta {
	var rows []storage.ImplementsDelta
	for pair := range after {
		if !before[pair] {
			rows = append(rows, implementsDelta(key, pair, storage.SourceSet))
		}
	}
	for pair := range before {
		if !after[pair] {
			rows = append(rows, implementsDelta(key, pair, storage.SourceDelete))
		}
	}
	slices.SortFunc(rows, func(left, right storage.ImplementsDelta) int {
		return cmp.Or(cmp.Compare(left.TypeHandle, right.TypeHandle), cmp.Compare(left.InterfaceHandle, right.InterfaceHandle))
	})
	return rows
}

func implementsDelta(key storage.ImplementsDelta, pair storage.ImplementsPair, operation storage.SourceOperation) storage.ImplementsDelta {
	key.TypeHandle, key.InterfaceHandle, key.Operation = pair.Type, pair.Interface, operation
	return key
}
