package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL query API", func() {
	It("retains the SQL result envelope in CLI JSON for rows, paths and graphs", func(ctx SpecContext) {
		database := indexOrders(ctx)
		for _, expression := range []string{
			"SELECT symbol.name FROM callees(func:Submit) LIMIT 1",
			"SELECT * FROM path(func:Submit, func:charge)",
			"SELECT * FROM graph(func:Submit)",
		} {
			result, err := queryModules(ctx, database, moduleQueryOptions{Expression: expression, RootKey: ordersRoot})
			Expect(err).NotTo(HaveOccurred(), expression)
			output, err := clicky.Format(result, clicky.FormatOptions{Format: "json"})
			Expect(err).NotTo(HaveOccurred(), expression)
			var envelope map[string]any
			Expect(json.Unmarshal([]byte(output), &envelope)).To(Succeed(), expression)
			Expect(envelope).To(HaveKeyWithValue("total", float64(result.Total)))
			Expect(envelope).To(HaveKey("matches"))
			if result.Graph != nil {
				Expect(envelope).To(HaveKey("graph"))
			} else if result.Path != nil {
				Expect(envelope).To(HaveKey("path"))
			} else {
				Expect(envelope).To(HaveKey("columns"))
				Expect(envelope).To(HaveKey("rows"))
			}
		}
	})
	It("serves projected columns, pre-limit totals, witnesses and terminal graphs through the existing route", func(ctx SpecContext) {
		runtime := &commandRuntime{database: indexOrders(ctx)}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).NotTo(HaveOccurred())
		post := func(expression string) moduleQueryResult {
			GinkgoHelper()
			body, err := json.Marshal(map[string]any{"args": []string{expression}, "root": ordersRoot, "language": "sql"})
			Expect(err).NotTo(HaveOccurred())
			request := httptest.NewRequest(http.MethodPost, "/api/v1/modules/query", strings.NewReader(string(body)))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
			Expect(response.Header().Get("Server-Timing")).NotTo(BeEmpty())
			var result moduleQueryResult
			Expect(json.Unmarshal(response.Body.Bytes(), &result)).To(Succeed())
			return result
		}
		result := post("SELECT symbol.name AS name, path, line FROM callees(func:Submit) ORDER BY line LIMIT 1")
		Expect(result.Total).To(BeNumerically(">", 1))
		Expect(result.Rows).To(HaveLen(1))
		Expect(result.Columns[0]).To(Equal(query.QueryColumn{Name: "name", Field: "symbol.name", Type: "string"}))
		Expect(result.Matches[0].Path).To(Equal(ordersPath))
		result = post("SELECT * FROM graph(func:Submit, direction => 'both', depth => 2)")
		Expect(result.Graph).NotTo(BeNil())
		Expect(result.Graph.Edges).NotTo(BeEmpty())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/modules/query-catalog", nil))
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var catalog query.SQLCatalog
		Expect(json.Unmarshal(response.Body.Bytes(), &catalog)).To(Succeed())
		Expect(catalog.Functions).To(ContainElement(HaveField("Name", "traverse")))
		Expect(catalog.Kinds).To(ContainElement(HaveField("Name", "func")))
	})
	It("returns positioned SQL diagnostics and refuses compact-only flags without fallback", func(ctx SpecContext) {
		runtime := &commandRuntime{database: openCommandDatabase(ctx)}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).NotTo(HaveOccurred())
		for _, body := range []string{`{"args":["SELECT * FROM"]}`, `{"args":["SELECT * FROM func:*"],"methods":true}`} {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/modules/query", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			Expect(response.Code).To(Equal(http.StatusBadRequest), response.Body.String())
			var diagnostic graphDiagnostic
			Expect(json.Unmarshal(response.Body.Bytes(), &diagnostic)).To(Succeed())
			Expect(diagnostic.Code).To(Equal("invalid_query"))
			Expect(diagnostic.Hint).NotTo(BeEmpty())
		}
	})
})
