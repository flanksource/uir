package databasecatalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/commons-db/connection"
	dbcontext "github.com/flanksource/commons-db/context"
	sqlinspect "github.com/flanksource/commons-db/inspect/sql"
	"github.com/flanksource/commons-db/models"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func IsSQL(connectionType string) bool {
	switch connectionType {
	case models.ConnectionTypePostgres, models.ConnectionTypeMySQL, models.ConnectionTypeSQLServer, models.ConnectionTypeSQLite:
		return true
	default:
		return false
	}
}

// Databases lists the databases visible to one SQL connection before a module
// is registered. SQLite exposes its single file as "snapshot".
func Databases(ctx context.Context, model models.Connection) (databases []string, err error) {
	client, err := openSQL(ctx, model, "")
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close database listing client: %w", closeErr))
		}
	}()
	return sqlinspect.ListDatabases(ctx, client, model.Type)
}

func openSQL(ctx context.Context, model models.Connection, databaseName string) (*sql.DB, error) {
	if !IsSQL(model.Type) {
		return nil, fmt.Errorf("connection %s is not a SQL connection", model.ID)
	}
	var sqlConnection connection.SQLConnection
	if err := sqlConnection.FromModel(model); err != nil {
		return nil, err
	}
	sqlConnection.ConnectionName = ""
	if err := sqlConnection.HydrateConnection(dbcontext.NewContext(ctx)); err != nil {
		return nil, err
	}
	if databaseName != "" && model.Type != models.ConnectionTypeSQLite {
		selected, err := sqlConnection.UseDatabase(databaseName)
		if err != nil {
			return nil, err
		}
		sqlConnection = selected
	}
	if model.Type == models.ConnectionTypeSQLite && databaseName != "" && databaseName != "snapshot" {
		return nil, fmt.Errorf("SQLite has one selectable database, snapshot; got %q", databaseName)
	}
	client, err := sqlConnection.Client(dbcontext.NewContext(ctx))
	if err != nil {
		return nil, err
	}
	if err := client.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("ping %s database: %w", model.Type, err), client.Close())
	}
	return client, nil
}

// Capture reads only catalogue metadata and publishes an immutable UIR snapshot.
// The connection and module registrations remain even if inspection fails.
func Capture(ctx context.Context, database *gorm.DB, module storage.DatabaseModule) (result indexer.ModuleResult, err error) {
	connectionModel, err := storage.GetConnection(ctx, database, module.ConnectionID)
	if err != nil {
		return indexer.ModuleResult{}, fmt.Errorf("load module connection: %w", err)
	}
	client, err := openSQL(ctx, connectionModel, module.DatabaseName)
	if err != nil {
		return indexer.ModuleResult{}, err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close schema capture client: %w", closeErr))
		}
	}()
	catalog, err := sqlinspect.Inspect(ctx, client, connectionModel.Type, sqlinspect.Limits{}, sqlinspect.Options{
		CacheKey: fmt.Sprintf("database-module:%s:%s:%d", module.ID, connectionModel.ID, connectionModel.UpdatedAt.UnixNano()), Refresh: true,
	})
	if err != nil {
		return indexer.ModuleResult{}, fmt.Errorf("inspect database catalogue: %w", err)
	}
	documents, err := Documents(catalog)
	if err != nil {
		return indexer.ModuleResult{}, err
	}
	publication, err := indexer.PublicationFromNodes(indexer.NodePublication{
		Options: indexer.PublishOptions{Producer: "database-catalog-v1"},
		RootKey: module.RootKey, Name: module.Name,
		Location:  indexer.ExternalLocation{URI: "database://" + module.ID.String()},
		Revision:  time.Now().UTC().Format(time.RFC3339Nano) + "/" + uuid.NewString(),
		Documents: documents,
	})
	if err != nil {
		return indexer.ModuleResult{}, fmt.Errorf("lower database catalogue: %w", err)
	}
	return indexer.Publish(ctx, database, publication)
}
