package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/commons/logger"
	"gorm.io/gorm"
)

// preHandleIndexTables lists, child tables first, the index tables of the unprefixed generation that
// predates compact symbol handles: 04–06 before snapshots and documents had ordinals, symbols had
// handles, and symbol_postings was keyed by them. Their rows are not migrated; `uir reindex` rebuilds
// them. location_heads goes too, because every head references a snapshot. The registration tables,
// modules, locations, and primary_locations, keep their rows: modules gains its ordinal, and the other
// two are unchanged in the current schema.
var preHandleIndexTables = []string{
	"symbol_postings", "package_coverage", "documents", "symbols", "source_deltas", "source_revisions",
	"location_heads", "snapshots",
}

// preHandleSQLiteSQL and preHandlePostgresSQL report whether modules exists without its ordinal
// column, the mark of a pre-handle database. Each reads only the catalog, so opening a current or
// empty database adds one cheap statement.
var (
	preHandleSQLiteSQL = fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = '%[1]s')
  AND NOT EXISTS (SELECT 1 FROM pragma_table_info('%[1]s') WHERE name = 'ordinal')`, ModuleRoot{}.TableName())
	preHandlePostgresSQL = fmt.Sprintf(`SELECT EXISTS (
    SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = ? AND c.relname = '%[1]s' AND c.relkind IN ('r', 'p'))
  AND NOT EXISTS (
    SELECT 1 FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = ? AND c.relname = '%[1]s' AND a.attname = 'ordinal' AND NOT a.attisdropped)`, ModuleRoot{}.TableName())
)

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

// isPreHandle reports whether the database holds a pre-handle index.
func isPreHandle(ctx context.Context, database *gorm.DB, schema string) (bool, error) {
	var preHandle bool
	query := database.WithContext(ctx).Raw(preHandleSQLiteSQL)
	if database.Name() != "sqlite" {
		query = database.WithContext(ctx).Raw(preHandlePostgresSQL, schema, schema)
	}
	if err := query.Scan(&preHandle).Error; err != nil {
		return false, fmt.Errorf("detect a pre-handle UIR index: %w", err)
	}
	return preHandle, nil
}

// discardPreHandleGeneration drops a pre-handle index and numbers the registered roots, in one
// transaction. It runs before migration, because the current schema cannot be reconciled onto those
// tables without rewriting their rows. A discarded table that an unmanaged table references refuses the
// whole cutover: SQLite is checked explicitly, and PostgreSQL refuses the DROP of a table another
// object depends on.
func discardPreHandleGeneration(ctx context.Context, database *gorm.DB, schema string) error {
	preHandle, err := isPreHandle(ctx, database, schema)
	if err != nil || !preHandle {
		return err
	}
	if database.Name() == "sqlite" {
		err = cutOverSQLite(ctx, database)
	} else {
		err = cutOverPostgres(ctx, database, schema)
	}
	if err != nil {
		return err
	}
	logger.Warnf("discarded the pre-handle UIR index (%d tables) and kept the registered checkouts; run `uir reindex` to index them again", len(preHandleIndexTables))
	return nil
}

func cutOverPostgres(ctx context.Context, database *gorm.DB, schema string) error {
	modules := `"` + schema + `"."` + ModuleRoot{}.TableName() + `"`
	return database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dropPreHandleIndex(tx, func(table string) string { return `"` + schema + `"."` + table + `"` }); err != nil {
			return err
		}
		return execAll(tx, "number the registered module roots", []string{
			"ALTER TABLE " + modules + " ADD COLUMN ordinal integer",
			"UPDATE " + modules + " AS m SET ordinal = r.ordinal FROM (SELECT id, " + moduleOrdinalSQL + " AS ordinal FROM " + modules + ") AS r WHERE m.id = r.id",
			"ALTER TABLE " + modules + " ALTER COLUMN ordinal SET NOT NULL",
		})
	})
}

func cutOverSQLite(ctx context.Context, database *gorm.DB) error {
	return database.WithContext(ctx).Connection(func(connection *gorm.DB) error {
		return withoutSQLiteForeignKeys(connection, func() error {
			return connection.Transaction(func(tx *gorm.DB) error {
				if err := checkSQLiteReferences(tx, "pre-handle", preHandleIndexTables); err != nil {
					return err
				}
				if err := dropPreHandleIndex(tx, func(table string) string { return `"` + table + `"` }); err != nil {
					return err
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
func withoutSQLiteForeignKeys(connection *gorm.DB, cutover func() error) error {
	if err := connection.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		return fmt.Errorf("suspend SQLite foreign keys for the pre-handle cutover: %w", err)
	}
	cutoverErr := cutover()
	if err := connection.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		return errors.Join(cutoverErr, fmt.Errorf("restore SQLite foreign keys after the pre-handle cutover: %w", err))
	}
	return cutoverErr
}

func dropPreHandleIndex(tx *gorm.DB, qualify func(string) string) error {
	for _, table := range preHandleIndexTables {
		if err := tx.Exec("DROP TABLE " + qualify(table)).Error; err != nil {
			return fmt.Errorf("discard pre-handle table %s: %w", table, err)
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
