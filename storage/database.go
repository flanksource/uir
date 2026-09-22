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

//go:embed migrations/*.hcl
var migrations embed.FS

func UirDB(ctx context.Context, options DBOptions) (*gorm.DB, error) {
	if strings.TrimSpace(options.DSN) == "" {
		return nil, errors.New("UIR database DSN is required")
	}
	migrationOptions := []commonsmigrate.Option{
		commonsmigrate.WithDir("migrations"),
		commonsmigrate.WithName("uir"),
	}
	if options.Schema != "" {
		migrationOptions = append(migrationOptions, commonsmigrate.WithSchema(options.Schema))
	}
	if err := commonsmigrate.Apply(ctx, options.DSN, migrations, migrationOptions...); err != nil {
		return nil, fmt.Errorf("migrate UIR schema: %w", err)
	}

	connection := options.DSN
	if options.Schema != "" && options.Schema != defaultSchema {
		var err error
		connection, err = commonsmigrate.ConnectionForSchema(options.DSN, options.Schema)
		if err != nil {
			return nil, fmt.Errorf("scope UIR PostgreSQL connection: %w", err)
		}
	}
	database, err := commonsdb.NewGorm(connection, commonsdb.DefaultGormConfig())
	if err != nil {
		return nil, fmt.Errorf("open UIR database: %w", err)
	}
	if err := ping(ctx, database); err != nil {
		return nil, err
	}
	return database, nil
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
