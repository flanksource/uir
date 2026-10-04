package graph_test

import (
	"strings"

	"github.com/flanksource/clicky/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
)

// treeLines is the plain text of a rendered graph, line by line. Labels are joined with
// non-breaking spaces, which are read here as the spaces they print as.
func treeLines(text api.Text) []string {
	return strings.Split(strings.ReplaceAll(text.String(), " ", " "), "\n")
}

func edgeOf(from, to graph.Node, kind uir.RelationshipType, sites ...graph.Site) graph.Edge {
	return graph.Edge{ID: from.ID + "|" + to.ID + "|" + string(kind), From: from.ID, To: to.ID, Type: kind, Sites: sites}
}

// drawnGraph is a root with a caller and callees that cover every part of a line: a site count,
// guards, a dispatch edge with properties, a change of group, an unresolved leaf, a node reached
// twice, and a node whose kind is no uir.NodeType.
func drawnGraph() graph.Graph {
	fn := string(uir.NodeTypeFunction)
	run := graph.Node{ID: "run", Kind: fn, Label: "Run", Group: "app", Location: &graph.Location{Path: "app.go", Line: 3}}
	main := graph.Node{ID: "main", Kind: fn, Label: "Main", Group: "app", Depth: -1}
	charge := graph.Node{ID: "charge", Kind: fn, Label: "charge", Group: "app", Depth: 1}
	save := graph.Node{ID: "save", Kind: fn, Label: "save", Group: "app", Depth: 1}
	rates := graph.Node{ID: "rates", Kind: "copybook", Label: "Rates", Group: "app", Depth: 2}
	notify := graph.Node{ID: "notify", Kind: fn, Label: "Mail.Notify", Group: "mail", Depth: 1}
	hook := graph.Node{ID: "hook", Kind: "unresolved", Label: "hook", Group: "app", Depth: 1, Unresolved: true}
	dispatch := edgeOf(run, notify, uir.RelationshipTypeDispatch, graph.Site{Path: "app.go", Line: 8})
	dispatch.Properties = map[string]string{"plans": "gold, silver"}
	return graph.Graph{
		Roots: []string{run.ID},
		Nodes: []graph.Node{main, run, charge, hook, notify, save, rates},
		Edges: []graph.Edge{
			edgeOf(main, run, uir.RelationshipTypeCall, graph.Site{Path: "main.go", Line: 10}),
			edgeOf(run, hook, uir.RelationshipTypeCall, graph.Site{Path: "app.go", Line: 9, Guards: []string{"hook != nil"}}),
			dispatch,
			edgeOf(run, save, uir.RelationshipTypeCall, graph.Site{Path: "app.go", Line: 7}),
			edgeOf(run, charge, uir.RelationshipTypeCall,
				graph.Site{Path: "app.go", Line: 5, Guards: []string{"total > 0", "!rush"}}, graph.Site{Path: "app.go", Line: 6}),
			edgeOf(charge, save, uir.RelationshipTypeCall, graph.Site{Path: "app.go", Line: 20}),
			edgeOf(save, rates, uir.RelationshipTypeCall, graph.Site{Path: "app.go", Line: 22, Guards: []string{"rush"}}),
		},
		Omitted: graph.Omitted{BeyondDepth: 1, Unresolved: 1, Excluded: map[string]int{"os": 1, "fmt": 2}},
	}
}

