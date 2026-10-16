package storage

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/commons-db/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"modernc.org/sqlite"
)

// commons-db creates a connection with a nil id and reads back the one the column default assigns
// (14_connections.hcl). PostgreSQL provides gen_random_uuid; SQLite gets the same function here, so
// one canonical schema serves both.
func init() {
	sqlite.MustRegisterScalarFunction("gen_random_uuid", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
		return uuid.NewString(), nil
	})
}

// connectionTypeCheck limited connections to the nine types UIR's own editor offered. The commons-db
// connection service validates every type it offers, so 14_connections.hcl no longer declares the
// check, and a database created before that still carries it.
const connectionTypeCheck = "connections_type_check"

// sqliteConnectionsSQL rebuilds connections without connectionTypeCheck, since the SQLite migration
// refuses to drop a check. The CREATE TABLE is the previous 14_connections.hcl table as commons-db
// projects it for SQLite, less the check; the migration that follows adds the newer columns and
// defaults. The database_modules foreign key names connections, so it binds to the rebuilt table.
var sqliteConnectionsSQL = []string{
	"CREATE TABLE `connections_without_type_check` (`id` text NOT NULL, `name` text NOT NULL, `namespace` text NOT NULL, `type` text NOT NULL, " +
		"`url` text NOT NULL, `username` text NOT NULL, `password` text NOT NULL, `certificate` text NOT NULL, `properties` text NULL, " +
		"`insecure_tls` boolean NOT NULL, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, PRIMARY KEY (`id`), " +
		"CONSTRAINT `connections_name_check` CHECK (length(name) > 0), CONSTRAINT `connections_properties_json` CHECK (json_valid(\"properties\")))",
	"INSERT INTO `connections_without_type_check` (id, name, namespace, type, url, username, password, certificate, properties, insecure_tls, created_at, updated_at) " +
		"SELECT id, name, namespace, type, url, username, password, certificate, properties, insecure_tls, created_at, updated_at FROM connections",
	"DROP TABLE connections",
	"ALTER TABLE `connections_without_type_check` RENAME TO `connections`",
	"CREATE UNIQUE INDEX `connections_namespace_name_key` ON `connections` (`namespace`, `name`)",
}

// dropConnectionTypeCheck removes connectionTypeCheck from a database that has it. It runs before
// migration, which refuses to drop a check on SQLite.
func dropConnectionTypeCheck(ctx context.Context, database *gorm.DB, schema string) error {
	if database.Name() != "sqlite" {
		statement := `ALTER TABLE IF EXISTS "` + schema + `"."connections" DROP CONSTRAINT IF EXISTS ` + connectionTypeCheck
		if err := database.WithContext(ctx).Exec(statement).Error; err != nil {
			return fmt.Errorf("drop %s: %w", connectionTypeCheck, err)
		}
		return nil
	}
	var carriers int64
	if err := database.WithContext(ctx).Raw("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'connections' AND sql LIKE ?",
		"%"+connectionTypeCheck+"%").Scan(&carriers).Error; err != nil {
		return fmt.Errorf("inspect SQLite connections for %s: %w", connectionTypeCheck, err)
	}
	if carriers == 0 {
		return nil
	}
	return database.WithContext(ctx).Connection(func(connection *gorm.DB) error {
		if err := connection.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
			return fmt.Errorf("suspend SQLite foreign keys to drop %s: %w", connectionTypeCheck, err)
		}
		rebuildErr := connection.Transaction(func(tx *gorm.DB) error {
			return execAll(tx, "drop "+connectionTypeCheck, sqliteConnectionsSQL)
		})
		if err := connection.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
			return errors.Join(rebuildErr, fmt.Errorf("restore SQLite foreign keys after dropping %s: %w", connectionTypeCheck, err))
		}
		return rebuildErr
	})
}

// DatabaseModule registers one database under a reusable connection before its
// first catalogue snapshot has succeeded.
type DatabaseModule struct {
	ID           uuid.UUID `gorm:"column:id;primaryKey" json:"id"`
	ConnectionID uuid.UUID `gorm:"column:connection_id" json:"connection_id"`
	DatabaseName string    `gorm:"column:database_name" json:"database_name"`
	Name         string    `gorm:"column:name" json:"name"`
	RootKey      string    `gorm:"column:root_key" json:"root_key"`
	CreatedAt    time.Time `gorm:"column:created_at" json:"created_at"`
}

func (DatabaseModule) TableName() string { return "database_modules" }

func GetConnection(ctx context.Context, database *gorm.DB, id uuid.UUID) (models.Connection, error) {
	var connection models.Connection
	err := database.WithContext(ctx).Table("connections").Where("id = ?", id).First(&connection).Error
	return connection, err
}

func ListDatabaseModules(ctx context.Context, database *gorm.DB) ([]DatabaseModule, error) {
	var modules []DatabaseModule
	err := database.WithContext(ctx).Order("name, id").Find(&modules).Error
	return modules, err
}

func GetDatabaseModule(ctx context.Context, database *gorm.DB, id uuid.UUID) (DatabaseModule, error) {
	var module DatabaseModule
	err := database.WithContext(ctx).Where("id = ?", id).First(&module).Error
	return module, err
}

func CreateDatabaseModule(ctx context.Context, database *gorm.DB, module *DatabaseModule) error {
	if module == nil {
		return errors.New("database module is required")
	}
	if module.ID == uuid.Nil {
		module.ID = uuid.New()
	}
	if module.RootKey == "" {
		module.RootKey = "database/" + module.ID.String()
	}
	module.CreatedAt = time.Now().UTC()
	return database.WithContext(ctx).Create(module).Error
}

func DeleteDatabaseModule(ctx context.Context, database *gorm.DB, id uuid.UUID) error {
	return database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		module, err := GetDatabaseModule(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Delete(&module).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Where("root_key = ?", module.RootKey).Delete(&ModuleRoot{}).Error
	})
}
