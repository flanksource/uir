// Package storage persists immutable UIR snapshots through GORM.
package storage

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"

	commonsdb "github.com/flanksource/commons-db/db"
	commonsmigrate "github.com/flanksource/commons-db/migrate"
	"gorm.io/gorm"
)

const defaultSchema = "public"

type DBOptions struct {
	DSN    string
	Schema string
}

//go:embed migrations/04_module_roots.hcl migrations/05_source_deltas.hcl migrations/06_symbol_index.hcl migrations/07_symbol_handles.hcl migrations/08_snapshot_dependencies.hcl
var migrations embed.FS

// UirDB opens the database, discards a pre-handle index, applies the schema, and then discards the
// uir_-prefixed legacy tables. The pre-handle cutover must precede migration, which cannot reconcile
// the current schema onto those tables.
func UirDB(ctx context.Context, options DBOptions) (*gorm.DB, error) {
	if strings.TrimSpace(options.DSN) == "" {
		return nil, errors.New("UIR database DSN is required")
	}
	connection, schema := options.DSN, defaultSchema
	if options.Schema != "" && options.Schema != defaultSchema {
		var err error
		if connection, err = commonsmigrate.ConnectionForSchema(options.DSN, options.Schema); err != nil {
			return nil, fmt.Errorf("scope UIR connection to schema %q: %w", options.Schema, err)
		}
		schema = options.Schema
	}
	database, err := commonsdb.NewGorm(connection, commonsdb.DefaultGormConfig())
	if err != nil {
		return nil, fmt.Errorf("open UIR database: %w", err)
	}
	if err := ping(ctx, database); err != nil {
		return nil, err
	}
	if err := migrate(ctx, database, options.DSN, schema); err != nil {
		return nil, errors.Join(err, closeDatabase(database))
	}
	return database, nil
}

func migrate(ctx context.Context, database *gorm.DB, dsn, schema string) error {
	if err := discardPreHandleGeneration(ctx, database, schema); err != nil {
		return err
	}
	migrationOptions := []commonsmigrate.Option{
		commonsmigrate.WithDir("migrations"),
		commonsmigrate.WithName("uir"),
		commonsmigrate.WithSchema(schema),
		commonsmigrate.WithRebuilds(),
	}
	if err := commonsmigrate.Apply(ctx, dsn, migrations, migrationOptions...); err != nil {
		return fmt.Errorf("migrate UIR schema: %w", err)
	}
	return discardLegacyTables(ctx, database)
}

func closeDatabase(database *gorm.DB) error {
	sqlDB, err := database.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func ping(ctx context.Context, database *gorm.DB) error {
	sqlDB, err := database.DB()
	if err != nil {
		return fmt.Errorf("access UIR database: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return errors.Join(fmt.Errorf("ping UIR database: %w", err), sqlDB.Close())
	}
	return nil
}
