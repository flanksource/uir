package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const (
	ordersRoot   = "example.org/orders"
	ordersPath   = "orders.go"
	ordersSource = `package orders

type Notifier interface {
	Notify(message string)
}

type Mail struct{}

func (Mail) Notify(message string) {}

type Order struct {
	Total int
	Rush  bool
}

func Submit(order Order, notifier Notifier, hook func()) {
	if order.Total > 0 {
		charge(order)
	}
	if order.Rush {
		charge(order)
	}
	if hook != nil {
		hook()
	}
	notifier.Notify("submitted")
}

func charge(order Order) {
	if order.Rush {
		charge(Order{Total: order.Total})
	}
}

func Checkout(order Order, notifier Notifier) { Submit(order, notifier, nil) }
`
)

// ordersLine is the line of the fixture that holds snippet.
func ordersLine(snippet string) int {
	GinkgoHelper()
	Expect(strings.Count(ordersSource, snippet)).To(Equal(1), "%q appears once in the fixture", snippet)
	before, _, _ := strings.Cut(ordersSource, snippet)
	return strings.Count(before, "\n") + 1
}

// indexOrders indexes a module whose Submit is called by Checkout and calls a
// function twice under different guards, a function value, and an interface
// method with one implementation.
func indexOrders(ctx context.Context) *gorm.DB {
	GinkgoHelper()
	database := openCommandDatabase(ctx)
	workspace := GinkgoT().TempDir()
	Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module "+ordersRoot+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(workspace, ordersPath), []byte(ordersSource), 0o644)).To(Succeed())
	_, err := addModules(ctx, database, workspace, false)
	Expect(err).ToNot(HaveOccurred())
	return database
}

type graphDiagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

