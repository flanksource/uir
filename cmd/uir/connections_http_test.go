package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/flanksource/uir/databasecatalog"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("connection HTTP API", func() {
	It("accepts a literal credential, warns, masks reads, and preserves it on a name edit", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		runtime := &commandRuntime{database: database}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).ToNot(HaveOccurred())
		request := httptest.NewRequest(http.MethodPost, "/api/v1/connections", strings.NewReader(`{"name":"Example","type":"postgres","url":"postgres://alice:literal-secret@db.example/catalog"}`))
		request.RemoteAddr = "127.0.0.1:12345"
		request.Host = "localhost:8080"
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		Expect(response.Code).To(Equal(http.StatusCreated), response.Body.String())
		var view databasecatalog.ConnectionView
		Expect(json.Unmarshal(response.Body.Bytes(), &view)).To(Succeed())
		Expect(view.LiteralCredentials).To(BeTrue())
		Expect(response.Body.String()).NotTo(ContainSubstring("literal-secret"))
		edit := httptest.NewRequest(http.MethodPut, "/api/v1/connections/"+view.ID.String(), strings.NewReader(`{"name":"Renamed"}`))
		edit.RemoteAddr = "127.0.0.1:12345"
		edit.Host = "localhost:8080"
		edit.Header.Set("Content-Type", "application/json")
		updated := httptest.NewRecorder()
		handler.ServeHTTP(updated, edit)
		Expect(updated.Code).To(Equal(http.StatusOK), updated.Body.String())
		saved, err := storage.GetConnection(ctx, database, view.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(saved.Name).To(Equal("Renamed"))
		Expect(saved.URL).To(ContainSubstring("literal-secret"))
	})

	It("serves nine adapted form schemas only to local requests", func(ctx SpecContext) {
		runtime := &commandRuntime{database: openCommandDatabase(ctx)}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).ToNot(HaveOccurred())
		request := httptest.NewRequest(http.MethodGet, "/api/v1/connections/schema", nil)
		request.RemoteAddr = "127.0.0.1:12345"
		request.Host = "localhost:8080"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		Expect(response.Code).To(Equal(http.StatusOK))
		var schemas map[string]any
		Expect(json.Unmarshal(response.Body.Bytes(), &schemas)).To(Succeed())
		Expect(schemas).To(HaveLen(9))
		remote := httptest.NewRequest(http.MethodGet, "/api/v1/connections/schema", nil)
		remote.RemoteAddr = "192.0.2.1:12345"
		remote.Host = "localhost:8080"
		blocked := httptest.NewRecorder()
		handler.ServeHTTP(blocked, remote)
		Expect(blocked.Code).To(Equal(http.StatusForbidden))
	})

	It("describes the connection and database module routes in OpenAPI", func(ctx SpecContext) {
		runtime := &commandRuntime{database: openCommandDatabase(ctx)}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).ToNot(HaveOccurred())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
		Expect(response.Code).To(Equal(http.StatusOK))
		var spec struct {
			Paths map[string]map[string]any `json:"paths"`
		}
		Expect(json.Unmarshal(response.Body.Bytes(), &spec)).To(Succeed())
		for _, path := range []string{
			"/api/v1/connections/schema", "/api/v1/connections", "/api/v1/connections/{id}",
			"/api/v1/connections/{id}/test", "/api/v1/connections/{id}/databases",
			"/api/v1/database-modules", "/api/v1/database-modules/{id}",
			"/api/v1/database-modules/{id}/capture", "/api/v1/database-modules/{id}/snapshots",
			"/api/v1/database-modules/snapshots/{snapshot}/schemas", "/api/v1/database-modules/diff",
		} {
			Expect(spec.Paths).To(HaveKey(path))
		}
		Expect(spec.Paths["/api/v1/connections"]["post"]).To(HaveKey("requestBody"))
		Expect(spec.Paths["/api/v1/database-modules/diff"]["post"]).To(HaveKey("responses"))
	})
})