var _ = Describe("Graph.Pretty", func() {
	It("draws the callers and callees of the root, each node expanded once, then what was left out", func() {
		Expect(treeLines(drawnGraph().Pretty())).To(Equal([]string{
			"ƒ Run app app.go:3",
			"├── callers",
			"│   ╰── ƒ Main main.go:10",
			"├── callees",
			"│   ├── ƒ charge ×2 [total > 0] [!rush] app.go:5",
			"│   │   ╰── ƒ save app.go:20 ↩ expanded elsewhere",
			"│   ├── ƒ save app.go:7",
			"│   │   ╰── Rates [rush] app.go:22",
			"│   ├── → dispatch ƒ Mail.Notify mail app.go:8",
			"│   ╰── hook unresolved [hook != nil] app.go:9",
			"├── omitted: 1 beyond depth; 1 unresolved",
			"╰── excluded: fmt 2, os 1",
		}))
	})

	It("names nodes with the renderer the caller passes", func() {
		upper := graph.TreeOptions{Name: func(node graph.Node) api.Text { return api.Text{Content: strings.ToUpper(node.Label)} }}
		g := drawnGraph()
		g.Edges, g.Omitted = g.Edges[:1], graph.Omitted{NodeLimit: true, UnreadableSource: []string{"a.go", "b.go"}}
		Expect(treeLines(api.Text{}.Add(api.NewTree(g.Tree(upper))))).To(Equal([]string{
			"RUN app app.go:3",
			"├── callers",
			"│   ╰── MAIN main.go:10",
			"╰── omitted: node limit reached; guards unreadable in a.go, b.go",
		}))
	})

	It("labels read and write edges with their access and kind, and names what the first site reads or writes", func() {
		rule := graph.Node{ID: "rule", Kind: "copybook", Label: "Settle", Group: "Plan"}
		policy := graph.Node{ID: "table:AsPolicy", Kind: "db-table", Label: "AsPolicy", Group: "Database", Depth: 1}
		log := graph.Node{ID: "table:AsPolicyLog", Kind: "db-table", Label: "AsPolicyLog", Group: "Database", Depth: 1}
		status := graph.Node{ID: "field:Policy.Status", Kind: "field", Label: "Status", Group: "Plan", Depth: 1}
		update := graph.Node{ID: "procedure:Update", Kind: "procedure", Label: "Update", Group: "Database", Depth: 1}
		read := edgeOf(rule, policy, uir.RelationshipTypeRead,
			graph.Site{Path: "rule", Line: 4, Text: "SELECT STATUSCODE FROM AsPolicy WHERE POLICYGUID = @p1"}, graph.Site{Path: "rule", Line: 9, Text: "MathVariable"})
		read.Kind = "sql"
		write := edgeOf(rule, log, uir.RelationshipTypeWrite, graph.Site{Path: "rule", Line: 6, Guards: []string{"rush"}, Text: "INSERT INTO AsPolicyLog (PolicyGUID) VALUES (@p1)"})
		write.Kind = "sql"
		copyTo := edgeOf(rule, status, uir.RelationshipTypeWrite, graph.Site{Path: "rule", Line: 7, Text: "CopyToPolicyFields"})
		copyTo.Kind = "copyto, mathupdate"
		exec := edgeOf(rule, update, uir.RelationshipTypeCall, graph.Site{Path: "rule", Line: 8, Text: "Query"})
		exec.Kind = "exec"
		g := graph.Graph{Roots: []string{rule.ID}, Nodes: []graph.Node{rule, policy, log, status, update}, Edges: []graph.Edge{read, write, copyTo, exec}}

		Expect(treeLines(g.Pretty())).To(Equal([]string{
			"Settle Plan",
			"╰── callees",
			"    ├── → reads sql AsPolicy Database ×2 rule:4 SELECT STATUSCODE FROM AsPolicy WHERE POLICYGUID = @p1",
			"    ├── → writes sql AsPolicyLog Database [rush] rule:6 INSERT INTO AsPolicyLog (PolicyGUID) VALUES (@p1)",
			"    ├── → writes copyto, mathupdate Status rule:7 CopyToPolicyFields",
			"    ╰── Update Database rule:8",
		}))
	})

	It("draws a read or write edge with no sites by its access and kind alone", func() {
		rule := graph.Node{ID: "rule", Kind: "copybook", Label: "Settle"}
		policy := graph.Node{ID: "table:AsPolicy", Kind: "db-table", Label: "AsPolicy", Depth: -1}
		read := edgeOf(policy, rule, uir.RelationshipTypeRead)
		read.Kind = "sql"
		g := graph.Graph{Roots: []string{rule.ID}, Nodes: []graph.Node{rule, policy}, Edges: []graph.Edge{read}}

		Expect(treeLines(g.Pretty())).To(Equal([]string{
			"Settle",
			"╰── callers",
			"    ╰── → reads sql AsPolicy",
		}))
	})

	It("says so when the graph has no root", func() {
		Expect(treeLines(graph.Graph{}.Pretty())).To(Equal([]string{"no call graph"}))
	})
})
