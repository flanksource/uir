package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL metrics API", func() {
	It("serves metrics and CLI JSON without source files, and catalogs the new fields", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		checkout := GinkgoT().TempDir()
		const module = "example.org/metric-api"
		Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+module+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(checkout, "sample.go"), []byte("package sample\nfunc Sample(a, b int) {}\n"), 0o644)).To(Succeed())
		indexed, err := addModules(ctx, database, checkout, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.RemoveAll(checkout)).To(Succeed())
		Expect(database.Exec("DELETE FROM source_blobs").Error).To(Succeed())
		runtime := &commandRuntime{database: database}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).NotTo(HaveOccurred())
		for _, expression := range []string{
			"SELECT path, file.lines, file.definitions FROM files() WHERE file.definitions < 3",
			"SELECT symbol.name, path, line, symbol.metrics.param_count, symbol.metrics.body_lines FROM func:*",
		} {
			result, err := queryModules(ctx, database, moduleQueryOptions{Expression: expression, SnapshotID: indexed[0].SnapshotID})
			Expect(err).NotTo(HaveOccurred(), expression)
			Expect(result.Total).To(Equal(1))
			Expect(result.Matches[0].SourceID).NotTo(BeEmpty())
			output, err := clicky.Format(result, clicky.FormatOptions{Format: "json"})
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("integer"))
			body, err := json.Marshal(map[string]any{"args": []string{expression}, "snapshot": indexed[0].SnapshotID, "language": "sql"})
			Expect(err).NotTo(HaveOccurred())
			request := httptest.NewRequest(http.MethodPost, "/api/v1/modules/query", strings.NewReader(string(body)))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
			var decoded moduleQueryResult
			Expect(json.Unmarshal(response.Body.Bytes(), &decoded)).To(Succeed())
			Expect(decoded.Rows).To(HaveLen(1))
			Expect(decoded.Matches[0].Path).To(Equal("sample.go"))
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/modules/query-catalog", nil))
		Expect(response.Code).To(Equal(http.StatusOK))
		var catalog query.SQLCatalog
		Expect(json.Unmarshal(response.Body.Bytes(), &catalog)).To(Succeed())
		Expect(catalog.Functions).To(ContainElement(And(HaveField("Name", "files"), HaveField("Arguments", 0))))
		Expect(catalog.Fields).To(HaveKeyWithValue("file.lines", "integer"))
		Expect(catalog.Fields).To(HaveKeyWithValue("symbol.metrics.param_count", "integer"))
	})
})