var _ = Describe("module call graph", func() {
	var (
		database *gorm.DB
		handler  http.Handler
	)

	get := func(parameters url.Values) *httptest.ResponseRecorder {
		GinkgoHelper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/modules/graph?"+parameters.Encode(), nil))
		return response
	}

	BeforeEach(func(ctx SpecContext) {
		database = indexOrders(ctx)
		runtime := &commandRuntime{database: database}
		var err error
		handler, err = newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).ToNot(HaveOccurred())
	})

	It("serves the graph fields at the top level beside stages, warnings and candidates", func() {
		response := get(url.Values{"selector": {"orders.Submit"}, "direction": {"both"}, "depth": {"2"}, "root": {ordersRoot}})
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var envelope map[string]any
		Expect(json.Unmarshal(response.Body.Bytes(), &envelope)).To(Succeed())
		Expect(envelope).To(And(HaveKey("roots"), HaveKey("nodes"), HaveKey("edges"), HaveKey("omitted"), HaveKey("stages"),
			HaveKey("packages"), HaveKeyWithValue("warnings", BeEmpty()), HaveKeyWithValue("candidates", BeEmpty())))

		var result query.GraphResult
		Expect(json.Unmarshal(response.Body.Bytes(), &result)).To(Succeed())
		depths := map[string]int{}
		ids := map[string]string{}
		for _, node := range result.Nodes {
			depths[node.Label], ids[node.Label] = node.Depth, node.ID
		}
		Expect(depths).To(Equal(map[string]int{"Submit": 0, "Checkout": -1, "charge": 1, "hook": 1, "Notifier.Notify": 1, "Mail.Notify": 1}))
		Expect(result.Roots).To(Equal([]string{ids["Submit"]}))
		Expect(result.Omitted).To(Equal(graph.Omitted{Unresolved: 1}))
		Expect(result.Exclude).To(Equal(query.DefaultGraphExclusions))
		Expect(result.Packages).To(Equal([]query.GraphPackage{{Path: ordersRoot, Nodes: 6}}))
		Expect(result.Edges).To(ContainElements(
			graph.Edge{ID: ids["Submit"] + "|" + ids["charge"] + "|call", From: ids["Submit"], To: ids["charge"], Type: "call", Sites: []graph.Site{
				{Path: ordersPath, Line: ordersLine("charge(order)\n\t}\n\tif order.Rush"), Column: 3, Text: "charge(order)", Guards: []string{"order.Total > 0"}},
				{Path: ordersPath, Line: ordersLine("charge(order)\n\t}\n\tif hook"), Column: 3, Text: "charge(order)", Guards: []string{"order.Rush"}},
			}},
			graph.Edge{ID: ids["Submit"] + "|" + ids["Mail.Notify"] + "|dispatch", From: ids["Submit"], To: ids["Mail.Notify"], Type: "dispatch", Sites: []graph.Site{
				{Path: ordersPath, Line: ordersLine("notifier.Notify("), Column: 11, Text: `notifier.Notify("submitted")`},
			}},
		))
	})

	It("takes the exclusion patterns as a comma list and echoes them", func() {
		response := get(url.Values{"selector": {"orders.Submit"}, "exclude": {"external,example.org/orders/..."}})
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var result query.GraphResult
		Expect(json.Unmarshal(response.Body.Bytes(), &result)).To(Succeed())
		Expect(result.Exclude).To(Equal([]string{"external", "example.org/orders/..."}))
		Expect(result.Nodes).To(HaveLen(1), "only the root is left of its excluded package")
		Expect(result.Omitted.Excluded).To(Equal(map[string]int{ordersRoot: 5}))
		Expect(result.Packages).To(Equal([]query.GraphPackage{{Path: ordersRoot, Nodes: 6, Excluded: true}}))
	})

	It("accepts the canonical symbol id of a node in place of a selector", func() {
		selected := get(url.Values{"selector": {"orders.Submit"}, "direction": {"callees"}, "depth": {"1"}})
		Expect(selected.Code).To(Equal(http.StatusOK), selected.Body.String())
		var bySelector query.GraphResult
		Expect(json.Unmarshal(selected.Body.Bytes(), &bySelector)).To(Succeed())

		response := get(url.Values{"symbol": bySelector.Roots, "direction": {"callees"}, "depth": {"1"}})
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var bySymbol query.GraphResult
		Expect(json.Unmarshal(response.Body.Bytes(), &bySymbol)).To(Succeed())
		Expect(bySymbol.Graph).To(Equal(bySelector.Graph))
	})

	It("returns the candidates of an ambiguous selector with an empty graph", func() {
		response := get(url.Values{"selector": {"Notify"}})
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var envelope map[string]any
		Expect(json.Unmarshal(response.Body.Bytes(), &envelope)).To(Succeed())
		Expect(envelope).To(And(HaveKeyWithValue("roots", BeEmpty()), HaveKeyWithValue("nodes", BeEmpty()), HaveKeyWithValue("edges", BeEmpty())))
		var result query.GraphResult
		Expect(json.Unmarshal(response.Body.Bytes(), &result)).To(Succeed())
		names := make([]string, 0, len(result.Candidates))
		for _, candidate := range result.Candidates {
			names = append(names, candidate.QueryName)
		}
		Expect(names).To(ConsistOf(ordersRoot+".Notifier.Notify", ordersRoot+".Mail.Notify"))
	})

	DescribeTable("rejects a request it cannot draw",
		func(parameters url.Values, status int, code, message string) {
			response := get(parameters)
			Expect(response.Code).To(Equal(status), response.Body.String())
			var diagnostic graphDiagnostic
			Expect(json.Unmarshal(response.Body.Bytes(), &diagnostic)).To(Succeed())
			Expect(diagnostic.Code).To(Equal(code))
			Expect(diagnostic.Message).To(ContainSubstring(message))
			Expect(diagnostic.Hint).ToNot(BeEmpty())
		},
		Entry("a depth beyond the maximum", url.Values{"selector": {"orders.Submit"}, "depth": {"9"}}, http.StatusBadRequest, "invalid_query", "depth 9 is outside 1..8"),
		Entry("a limit beyond the maximum", url.Values{"selector": {"orders.Submit"}, "limit": {"1001"}}, http.StatusBadRequest, "invalid_query", "limit 1001 is outside 1..1000"),
		Entry("an unknown direction", url.Values{"selector": {"orders.Submit"}, "direction": {"sideways"}}, http.StatusBadRequest, "invalid_query", `direction "sideways"`),
		Entry("an exclusion pattern that is a glob", url.Values{"selector": {"orders.Submit"}, "exclude": {"std,gorm.io/*"}}, http.StatusBadRequest, "invalid_query", `"gorm.io/*"`),
		Entry("none beside another exclusion pattern", url.Values{"selector": {"orders.Submit"}, "exclude": {"none,std"}}, http.StatusBadRequest, "invalid_query", "none excludes nothing"),
		Entry("neither a selector nor a symbol", url.Values{"depth": {"2"}}, http.StatusBadRequest, "invalid_query", "requires a selector or a symbol"),
		Entry("both a selector and a symbol", url.Values{"selector": {"orders.Submit"}, "symbol": {"0123"}}, http.StatusBadRequest, "invalid_query", "not both"),
		Entry("a selector that names a type", url.Values{"selector": {"orders.Order"}}, http.StatusBadRequest, "invalid_query", "matched no function or method"),
		Entry("a selector that matches nothing", url.Values{"selector": {"orders.Missing"}}, http.StatusNotFound, "symbol_not_found", "orders.Missing"),
		Entry("a symbol id that is not indexed", url.Values{"symbol": {"0123456789abcdef"}}, http.StatusNotFound, "symbol_not_found", "0123456789abcdef"),
	)

	It("publishes the route once per parameter in the OpenAPI document", func() {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
		Expect(response.Code).To(Equal(http.StatusOK))
		var document struct {
			Paths map[string]map[string]struct {
				Parameters []struct {
					Name string `json:"name"`
					In   string `json:"in"`
				} `json:"parameters"`
			} `json:"paths"`
		}
		Expect(json.Unmarshal(response.Body.Bytes(), &document)).To(Succeed())
		Expect(document.Paths).To(HaveKey("/api/v1/modules/graph"))
		Expect(document.Paths["/api/v1/modules/graph"]).To(HaveLen(1))
		operation, published := document.Paths["/api/v1/modules/graph"]["get"]
		Expect(published).To(BeTrue(), "the graph is read with GET")
		names := make([]string, 0, len(operation.Parameters))
		for _, parameter := range operation.Parameters {
			Expect(parameter.In).To(Equal("query"), parameter.Name)
			names = append(names, parameter.Name)
		}
		Expect(names).To(ConsistOf("args", "selector", "symbol", "direction", "depth", "limit", "exclude", "access", "root", "location", "snapshot"))
	})

	It("takes the selector as the first argument in place of the flag", func(ctx SpecContext) {
		response := get(url.Values{"args": {"orders.Submit"}, "direction": {"callers"}, "depth": {"1"}})
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var result query.GraphResult
		Expect(json.Unmarshal(response.Body.Bytes(), &result)).To(Succeed())
		labels := make([]string, 0, len(result.Nodes))
		for _, node := range result.Nodes {
			labels = append(labels, node.Label)
		}
		Expect(labels).To(ConsistOf("Submit", "Checkout"))

		runtime := &commandRuntime{database: database}
		root := newRootCommand(runtime)
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs([]string{"graph", "orders.Missing", "--direction", "callers"})
		Expect(root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))).To(MatchError(ContainSubstring("orders.Missing")))
	})
})
