package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/commons-db/models"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// localRequest is a same-origin request from the loopback interface, which the connection routes require.
func localRequest(method, path, body string) *http.Request {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	request.RemoteAddr = "127.0.0.1:12345"
	request.Host = "localhost:8080"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func serveConnectionRequest(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func connectionHandler(database *gorm.DB) http.Handler {
	GinkgoHelper()
	runtime := &commandRuntime{database: database}
	handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
	Expect(err).ToNot(HaveOccurred())
	return handler
}

func createConnection(handler http.Handler, body string) uuid.UUID {
	GinkgoHelper()
	created := serveConnectionRequest(handler, localRequest(http.MethodPost, "/api/v1/connection", body))
	Expect(created.Code).To(BeNumerically("<", 300), created.Body.String())
	var connection models.Connection
	Expect(json.Unmarshal(created.Body.Bytes(), &connection)).To(Succeed())
	Expect(connection.ID).ToNot(Equal(uuid.Nil), created.Body.String())
	return connection.ID
}

var _ = Describe("connection HTTP API", func() {
	It("serves the commons-db connection service, its form schema and dashboard, to local requests only", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		handler := connectionHandler(database)
		id := createConnection(handler, `{"name":"cache","type":"redis","url":"redis://127.0.0.1:1"}`)
		saved, err := storage.GetConnection(ctx, database, id)
		Expect(err).ToNot(HaveOccurred())
		Expect(saved.Type).To(Equal(models.ConnectionTypeRedis))

		dashboard := serveConnectionRequest(handler, localRequest(http.MethodGet, "/api/v1/connections/dashboard", ""))
		Expect(dashboard.Code).To(Equal(http.StatusOK), dashboard.Body.String())
		Expect(dashboard.Body.String()).To(ContainSubstring(id.String()))

		tested := serveConnectionRequest(handler, localRequest(http.MethodPost, "/api/v1/connection/test", `{"type":"redis","url":"redis://127.0.0.1:1"}`))
		Expect(tested.Code).To(Equal(http.StatusOK), tested.Body.String())
		var result struct {
			OK *bool `json:"ok"`
		}
		Expect(json.Unmarshal(tested.Body.Bytes(), &result)).To(Succeed())
		Expect(result.OK).To(HaveValue(BeFalse()))

		schemaRequest := localRequest(http.MethodGet, "/api/v1/connection", "")
		schemaRequest.Header.Set("Accept", "application/schema+json")
		schema := serveConnectionRequest(handler, schemaRequest)
		Expect(schema.Code).To(Equal(http.StatusOK), schema.Body.String())
		Expect(schema.Body.String()).To(ContainSubstring(`"redis"`))

		remote := localRequest(http.MethodGet, "/api/v1/connection", "")
		remote.RemoteAddr = "192.0.2.1:12345"
		Expect(serveConnectionRequest(handler, remote).Code).To(Equal(http.StatusForbidden))
	})

	DescribeTable("refuses to delete a connection a database module depends on and keeps the row",
		func(ctx SpecContext, open func(context.Context) *gorm.DB) {
			database := open(ctx)
			handler := connectionHandler(database)
			id := createConnection(handler, `{"name":"warehouse","type":"postgres","url":"postgres://db.example/warehouse"}`)
			module := storage.DatabaseModule{ConnectionID: id, DatabaseName: "orders", Name: "Orders"}
			Expect(storage.CreateDatabaseModule(ctx, database, &module)).To(Succeed())
			deleted := serveConnectionRequest(handler, localRequest(http.MethodDelete, "/api/v1/connection/"+id.String(), ""))
			Expect(deleted.Code).To(Equal(http.StatusConflict), deleted.Body.String())
			_, err := storage.GetConnection(ctx, database, id)
			Expect(err).ToNot(HaveOccurred())
		},
		Entry("SQLite", openCommandDatabase),
		Entry("PostgreSQL", func(ctx context.Context) *gorm.DB {
			database, err := storage.UirDB(ctx, storage.DBOptions{DSN: dbtest.ForGinkgo(dbtest.Options{Name: "uir_connections"}).DSN(), Schema: "uir_connections"})
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() {
				sqlDB, err := database.DB()
				Expect(err).ToNot(HaveOccurred())
				Expect(sqlDB.Close()).To(Succeed())
			})
			return database
		}),
	)

	It("lists a SQL connection's databases under the database-module routes and documents them", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		handler := connectionHandler(database)
		source := filepath.Join(GinkgoT().TempDir(), "source.db")
		connection := models.Connection{Name: "local", Type: models.ConnectionTypeSQLite, URL: source}
		Expect(database.WithContext(ctx).Create(&connection).Error).To(Succeed())
		listed := serveConnectionRequest(handler, localRequest(http.MethodPost, "/api/v1/database-modules/connections/"+connection.ID.String()+"/databases", ""))
		Expect(listed.Code).To(Equal(http.StatusOK), listed.Body.String())

		spec := serveConnectionRequest(handler, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
		Expect(spec.Code).To(Equal(http.StatusOK))
		var document struct {
			Paths map[string]map[string]any `json:"paths"`
		}
		Expect(json.Unmarshal(spec.Body.Bytes(), &document)).To(Succeed())
		for _, path := range []string{
			"/api/v1/connections/dashboard", "/api/v1/database-modules/connections/{id}/databases",
			"/api/v1/database-modules", "/api/v1/database-modules/{id}", "/api/v1/database-modules/diff",
		} {
			Expect(document.Paths).To(HaveKey(path))
		}
		Expect(document.Paths).ToNot(HaveKey("/api/v1/connections/{id}/databases"))
	})
})
