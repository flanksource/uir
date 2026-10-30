package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flanksource/commons-db/cmd/query/connections"
	"github.com/flanksource/commons-db/cmd/query/profiles"
	dbcontext "github.com/flanksource/commons-db/context"
	dbquery "github.com/flanksource/commons-db/query"
	"github.com/flanksource/uir/databasecatalog"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type databaseCapture struct {
	ID        uuid.UUID `json:"id"`
	StartedAt time.Time `json:"started_at"`
	Revision  string    `json:"revision"`
	Ordinal   int64     `json:"ordinal"`
}

// The commons-db connection service registers its `connection` entity in clicky's process-wide
// registry, so it is built once and resolves the database through the runtime of the command tree
// built last.
var (
	boundConnectionRuntime atomic.Pointer[commandRuntime]
	connectionServiceOnce  sync.Once
	connectionService      *connections.Service
	connectionServiceErr   error
)

// bindConnectionService binds the connection service to runtime, creating it and registering its
// clicky entity on first use. Neither opens the database.
func bindConnectionService(runtime *commandRuntime) (*connections.Service, error) {
	boundConnectionRuntime.Store(runtime)
	connectionServiceOnce.Do(func() {
		connectionService, connectionServiceErr = connections.New(connections.Options{
			Database: func() (*gorm.DB, error) {
				return boundConnectionRuntime.Load().Database(context.Background())
			},
			Context:    func() dbcontext.Context { return boundConnectionRuntime.Load().connectionContext() },
			DecodeBody: profiles.DecodeRequestBody,
			// UIR has no profile store, so the dashboard counts no profiles for any connection.
			Profiles: func(context.Context) ([]dbquery.Profile, error) { return []dbquery.Profile{}, nil },
		})
		if connectionServiceErr == nil {
			connectionService.RegisterClicky()
		}
	})
	return connectionService, connectionServiceErr
}

// connectionContext carries the database once it is open, as the query app's runtime context does;
// the service opens the database through its Database provider before any call that needs it here.
func (runtime *commandRuntime) connectionContext() dbcontext.Context {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	ctx := dbcontext.New()
	if runtime.database != nil {
		ctx = ctx.WithDB(runtime.database, nil)
	}
	return ctx
}

