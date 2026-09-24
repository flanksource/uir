package storage

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// legacyTables lists every table a previous schema generation created, child tables first.
// The uir_module_* and uir_source_* tables are the module-root generation that the unprefixed
// base schema replaces; the rest are the older project generation. Their data is not migrated.
var legacyTables = []string{
	"uir_source_deltas", "uir_module_location_heads", "uir_module_primaries",
	"uir_source_revisions", "uir_module_snapshots", "uir_module_locations", "uir_module_roots",
	"uir_relationships", "uir_fields", "uir_node_locations", "uir_nodes",
	"uir_sources", "uir_roots", "uir_project_heads", "uir_snapshots", "uir_projects",
}

func discardLegacyTables(ctx context.Context, database *gorm.DB) error {
	return database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Name() == "sqlite" {
			if err := checkLegacySQLiteReferences(tx); err != nil {
				return err
			}
		}
		for _, table := range legacyTables {
			if !tx.Migrator().HasTable(table) {
				continue
			}
			if err := tx.Exec("DROP TABLE " + table).Error; err != nil {
				return fmt.Errorf("discard legacy table %s: %w", table, err)
			}
		}
		return nil
	})
}

func checkLegacySQLiteReferences(tx *gorm.DB) error {
	var tables []string
	if err := tx.Raw("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'").Scan(&tables).Error; err != nil {
		return fmt.Errorf("list SQLite tables before legacy cutover: %w", err)
	}
	legacy := make(map[string]bool, len(legacyTables))
	for _, name := range legacyTables {
		legacy[name] = true
	}
	for _, table := range tables {
		if legacy[table] {
			continue
		}
		var references []struct {
			Table string `gorm:"column:table"`
		}
		quoted := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
		if err := tx.Raw("PRAGMA foreign_key_list(" + quoted + ")").Scan(&references).Error; err != nil {
			return fmt.Errorf("inspect foreign keys of SQLite table %s: %w", table, err)
		}
		for _, reference := range references {
			if legacy[reference.Table] {
				return fmt.Errorf("cannot discard legacy table %s: external table %s references it", reference.Table, table)
			}
		}
	}
	return nil
}
