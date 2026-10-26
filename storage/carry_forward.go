package storage

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

var carryCoverageSQL = fmt.Sprintf(`INSERT INTO %[1]s (snapshot_id, root_id, package_path, input_hash, export_shape_hash, coverage, file_count, diagnostics)
SELECT ?, root_id, package_path, input_hash, export_shape_hash, coverage, file_count, diagnostics FROM %[1]s WHERE snapshot_id = ?`, PackageCoverage{}.TableName())

// CarryForward gives snapshot, published on base with no source or document delta rows, the base's
// package coverage rows, copied in the database, and its size, with nothing added, changed, or
// deleted; the base's sources and documents are never read. A base whose coverage rows do not match
// its package count, or whose stats were never recorded, is an error.
func CarryForward(ctx context.Context, database *gorm.DB, base, snapshot ModuleSnapshot) error {
	if snapshot.BaseSnapshotID == nil || *snapshot.BaseSnapshotID != base.ID || snapshot.RootID != base.RootID {
		return fmt.Errorf("carry snapshot %s forward to %s: it is not a snapshot of root %s on that base", base.ID, snapshot.ID, base.RootID)
	}
	stats, err := base.Stats()
	if err != nil {
		return fmt.Errorf("carry snapshot %s forward to %s: %w", base.ID, snapshot.ID, err)
	}
	copied := database.WithContext(ctx).Exec(carryCoverageSQL, snapshot.ID, base.ID)
	if copied.Error != nil {
		return fmt.Errorf("carry package coverage of snapshot %s forward to %s: %w", base.ID, snapshot.ID, copied.Error)
	}
	if copied.RowsAffected != int64(base.PackageCount) {
		return fmt.Errorf("carry package coverage of snapshot %s forward to %s: copied %d rows, package_count is %d", base.ID, snapshot.ID, copied.RowsAffected, base.PackageCount)
	}
	updated := database.WithContext(ctx).Model(&ModuleSnapshot{}).Where("id = ?", snapshot.ID).Updates(map[string]any{
		"file_count": stats.FileCount, "symbol_count": stats.SymbolCount, "occurrence_count": stats.OccurrenceCount,
		"source_bytes": stats.SourceBytes, "files_added": 0, "files_changed": 0, "files_deleted": 0, "symbols_changed": 0,
	})
	if updated.Error != nil {
		return fmt.Errorf("record stats of carried snapshot %s: %w", snapshot.ID, updated.Error)
	}
	if updated.RowsAffected != 1 {
		return fmt.Errorf("record stats of carried snapshot %s: updated %d rows", snapshot.ID, updated.RowsAffected)
	}
	return nil
}
