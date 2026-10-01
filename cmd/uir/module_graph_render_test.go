package main

import (
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// graphLines is the plain text of the rendered graph, line by line. The renderer joins the parts of
// a label with non-breaking spaces, which are read here as the spaces they print as.
func graphLines(result moduleGraphResult) []string {
	return strings.Split(strings.ReplaceAll(result.Pretty().String(), " ", " "), "\n")
}

var _ = Describe("module call graph rendering", func() {
	It("draws the callers and the callees of the root with guards, site counts and markers", func(ctx SpecContext) {
		result, err := graphModules(ctx, indexOrders(ctx), moduleGraphOptions{Selector: "orders.Submit", RootKey: ordersRoot})
		Expect(err).ToNot(HaveOccurred())
		Expect(graphLines(result)).To(Equal([]string{
			"ƒ Submit example.org/orders orders.go:16",
			"├── callers",
			"│   ╰── ƒ Checkout orders.go:35",
			"├── callees",
			"│   ├── ƒ charge ×2 [order.Total > 0] orders.go:18",
			"│   │   ╰── ƒ charge [order.Rush] orders.go:31 ↩ expanded elsewhere",
			"│   ├── hook unresolved [hook != nil] orders.go:24",
			"│   ├── ƒ Notifier.Notify orders.go:26",
			"│   ╰── → dispatch ƒ Mail.Notify orders.go:26",
			"╰── omitted: 1 unresolved",
		}))
	})

	It("leaves out the subtree of a direction with nothing in it", func(ctx SpecContext) {
		result, err := graphModules(ctx, indexOrders(ctx), moduleGraphOptions{Selector: "orders.Checkout", Depth: 1})
		Expect(err).ToNot(HaveOccurred())
		Expect(graphLines(result)).To(Equal([]string{
			"ƒ Checkout example.org/orders orders.go:35",
			"├── callees",
			"│   ╰── ƒ Submit orders.go:35",
			"╰── omitted: 1 beyond depth",
		}))
	})

	It("draws a function that calls itself as its own caller and callee, expanded once", func(ctx SpecContext) {
		result, err := graphModules(ctx, indexOrders(ctx), moduleGraphOptions{Selector: "orders.charge", Direction: "callers", Depth: 2})
		Expect(err).ToNot(HaveOccurred())
		Expect(graphLines(result)).To(Equal([]string{
			"ƒ charge example.org/orders orders.go:29",
			"├── callers",
			"│   ├── ƒ Submit ×2 [order.Total > 0] orders.go:18",
			"│   │   ╰── ƒ Checkout orders.go:35",
			"│   ╰── ƒ charge [order.Rush] orders.go:31 ↩ expanded elsewhere",
			"╰── callees",
			"    ╰── ƒ charge [order.Rush] orders.go:31 ↩ expanded elsewhere",
		}))
	})

	It("lists the candidates of an ambiguous selector with the id that selects each", func(ctx SpecContext) {
		result, err := graphModules(ctx, indexOrders(ctx), moduleGraphOptions{Selector: "Notify"})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Candidates).To(HaveLen(2))
		Expect(graphLines(result)).To(Equal([]string{
			"2 candidates: narrow the selector or pass --symbol",
			"├── ƒ " + result.Candidates[0].QueryName + " " + result.Candidates[0].ID,
			"╰── ƒ " + result.Candidates[1].QueryName + " " + result.Candidates[1].ID,
		}))
	})

	It("names the package of a callee outside the caller's, and summarises what the graph left out", func() {
		root := graph.Node{ID: "root", Kind: "func", Label: "Run", Group: "example.org/app", Out: 4,
			Location: &graph.Location{Path: "app.go", Line: 3}}
		external := graph.Node{ID: "external", Kind: "func", Label: "New", Group: "errors", Depth: 1, Out: 2}
		builtin := graph.Node{ID: "builtin", Kind: "builtin", Label: "len", Depth: 1}
		call := func(to graph.Node, line int, text string) graph.Edge {
			return graph.Edge{ID: root.ID + "|" + to.ID + "|call", From: root.ID, To: to.ID, Type: uir.RelationshipTypeCall, Sites: []graph.Site{{Path: "app.go", Line: line, Column: 2, Text: text}}}
		}
		result := moduleGraphResult{GraphResult: query.GraphResult{Graph: graph.Graph{
			Roots: []string{root.ID}, Nodes: []graph.Node{root, builtin, external},
			Edges: []graph.Edge{call(builtin, 6, "len"), call(external, 5, "errors.New")},
			Omitted: graph.Omitted{NodeLimit: true, BeyondDepth: 1, Unresolved: 2, UnreadableSource: []string{"a.go", "b.go"},
				Excluded: map[string]int{"strings": 1, "fmt": 3, "builtin": 1}},
		}}}
		Expect(graphLines(result)).To(Equal([]string{
			"ƒ Run example.org/app app.go:3",
			"├── callees",
			"│   ├── ƒ New errors app.go:5",
			"│   ╰── len app.go:6",
			"├── omitted: node limit reached; 1 beyond depth; 2 unresolved; guards unreadable in a.go, b.go",
			"╰── excluded: fmt 3, builtin 1, strings 1",
		}))
	})
})
