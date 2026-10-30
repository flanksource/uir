package indexer

import (
	"context"
	"fmt"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

// publishCarried publishes a delta that changes nothing as a snapshot on its base with no source,
// document, symbol, or implements delta rows, which is what a full publication of the base's documents
// writes: readers resolve every path through the base. Its package coverage and stats are the base's,
// copied in the database, so no document, source, or content is read.
func publishCarried(ctx context.Context, database *gorm.DB, publication snapshotPublication, result *ModuleResult) (storage.ModuleSnapshot, error) {
	carried := publication.extraction.carried
	if carried.snapshot.ID != publication.base.snapshot.ID {
		return storage.ModuleSnapshot{}, fmt.Errorf("publish module %q: carried snapshot %s is not the publication base %s", publication.root.RootKey, carried.snapshot.ID, publication.base.snapshot.ID)
	}
	snapshot := publication.snapshot()
	snapshot.TaskRunID, snapshot.PackageCount = taskRunID(ctx), carried.snapshot.PackageCount
	if err := storage.CreateSnapshot(ctx, database, &snapshot); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	var err error
	if result.Warnings, err = snapshotWarnings(snapshot.Diagnostics); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	if err := Phase(ctx, "carry-forward", func(ctx context.Context) error {
		if err := storage.CarryForward(ctx, database, carried.snapshot, snapshot); err != nil {
			return err
		}
		return carryImplements(ctx, database, carried.snapshot, snapshot, publication.root.Ordinal)
	}); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	result.ReusedFiles = int(carried.stats.FileCount)
	if err := publication.advance(ctx, database, snapshot, result); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	return snapshot, nil
}

// carryImplements records the carried snapshot's implements facts: an external producer publishes
// none, so its base holds none and the snapshot records zero pairs with no delta rows.
func carryImplements(ctx context.Context, database *gorm.DB, base, snapshot storage.ModuleSnapshot, rootOrdinal int32) error {
	var prior []storage.ImplementsFacts
	if err := database.WithContext(ctx).Where("snapshot_ordinal = ?", base.Ordinal).Find(&prior).Error; err != nil {
		return fmt.Errorf("load the implements facts of base snapshot %s: %w", base.ID, err)
	}
	if len(prior) == 1 && prior[0].PairCount != 0 {
		return fmt.Errorf("carry snapshot %s forward: its external base holds %d implements pairs, an external publication has none", base.ID, prior[0].PairCount)
	}
	facts := storage.ImplementsFacts{SnapshotOrdinal: snapshot.Ordinal, RootOrdinal: rootOrdinal}
	if err := database.WithContext(ctx).Create(&facts).Error; err != nil {
		return fmt.Errorf("record the implements facts of snapshot %s: %w", snapshot.ID, err)
	}
	return nil
}
