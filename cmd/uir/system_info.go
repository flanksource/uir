package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"

	"github.com/flanksource/clicky"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

type systemInfo struct {
	BackendVersion    string `json:"backend_version"`
	DatabaseType      string `json:"database_type"`
	DatabaseVersion   string `json:"database_version"`
	DatabaseLocation  string `json:"database_location"`
	DatabaseSizeBytes int64  `json:"database_size_bytes"`
}

func registerSystemInfoCommand(root *cobra.Command) {
	command := clicky.AddNamedCommandWithContext("info", root, struct{}{}, func(ctx context.Context, _ struct{}) (systemInfo, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return systemInfo{}, err
		}
		info, err := readDatabaseInfo(ctx, database)
		if err != nil {
			return systemInfo{}, err
		}
		info.BackendVersion = root.Version
		return info, nil
	})
	command.Short = "Show backend and database details"
	setModuleRoute(command, "system/info")
	command.Annotations["clicky/operation-method"] = http.MethodGet
}

func readDatabaseInfo(ctx context.Context, database *gorm.DB) (systemInfo, error) {
	connection, err := database.DB()
	if err != nil {
		return systemInfo{}, fmt.Errorf("access UIR database connection: %w", err)
	}
	switch database.Name() {
	case "sqlite":
		return readSQLiteInfo(ctx, connection)
	case "postgres":
		return readPostgresInfo(ctx, connection)
	default:
		return systemInfo{}, fmt.Errorf("unsupported UIR database dialect %q", database.Name())
	}
}

func readSQLiteInfo(ctx context.Context, connection *sql.DB) (systemInfo, error) {
	info := systemInfo{DatabaseType: "SQLite"}
	if err := connection.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&info.DatabaseVersion); err != nil {
		return systemInfo{}, fmt.Errorf("read SQLite version: %w", err)
	}
	rows, err := connection.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return systemInfo{}, fmt.Errorf("read SQLite database location: %w", err)
	}
	for rows.Next() {
		var sequence int
		var name, location string
		if err := rows.Scan(&sequence, &name, &location); err != nil {
			_ = rows.Close()
			return systemInfo{}, fmt.Errorf("scan SQLite database location: %w", err)
		}
		if name == "main" {
			info.DatabaseLocation = location
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return systemInfo{}, fmt.Errorf("iterate SQLite database locations: %w", err)
	}
	if info.DatabaseLocation == "" {
		info.DatabaseLocation = ":memory:"
	}
	var pageCount, pageSize int64
	if err := connection.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return systemInfo{}, fmt.Errorf("read SQLite page count: %w", err)
	}
	if err := connection.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return systemInfo{}, fmt.Errorf("read SQLite page size: %w", err)
	}
	info.DatabaseSizeBytes = pageCount * pageSize
	return info, nil
}

func readPostgresInfo(ctx context.Context, connection *sql.DB) (systemInfo, error) {
	info := systemInfo{DatabaseType: "PostgreSQL"}
	var host, databaseName, schema string
	var port int
	err := connection.QueryRowContext(ctx, `SELECT current_setting('server_version'), COALESCE(host(inet_server_addr()), 'local socket'),
		COALESCE(inet_server_port(), 0), current_database(), current_schema(), pg_database_size(current_database())`).
		Scan(&info.DatabaseVersion, &host, &port, &databaseName, &schema, &info.DatabaseSizeBytes)
	if err != nil {
		return systemInfo{}, fmt.Errorf("read PostgreSQL database details: %w", err)
	}
	if port == 0 {
		info.DatabaseLocation = fmt.Sprintf("%s/%s (schema %s)", host, databaseName, schema)
	} else {
		info.DatabaseLocation = fmt.Sprintf("%s/%s (schema %s)", net.JoinHostPort(host, fmt.Sprint(port)), databaseName, schema)
	}
	return info, nil
}
