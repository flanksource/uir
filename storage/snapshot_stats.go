package storage

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SnapshotStats is a snapshot's size, from its effective documents and sources, and its change
// against its base snapshot, from the source and symbol delta rows it published. A snapshot without
// a base counts every file as added.
type SnapshotStats struct {
	FileCount       int64 `json:"file_count"`
	SymbolCount     int64 `json:"symbol_count"`
	OccurrenceCount int64 `json:"occurrence_count"`
	SourceBytes     int64 `json:"source_bytes"`
	FilesAdded      int   `json:"files_added"`
	FilesChanged    int   `json:"files_changed"`
	FilesDeleted    int   `json:"files_deleted"`
	SymbolsChanged  int   `json:"symbols_changed"`
}

// Stats reads the snapshot's recorded counts, failing for a row whose counts were never recorded.
func (snapshot ModuleSnapshot) Stats() (SnapshotStats, error) {
	if snapshot.FileCount == nil || snapshot.SymbolCount == nil || snapshot.OccurrenceCount == nil || snapshot.SourceBytes == nil ||
		snapshot.FilesAdded == nil || snapshot.FilesChanged == nil || snapshot.FilesDeleted == nil || snapshot.SymbolsChanged == nil {
		return SnapshotStats{}, fmt.Errorf("snapshot %s has no recorded size and change counts", snapshot.ID)
	}
	return SnapshotStats{
		FileCount: *snapshot.FileCount, SymbolCount: *snapshot.SymbolCount, OccurrenceCount: *snapshot.OccurrenceCount,
		SourceBytes: *snapshot.SourceBytes, FilesAdded: *snapshot.FilesAdded, FilesChanged: *snapshot.FilesChanged,
		FilesDeleted: *snapshot.FilesDeleted, SymbolsChanged: *snapshot.SymbolsChanged,
	}, nil
}

// SnapshotStatsOptions is what a snapshot's stats are counted from: the effective sources of its base
// snapshot, empty for a snapshot without a base, and every file the snapshot serves. Publication
// supplies what it already holds; every other caller derives them with DeriveSnapshotStatsOptions.
type SnapshotStatsOptions struct {
	Base  map[string]SourceRevision
	Files map[string]SnapshotFile
}

// SnapshotFile is one path a snapshot serves: its effective source revision and the counts of the
// document the snapshot activates for it.
type SnapshotFile struct {
	Source          SourceRevision
	SymbolCount     int
	OccurrenceCount int
}

// RecordSnapshotStats counts a published snapshot's stats from options and the symbol deltas its
// publication wrote, and stores them on the snapshot row. Publication calls it inside its transaction,
// after every row is written; the storage backfill and prune's rebase call it with derived options.
func RecordSnapshotStats(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID, options SnapshotStatsOptions) (SnapshotStats, error) {
	if options.Base == nil || options.Files == nil {
		return SnapshotStats{}, fmt.Errorf("record stats of snapshot %s: base sources and files are required, empty when there are none", snapshotID)
	}
	stats := countSnapshotStats(options)
	var symbols int64
	ordinal := database.WithContext(ctx).Model(&ModuleSnapshot{}).Select("ordinal").Where("id = ?", snapshotID)
	if err := database.WithContext(ctx).Model(&SymbolDelta{}).Where("snapshot_ordinal = (?)", ordinal).Count(&symbols).Error; err != nil {
		return SnapshotStats{}, fmt.Errorf("count symbol deltas of snapshot %s: %w", snapshotID, err)
	}
	stats.SymbolsChanged = int(symbols)
	updated := database.WithContext(ctx).Model(&ModuleSnapshot{}).Where("id = ?", snapshotID).Updates(map[string]any{
		"file_count": stats.FileCount, "symbol_count": stats.SymbolCount, "occurrence_count": stats.OccurrenceCount,
		"source_bytes": stats.SourceBytes, "files_added": stats.FilesAdded, "files_changed": stats.FilesChanged,
		"files_deleted": stats.FilesDeleted, "symbols_changed": stats.SymbolsChanged,
	})
	if updated.Error != nil {
		return SnapshotStats{}, fmt.Errorf("record stats of snapshot %s: %w", snapshotID, updated.Error)
	}
	if updated.RowsAffected != 1 {
		return SnapshotStats{}, fmt.Errorf("record stats of snapshot %s: updated %d rows", snapshotID, updated.RowsAffected)
	}
	return stats, nil
}

