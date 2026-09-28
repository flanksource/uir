package storage

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	commonsdb "github.com/flanksource/commons-db/db"
	commonsmigrate "github.com/flanksource/commons-db/migrate"
	"gorm.io/gorm"
)

// OpenReadOnly opens an initialized UIR index without running migrations or cutover.
func OpenReadOnly(ctx context.Context, options DBOptions) (*gorm.DB, error) {
	if strings.TrimSpace(options.DSN) == "" {
		return nil, errors.New("UIR database DSN is required")
	}
	connection := options.DSN
	if strings.HasPrefix(strings.ToLower(connection), "sqlite://") || strings.EqualFold(filepath.Ext(strings.Split(connection, "?")[0]), ".db") {
		if options.Schema != "" && options.Schema != defaultSchema {
			return nil, fmt.Errorf("SQLite does not support schema %q", options.Schema)
		}
		var err error
		connection, err = sqliteReadOnlyDSN(connection)
		if err != nil {
			return nil, err
		}
	} else if options.Schema != "" && options.Schema != defaultSchema {
		var err error
		connection, err = commonsmigrate.ConnectionForSchema(connection, options.Schema)
		if err != nil {
			return nil, fmt.Errorf("scope UIR PostgreSQL connection: %w", err)
		}
	}
	database, err := commonsdb.NewGorm(connection, commonsdb.DefaultGormConfig())
	if err != nil {
		return nil, fmt.Errorf("open existing UIR database: %w", err)
	}
	schema := options.Schema
	if schema == "" {
		schema = defaultSchema
	}
	preHandle, err := isPreHandle(ctx, database, schema)
	if err != nil {
		return nil, errors.Join(err, closeDatabase(database))
	}
	if preHandle {
		return nil, errors.Join(errors.New("database predates compact symbol handles; open it read-write once or run uir reindex"), closeDatabase(database))
	}
	var count int
	if err := database.WithContext(ctx).Raw("SELECT COUNT(*) FROM symbols WHERE 1 = 0").Scan(&count).Error; err != nil {
		return nil, errors.Join(fmt.Errorf("UIR index schema is unavailable: %w", err), closeDatabase(database))
	}
	return database, nil
}

func sqliteReadOnlyDSN(connection string) (string, error) {
	pathAndQuery := strings.TrimPrefix(connection, "sqlite://")
	path, rawQuery, _ := strings.Cut(pathAndQuery, "?")
	if strings.HasPrefix(path, "file:") {
		parsed, err := url.Parse(path)
		if err != nil {
			return "", fmt.Errorf("parse SQLite file URI %q: %w", path, err)
		}
		path = parsed.Path
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve SQLite path %q: %w", path, err)
	}
	if _, err := os.Stat(absolute); err != nil {
		return "", fmt.Errorf("open existing UIR index %q: %w", absolute, err)
	}
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", fmt.Errorf("parse SQLite DSN query: %w", err)
	}
	query.Set("mode", "ro")
	return "sqlite://" + (&url.URL{Scheme: "file", Path: absolute, RawQuery: query.Encode()}).String(), nil
}
