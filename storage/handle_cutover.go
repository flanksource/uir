package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/uir/storage/symbolhandle"
	"gorm.io/gorm"
)

// SymbolLayout is the one row that records the bit layout of the database's symbol handles.
type SymbolLayout struct {
	ID     int32  `gorm:"column:id;primaryKey;autoIncrement:false"`
	Layout string `gorm:"column:layout"`
}

func (SymbolLayout) TableName() string { return "symbol_layout" }

// indexGeneration is the shape of the index a database holds when it is opened.
type indexGeneration int

const (
	// generationEmpty has no modules table: nothing to cut over.
	generationEmpty indexGeneration = iota
	// generationPreHandle predates compact symbol handles: modules has no ordinal.
	generationPreHandle
	// generationH64a has compact handles in the H64a layout and no symbol_layout row.
	generationH64a
	// generationCurrent records symbolhandle.Layout.
	generationCurrent
)

// generationNames name a discarded generation in errors and in the cutover warning.
var generationNames = map[indexGeneration]string{generationPreHandle: "pre-handle", generationH64a: "H64a"}

// indexTables lists, child tables first, every index table a cutover discards. Their rows are not
// migrated; `uir reindex` rebuilds them. A table a generation never had is skipped, so the pre-handle
// generation (04–06 before ordinals and handles) and H64a (handles packed with 3 kind bits) share the
// list. The registration tables, modules, locations, and primary_locations, keep their rows, and so do
// task_runs and symbol_kinds, which describe no handle.
var indexTables = []string{
	"symbol_deltas", "symbol_postings", "snapshot_dependencies", "package_coverage", "documents", "symbols", "symbol_packages",
	"symbol_modules", "source_deltas", "source_revisions", "location_heads", "snapshots", "source_blobs",
}

// generationSQLiteSQL and generationPostgresSQL report whether modules exists, whether it has its
// ordinal column, and whether symbol_layout exists. Each reads only the catalog, so opening a current
// or empty database adds this one statement and one read of the layout row.
var (
	generationSQLiteSQL = fmt.Sprintf(`SELECT
  EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = '%[1]s') AS has_modules,
  EXISTS (SELECT 1 FROM pragma_table_info('%[1]s') WHERE name = 'ordinal') AS has_ordinal,
  EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = '%[2]s') AS has_layout`, ModuleRoot{}.TableName(), SymbolLayout{}.TableName())
	generationPostgresSQL = fmt.Sprintf(`SELECT
  EXISTS (SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = @schema AND c.relname = '%[1]s' AND c.relkind IN ('r', 'p')) AS has_modules,
  EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = @schema AND c.relname = '%[1]s' AND a.attname = 'ordinal' AND NOT a.attisdropped) AS has_ordinal,
  EXISTS (SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = @schema AND c.relname = '%[2]s' AND c.relkind IN ('r', 'p')) AS has_layout`, ModuleRoot{}.TableName(), SymbolLayout{}.TableName())
)

// detectGeneration classifies the database's index. A recorded layout other than symbolhandle.Layout
// is an error: it was written by another uir version, and discarding its index would be a guess.
func detectGeneration(ctx context.Context, database *gorm.DB, schema string) (indexGeneration, error) {
	var catalog struct {
		HasModules bool `gorm:"column:has_modules"`
		HasOrdinal bool `gorm:"column:has_ordinal"`
		HasLayout  bool `gorm:"column:has_layout"`
	}
	query := database.WithContext(ctx).Raw(generationSQLiteSQL)
	if database.Name() != "sqlite" {
		query = database.WithContext(ctx).Raw(generationPostgresSQL, map[string]any{"schema": schema})
	}
	if err := query.Scan(&catalog).Error; err != nil {
		return 0, fmt.Errorf("detect the UIR index generation: %w", err)
	}
	switch {
	case !catalog.HasModules:
		return generationEmpty, nil
	case !catalog.HasOrdinal:
		return generationPreHandle, nil
	case !catalog.HasLayout:
		return generationH64a, nil
	}
	var layouts []string
	if err := database.WithContext(ctx).Model(&SymbolLayout{}).Pluck("layout", &layouts).Error; err != nil {
		return 0, fmt.Errorf("read the symbol handle layout: %w", err)
	}
	switch {
	case len(layouts) == 0:
		return generationH64a, nil
	case layouts[0] != symbolhandle.Layout:
		return 0, fmt.Errorf("database records symbol handle layout %q; this uir writes %q", layouts[0], symbolhandle.Layout)
	}
	return generationCurrent, nil
}

// recordHandleLayout writes symbolhandle.Layout into a database that has none, after migration created
// symbol_layout; the cutover has already discarded any index packed with another layout.
func recordHandleLayout(ctx context.Context, database *gorm.DB) error {
	layout := SymbolLayout{ID: 1, Layout: symbolhandle.Layout}
	if err := database.WithContext(ctx).Where(SymbolLayout{ID: 1}).FirstOrCreate(&layout).Error; err != nil {
		return fmt.Errorf("record the symbol handle layout: %w", err)
	}
	if layout.Layout != symbolhandle.Layout {
		return fmt.Errorf("database records symbol handle layout %q; this uir writes %q", layout.Layout, symbolhandle.Layout)
	}
	return nil
}

// moduleOrdinalSQL numbers the registered roots densely from 1 by created_at, then root_key.
const moduleOrdinalSQL = "ROW_NUMBER() OVER (ORDER BY created_at, root_key)"