// DeriveSnapshotStatsOptions derives a stored snapshot's stats inputs from its rows: its active
// documents, and its base's effective sources when it has a base.
func DeriveSnapshotStatsOptions(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID) (SnapshotStatsOptions, error) {
	documents, err := ActiveDocuments(ctx, database, snapshotID, ActiveDocumentOptions{})
	if err != nil {
		return SnapshotStatsOptions{}, fmt.Errorf("size of snapshot %s: %w", snapshotID, err)
	}
	options := SnapshotStatsOptions{Base: map[string]SourceRevision{}, Files: make(map[string]SnapshotFile, len(documents))}
	for path, active := range documents {
		options.Files[path] = SnapshotFile{Source: active.Source, SymbolCount: active.Document.SymbolCount, OccurrenceCount: active.Document.OccurrenceCount}
	}
	var snapshot ModuleSnapshot
	if err := database.WithContext(ctx).Select("id", "base_snapshot_id").Where("id = ?", snapshotID).Take(&snapshot).Error; err != nil {
		return SnapshotStatsOptions{}, fmt.Errorf("load snapshot %s: %w", snapshotID, err)
	}
	if snapshot.BaseSnapshotID != nil {
		if options.Base, err = EffectiveSources(ctx, database, *snapshot.BaseSnapshotID); err != nil {
			return SnapshotStatsOptions{}, fmt.Errorf("base of snapshot %s: %w", snapshotID, err)
		}
	}
	return options, nil
}

// countSnapshotStats sizes the snapshot from its files and classifies them against its base: a path
// only the snapshot serves is added, one it serves at another revision is changed, and one only the
// base serves is deleted. These are exactly the paths of the snapshot's set and delete source deltas.
func countSnapshotStats(options SnapshotStatsOptions) SnapshotStats {
	var stats SnapshotStats
	for path, file := range options.Files {
		stats.FileCount++
		stats.SymbolCount += int64(file.SymbolCount)
		stats.OccurrenceCount += int64(file.OccurrenceCount)
		stats.SourceBytes += file.Source.SizeBytes
		prior, existed := options.Base[path]
		switch {
		case !existed:
			stats.FilesAdded++
		case prior.ID != file.Source.ID:
			stats.FilesChanged++
		}
	}
	for path := range options.Base {
		if _, served := options.Files[path]; !served {
			stats.FilesDeleted++
		}
	}
	return stats
}

// recordDerivedSnapshotStats records a stored snapshot's stats from options derived from its rows.
func recordDerivedSnapshotStats(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID) error {
	options, err := DeriveSnapshotStatsOptions(ctx, database, snapshotID)
	if err != nil {
		return err
	}
	_, err = RecordSnapshotStats(ctx, database, snapshotID, options)
	return err
}

// backfillSnapshotStats records the stats of every snapshot published before they were recorded. A
// row is recorded once, so reopening a backfilled database costs one query.
func backfillSnapshotStats(ctx context.Context, database *gorm.DB) error {
	var pending []uuid.UUID
	if err := database.WithContext(ctx).Model(&ModuleSnapshot{}).Where("file_count IS NULL").Order("ordinal").Pluck("id", &pending).Error; err != nil {
		return fmt.Errorf("find snapshots without stats: %w", err)
	}
	for _, id := range pending {
		if err := recordDerivedSnapshotStats(ctx, database, id); err != nil {
			return fmt.Errorf("backfill snapshot stats: %w", err)
		}
	}
	return nil
}
