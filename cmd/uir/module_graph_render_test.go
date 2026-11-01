package main

import (
	"fmt"
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
	It("draws SQL native entity and relationship kinds instead of inferred function icons", func() {
		service := graph.Node{ID: "service", Kind: "k8s.service", Label: "checkout", Identifier: uir.Identifier{NodeType: uir.NodeTypeFunction}}
		pod := graph.Node{ID: "pod", Kind: "k8s.pod", Label: "checkout-pod", Depth: 1, Identifier: uir.Identifier{NodeType: uir.NodeTypeFunction}}
		result := moduleGraphResult{GraphResult: query.GraphResult{Graph: graph.Graph{
			Roots: []string{service.ID}, Nodes: []graph.Node{service, pod},
			Edges: []graph.Edge{
				{From: service.ID, To: pod.ID, Type: uir.RelationshipTypeReference, Kind: "k8s.selects"},
				{From: service.ID, To: pod.ID, Type: uir.RelationshipTypeReference, Kind: "k8s.uses_config"},
			},
		}}}

		Expect(graphLines(result)).To(Equal([]string{
			"k8s.service checkout",
			"╰── outgoing",
			"    ├── → k8s.selects k8s.pod checkout-pod",
			"    ╰── → k8s.uses_config k8s.pod checkout-pod",
		}))
	})

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

	It("draws the readers of a Go field when the access names reads, and refuses its graph under calls alone", func(ctx SpecContext) {
		database := indexOrders(ctx)
		_, err := graphModules(ctx, database, moduleGraphOptions{Selector: "orders.Order.Total", Direction: "callers", Depth: 1})
		Expect(err).To(MatchError(ContainSubstring("the graph of field example.org/orders.Order.Total needs read or write access, access call follows neither")),
			"calls alone reach no field")

		reads, err := graphModules(ctx, database, moduleGraphOptions{Selector: "orders.Order.Total", Direction: "callers", Depth: 1, Access: []string{"read,write"}})
		Expect(err).ToNot(HaveOccurred())
		readers := map[string]uir.RelationshipType{}
		for _, edge := range reads.Edges {
			for _, node := range reads.Nodes {
				if node.ID == edge.From {
					readers[node.Label] = edge.Type
				}
			}
		}
		Expect(readers).To(Equal(map[string]uir.RelationshipType{"Submit": uir.RelationshipTypeRead, "charge": uir.RelationshipTypeRead}))
		Expect(graphLines(reads)).To(Equal([]string{
			fmt.Sprintf("𝑣 Order.Total example.org/orders orders.go:%d", ordersLine("Total int")),
			"╰── callers",
			fmt.Sprintf("    ├── → reads ƒ Submit orders.go:%d Total", ordersLine("if order.Total > 0")),
			fmt.Sprintf("    ╰── → reads ƒ charge ×2 [order.Rush] orders.go:%d Total", ordersLine("charge(Order{Total: order.Total})")),
		}), "a read site is the field's name under the guards that reach it")
	})

	It("refuses an access that is not call, read or write", func(ctx SpecContext) {
		_, err := graphModules(ctx, indexOrders(ctx), moduleGraphOptions{Selector: "orders.Submit", Access: []string{"import"}})
		Expect(err).To(MatchError(ContainSubstring(`access "import" is not one of call, read, write`)))
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

	It("marks the changed nodes of a change query and lists the path to each and the changed dependencies", func() {
		node := func(id string, depth, line int) graph.Node {
			return graph.Node{ID: id, Kind: "func", Label: id, Group: "example.org/app", Depth: depth, Location: &graph.Location{Path: "app.go", Line: line}}
		}
		call := func(from, to string, line int) graph.Edge {
			return graph.Edge{ID: from + "|" + to + "|call", From: from, To: to, Type: uir.RelationshipTypeCall, Sites: []graph.Site{{Path: "app.go", Line: line, Column: 2, Text: to + "()"}}}
		}
		result := moduleGraphResult{GraphResult: query.GraphResult{
			Graph: graph.Graph{Roots: []string{"A"}, Nodes: []graph.Node{node("A", 0, 1), node("B", 1, 5), node("C", 2, 9)}, Edges: []graph.Edge{call("A", "B", 2), call("B", "C", 6)}},
			Since: &query.GraphChanges{Revision: "v1", Changed: true,
				Nodes:        []query.GraphChangedNode{{ID: "C", Reason: query.GraphChangeBody, Path: []string{"A", "B", "C"}}},
				Dependencies: []query.GraphDependencyChange{{Module: "gorm.io/gorm", From: "v1.0.0", To: "v1.1.0", Excluded: true}},
			},
		}}
		Expect(graphLines(result)).To(Equal([]string{
			"ƒ A example.org/app app.go:1",
			"├── callees",
			"│   ╰── ƒ B app.go:2",
			"│       ╰── ƒ C changed: body app.go:6",
			"╰── changed since v1",
			"    ├── body C: A → B → C",
			"    ╰── dependency gorm.io/gorm v1.0.0 → v1.1.0, excluded from the walk",
		}))

		result.Since = &query.GraphChanges{Revision: "v1", Nodes: []query.GraphChangedNode{}, Dependencies: []query.GraphDependencyChange{}}
		Expect(graphLines(result)[len(graphLines(result))-1]).To(Equal("╰── unchanged since v1"))
	})

	It("names a removed node and edge from the earlier graph", func() {
		root := graph.Node{ID: "root", Kind: "func", Label: "Run", Group: "example.org/app"}
		removed := graph.Node{ID: "removed", Kind: "method", Label: "Mail.Notify", Depth: 1}
		result := moduleGraphResult{GraphResult: query.GraphResult{
			Graph: graph.Graph{Roots: []string{root.ID}, Nodes: []graph.Node{root}},
			Since: &query.GraphChanges{Revision: "v1", Changed: true,
				Before: &graph.Graph{Roots: []string{root.ID}, Nodes: []graph.Node{root, removed}},
				Nodes:  []query.GraphChangedNode{{ID: removed.ID, Reason: query.GraphChangeRemoved, Path: []string{root.ID, removed.ID}}},
				Edges:  []query.GraphChangedEdge{{ID: "root|removed|dispatch", Reason: query.GraphChangeRemoved, Path: []string{root.ID, removed.ID}}},
			},
		}}
		Expect(graphLines(result)).To(Equal([]string{
			"ƒ Run example.org/app",
			"╰── changed since v1",
			"    ├── removed Mail.Notify: Run → Mail.Notify",
			"    ╰── removed edge Run → Mail.Notify",
		}))
		result.Since.Nodes = nil
		Expect(graphLines(result)).To(ContainElement("    ╰── removed edge Run → Mail.Notify"))
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
