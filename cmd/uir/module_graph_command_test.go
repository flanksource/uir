package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("uir graph --file", func() {
	It("draws a Go file read from stdin without opening an index", func(ctx SpecContext) {
		runtime := &commandRuntime{}
		root := newRootCommand(runtime)
		root.SetIn(strings.NewReader(ordersSource))
		root.SetArgs([]string{"graph", "--file", "-", "--path", ordersPath, "Submit", "--direction", "callees", "--index=false", "--format", "json"})
		output, err := captureStdout(func() error {
			return root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))
		})
		Expect(err).ToNot(HaveOccurred(), output)
		var result query.GraphResult
		Expect(json.Unmarshal([]byte(output), &result)).To(Succeed(), output)
		labels := map[string]bool{}
		for _, node := range result.Nodes {
			labels[node.Label] = node.Unresolved
		}
		Expect(labels).To(Equal(map[string]bool{"Submit": false, "charge": false, "hook": true, "notifier.Notify": true}))
		Expect(runtime.database).To(BeNil(), "a standalone graph opens no database")
	})

	It("refuses a file over HTTP, which would read the server's disk", func(ctx SpecContext) {
		database := indexOrders(ctx)
		runtime := &commandRuntime{database: database}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).ToNot(HaveOccurred())
		response := httptest.NewRecorder()
		parameters := url.Values{"file": {ordersPath}, "selector": {"Submit"}, "root": {ordersRoot}}
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/modules/graph?"+parameters.Encode(), nil))
		Expect(response.Code).To(Equal(http.StatusBadRequest), response.Body.String())
		var diagnostic graphDiagnostic
		Expect(json.Unmarshal(response.Body.Bytes(), &diagnostic)).To(Succeed())
		Expect([]string{diagnostic.Code, diagnostic.Message}).To(Equal([]string{"invalid_query", "an HTTP graph cannot read file, which names a path on the server; draw a file graph on the command line"}))
	})
})