func registerConnectionRoutes(mux *http.ServeMux, runtime *commandRuntime) {
	mux.HandleFunc("/api/v1/database-modules/connections/", func(w http.ResponseWriter, r *http.Request) {
		connectionDatabases(w, r, runtime)
	})
	mux.HandleFunc("/api/v1/database-modules/diff", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		database, ok := connectionDatabase(w, r, runtime)
		if !ok {
			return
		}
		var input struct {
			From       string `json:"from"`
			To         string `json:"to"`
			FromSchema string `json:"from_schema"`
			ToSchema   string `json:"to_schema"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		result, err := databasecatalog.Diff(r.Context(), database, input.From, input.To, input.FromSchema, input.ToSchema)
		if err != nil {
			apiFailure(w, http.StatusBadRequest, "diff_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("/api/v1/database-modules/snapshots/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/database-modules/snapshots/"), "/")
		if len(parts) != 2 || parts[1] != "schemas" {
			http.NotFound(w, r)
			return
		}
		database, ok := connectionDatabase(w, r, runtime)
		if !ok {
			return
		}
		schemas, err := databasecatalog.Schemas(r.Context(), database, parts[0])
		if err != nil {
			apiFailure(w, http.StatusBadRequest, "schema_list_failed", "Could not list schemas in this capture")
			return
		}
		writeJSON(w, http.StatusOK, schemas)
	})
	mux.HandleFunc("/api/v1/database-modules", func(w http.ResponseWriter, r *http.Request) {
		database, ok := connectionDatabase(w, r, runtime)
		if !ok {
			return
		}
		switch r.Method {
		case http.MethodGet:
			modules, err := storage.ListDatabaseModules(r.Context(), database)
			if err != nil {
				apiFailure(w, http.StatusInternalServerError, "list_failed", "Could not list database modules")
				return
			}
			writeJSON(w, http.StatusOK, modules)
		case http.MethodPost:
			createDatabaseModule(w, r, database)
		default:
			methodNotAllowed(w)
		}
	})
	mux.HandleFunc("/api/v1/database-modules/", func(w http.ResponseWriter, r *http.Request) {
		databaseModuleItem(w, r, runtime)
	})
}

func connectionDatabase(w http.ResponseWriter, r *http.Request, runtime *commandRuntime) (*gorm.DB, bool) {
	database, err := runtime.Database(r.Context())
	if err != nil {
		apiFailure(w, http.StatusInternalServerError, "database_unavailable", "UIR database is unavailable")
		return nil, false
	}
	return database, true
}

// connectionDatabases lists the databases a saved SQL connection can reach, for registering one as a
// database module.
func connectionDatabases(w http.ResponseWriter, r *http.Request, runtime *commandRuntime) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/database-modules/connections/"), "/")
	if len(parts) != 2 || parts[1] != "databases" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		apiFailure(w, http.StatusBadRequest, "invalid_id", "Connection ID must be a UUID")
		return
	}
	database, ok := connectionDatabase(w, r, runtime)
	if !ok {
		return
	}
	item, err := storage.GetConnection(r.Context(), database, id)
	if err != nil {
		apiFailure(w, http.StatusNotFound, "not_found", "Connection was not found")
		return
	}
	if !databasecatalog.IsSQL(item.Type) {
		apiFailure(w, http.StatusBadRequest, "not_sql", "This connection has no databases to select")
		return
	}
	databases, err := databasecatalog.Databases(r.Context(), item)
	if err != nil {
		apiFailure(w, http.StatusUnprocessableEntity, "database_list_failed", "Could not list databases for this connection")
		return
	}
	writeJSON(w, http.StatusOK, databases)
}

func createDatabaseModule(w http.ResponseWriter, r *http.Request, database *gorm.DB) {
	var input struct {
		ConnectionID uuid.UUID `json:"connection_id"`
		DatabaseName string    `json:"database_name"`
		Name         string    `json:"name"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	connectionModel, err := storage.GetConnection(r.Context(), database, input.ConnectionID)
	if err != nil {
		apiFailure(w, http.StatusBadRequest, "invalid_connection", "Choose a saved connection")
		return
	}
	if !databasecatalog.IsSQL(connectionModel.Type) {
		apiFailure(w, http.StatusBadRequest, "not_sql", "Only SQL connections can have database modules")
		return
	}
	if strings.TrimSpace(input.DatabaseName) == "" || strings.TrimSpace(input.Name) == "" {
		apiFailure(w, http.StatusBadRequest, "invalid_module", "Database and module name are required")
		return
	}
	module := storage.DatabaseModule{ConnectionID: input.ConnectionID, DatabaseName: input.DatabaseName, Name: input.Name}
	if err := storage.CreateDatabaseModule(r.Context(), database, &module); err != nil {
		apiFailure(w, http.StatusConflict, "save_failed", "Could not register database module; it may already exist")
		return
	}
	result, err := databasecatalog.Capture(r.Context(), database, module)
	if err != nil {
		writeJSON(w, http.StatusCreated, map[string]any{"module": module, "capture_error": "First schema capture failed; the module remains registered for retry"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"module": module, "capture": result})
}

func databaseModuleItem(w http.ResponseWriter, r *http.Request, runtime *commandRuntime) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/database-modules/"), "/")
	id, err := uuid.Parse(parts[0])
	if err != nil {
		apiFailure(w, http.StatusBadRequest, "invalid_id", "Database module ID must be a UUID")
		return
	}
	database, ok := connectionDatabase(w, r, runtime)
	if !ok {
		return
	}
	module, err := storage.GetDatabaseModule(r.Context(), database, id)
	if err != nil {
		apiFailure(w, http.StatusNotFound, "not_found", "Database module was not found")
		return
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "capture":
			if r.Method != http.MethodPost {
				methodNotAllowed(w)
				return
			}
			result, err := databasecatalog.Capture(r.Context(), database, module)
			if err != nil {
				apiFailure(w, http.StatusUnprocessableEntity, "capture_failed", "Schema capture failed; check the connection and selected database")
				return
			}
			writeJSON(w, http.StatusOK, result)
		case "snapshots":
			if r.Method != http.MethodGet {
				methodNotAllowed(w)
				return
			}
			snapshots := []storage.ModuleSnapshot{}
			err := database.WithContext(r.Context()).Model(&storage.ModuleSnapshot{}).
				Joins("JOIN modules ON modules.id = snapshots.root_id").Where("modules.root_key = ?", module.RootKey).
				Order("snapshots.started_at DESC").Limit(100).Find(&snapshots).Error
			if err != nil {
				apiFailure(w, http.StatusInternalServerError, "snapshot_list_failed", "Could not list schema captures")
				return
			}
			views := make([]databaseCapture, 0, len(snapshots))
			for _, snapshot := range snapshots {
				views = append(views, databaseCapture{snapshot.ID, snapshot.StartedAt, snapshot.Revision, snapshot.Ordinal})
			}
			writeJSON(w, http.StatusOK, views)
		default:
			http.NotFound(w, r)
		}
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, module)
	case http.MethodDelete:
		if err := storage.DeleteDatabaseModule(r.Context(), database, id); err != nil {
			apiFailure(w, http.StatusInternalServerError, "delete_failed", "Could not remove database module and snapshots")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w)
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request, into any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		apiFailure(w, http.StatusBadRequest, "invalid_json", "Invalid request JSON: "+err.Error())
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		apiFailure(w, http.StatusBadRequest, "invalid_json", "Expected one JSON object")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		apiFailure(w, http.StatusInternalServerError, "encoding_failed", "Could not encode API response")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(encoded, '\n'))
}

func apiFailure(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}

func methodNotAllowed(w http.ResponseWriter) {
	apiFailure(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method is not allowed")
}