// sqliteModulesSQL rebuilds modules with its ordinal, since SQLite can add neither a NOT NULL column
// nor a check to a populated table. The CREATE TABLE is the table 04_module_roots.hcl declares, as
// commons-db projects it for SQLite, so the migration that follows finds it current and only adds the
// modules_root_key_key and modules_ordinal_key unique indexes. The locations foreign key names
// modules, so it binds to the rebuilt table.
var sqliteModulesSQL = []string{
	"CREATE TABLE `modules_with_ordinal` (`id` text NOT NULL, `root_key` text NOT NULL, `name` text NOT NULL, `created_at` datetime NOT NULL, `ordinal` integer NOT NULL, " +
		"PRIMARY KEY (`id`), CONSTRAINT `modules_root_key_check` CHECK (length(root_key) > 0), CONSTRAINT `modules_ordinal_check` CHECK (ordinal >= 1))",
	"INSERT INTO `modules_with_ordinal` (id, root_key, name, created_at, ordinal) SELECT id, root_key, name, created_at, " + moduleOrdinalSQL + " FROM modules",
	"DROP TABLE modules",
	"ALTER TABLE `modules_with_ordinal` RENAME TO `modules`",
}

// discardStaleGeneration drops a pre-handle or H64a index in one transaction, and numbers the
// registered roots of a pre-handle one. It runs before migration, because the current schema cannot be
// reconciled onto those tables without rewriting their rows, and their handles mean nothing in H64b. A
// discarded table that an unmanaged table references refuses the whole cutover: SQLite is checked
// explicitly, and PostgreSQL refuses the DROP of a table another object depends on.
func discardStaleGeneration(ctx context.Context, database *gorm.DB, schema string) error {
	generation, err := detectGeneration(ctx, database, schema)
	if err != nil || generation == generationEmpty || generation == generationCurrent {
		return err
	}
	if database.Name() == "sqlite" {
		err = cutOverSQLite(ctx, database, generation)
	} else {
		err = cutOverPostgres(ctx, database, schema, generation)
	}
	if err != nil {
		return err
	}
	logger.Warnf("discarded the %s UIR index and kept the registered checkouts; run `uir reindex` to index them again", generationNames[generation])
	return nil
}

func cutOverPostgres(ctx context.Context, database *gorm.DB, schema string, generation indexGeneration) error {
	modules := `"` + schema + `"."` + ModuleRoot{}.TableName() + `"`
	return database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dropIndexTables(tx, generation, func(table string) string { return `"` + schema + `"."` + table + `"` }); err != nil {
			return err
		}
		if generation != generationPreHandle {
			return nil
		}
		return execAll(tx, "number the registered module roots", []string{
			"ALTER TABLE " + modules + " ADD COLUMN ordinal integer",
			"UPDATE " + modules + " AS m SET ordinal = r.ordinal FROM (SELECT id, " + moduleOrdinalSQL + " AS ordinal FROM " + modules + ") AS r WHERE m.id = r.id",
			"ALTER TABLE " + modules + " ALTER COLUMN ordinal SET NOT NULL",
		})
	})
}

func cutOverSQLite(ctx context.Context, database *gorm.DB, generation indexGeneration) error {
	return database.WithContext(ctx).Connection(func(connection *gorm.DB) error {
		return withoutSQLiteForeignKeys(connection, generation, func() error {
			return connection.Transaction(func(tx *gorm.DB) error {
				if err := checkSQLiteReferences(tx, generationNames[generation], indexTables); err != nil {
					return err
				}
				if err := dropIndexTables(tx, generation, func(table string) string { return `"` + table + `"` }); err != nil {
					return err
				}
				if generation != generationPreHandle {
					return nil
				}
				return execAll(tx, "number the registered module roots", sqliteModulesSQL)
			})
		})
	})
}

// withoutSQLiteForeignKeys runs cutover with foreign key enforcement off on this one connection and
// turns it back on afterwards. With enforcement on, SQLite's DROP TABLE deletes every row first and
// checks each against its referencing tables; symbols references itself through owner_id, which has
// no index, so that check is quadratic (minutes on an indexed corpus), and the modules rebuild could
// not drop the table locations references. The caller has already refused any table outside the
// dropped set that references one inside it, and modules keeps every id, so nothing is left dangling.
func withoutSQLiteForeignKeys(connection *gorm.DB, generation indexGeneration, cutover func() error) error {
	if err := connection.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		return fmt.Errorf("suspend SQLite foreign keys for the %s cutover: %w", generationNames[generation], err)
	}
	cutoverErr := cutover()
	if err := connection.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		return errors.Join(cutoverErr, fmt.Errorf("restore SQLite foreign keys after the %s cutover: %w", generationNames[generation], err))
	}
	return cutoverErr
}

func dropIndexTables(tx *gorm.DB, generation indexGeneration, qualify func(string) string) error {
	for _, table := range indexTables {
		if err := tx.Exec("DROP TABLE IF EXISTS " + qualify(table)).Error; err != nil {
			return fmt.Errorf("discard %s table %s: %w", generationNames[generation], table, err)
		}
	}
	return nil
}

func execAll(tx *gorm.DB, subject string, statements []string) error {
	for _, statement := range statements {
		if err := tx.Exec(statement).Error; err != nil {
			return fmt.Errorf("%s: %w", subject, err)
		}
	}
	return nil
}
