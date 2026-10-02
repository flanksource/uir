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

// RecordSnapshotStats derives a published snapshot's stats from the rows its publication wrote and
// stores them on the snapshot row. Publication calls it inside its transaction, after every row is
// written; the storage backfill calls it for rows published before stats were recorded.
func RecordSnapshotStats(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID) (SnapshotStats, error) {
	stats, err := deriveSnapshotStats(ctx, database, snapshotID)
	if err != nil {
		return SnapshotStats{}, err
	}
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

func deriveSnapshotStats(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID) (SnapshotStats, error) {
	documents, err := ActiveDocuments(ctx, database, snapshotID, ActiveDocumentOptions{})
	if err != nil {
		return SnapshotStats{}, fmt.Errorf("size of snapshot %s: %w", snapshotID, err)
	}
	var stats SnapshotStats
	for _, active := range documents {
		stats.FileCount++
		stats.SymbolCount += int64(active.Document.SymbolCount)
		stats.OccurrenceCount += int64(active.Document.OccurrenceCount)
		stats.SourceBytes += active.Source.SizeBytes
	}
	var snapshot ModuleSnapshot
	if err := database.WithContext(ctx).Select("id", "base_snapshot_id", "ordinal").Where("id = ?", snapshotID).Take(&snapshot).Error; err != nil {
		return SnapshotStats{}, fmt.Errorf("load snapshot %s: %w", snapshotID, err)
	}
	if err := countSourceChanges(ctx, database, snapshot, &stats); err != nil {
		return SnapshotStats{}, err
	}
	var symbols int64
	if err := database.WithContext(ctx).Model(&SymbolDelta{}).Where("snapshot_ordinal = ?", snapshot.Ordinal).Count(&symbols).Error; err != nil {
		return SnapshotStats{}, fmt.Errorf("count symbol deltas of snapshot %s: %w", snapshotID, err)
	}
	stats.SymbolsChanged = int(symbols)
	return stats, nil
}

// countSourceChanges classifies the snapshot's source deltas against its base's effective sources: a
// delete is a deleted file, and a set is a changed file when the base has the path, else an added one.
func countSourceChanges(ctx context.Context, database *gorm.DB, snapshot ModuleSnapshot, stats *SnapshotStats) error {
	base := map[string]SourceRevision{}
	if snapshot.BaseSnapshotID != nil {
		var err error
		if base, err = EffectiveSources(ctx, database, *snapshot.BaseSnapshotID); err != nil {
			return fmt.Errorf("base of snapshot %s: %w", snapshot.ID, err)
		}
	}
	var deltas []SourceDelta
	if err := database.WithContext(ctx).Where("snapshot_id = ?", snapshot.ID).Find(&deltas).Error; err != nil {
		return fmt.Errorf("load source deltas of snapshot %s: %w", snapshot.ID, err)
	}
	for _, delta := range deltas {
		_, existed := base[delta.PathKey]
		switch {
		case delta.Operation == SourceDelete:
			stats.FilesDeleted++
		case existed:
			stats.FilesChanged++
		default:
			stats.FilesAdded++
		}
	}
	return nil
}

// backfillSnapshotStats records the stats of every snapshot published before they were recorded. A
// row is recorded once, so reopening a backfilled database costs one query.
func backfillSnapshotStats(ctx context.Context, database *gorm.DB) error {
	var pending []uuid.UUID
	if err := database.WithContext(ctx).Model(&ModuleSnapshot{}).Where("file_count IS NULL").Order("ordinal").Pluck("id", &pending).Error; err != nil {
		return fmt.Errorf("find snapshots without stats: %w", err)
	}
	for _, id := range pending {
		if _, err := RecordSnapshotStats(ctx, database, id); err != nil {
			return fmt.Errorf("backfill snapshot stats: %w", err)
		}
	}
	return nil
}
