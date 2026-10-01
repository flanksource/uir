package graph_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
)

const call = uir.RelationshipTypeCall

type failingSource struct{ stubSource }

var errSourceDown = errors.New("index unavailable")

func (failingSource) Out(context.Context, string) ([]graph.Step, error) {
	return nil, errSourceDown
}

// rulesTheme renames every node and drops dispatch edges.
type rulesTheme struct{}

func (rulesTheme) Node(node *graph.Node) {
	node.Label = strings.ToUpper(node.Label)
	node.Kind = "rule"
	node.Tone = "accent"
	node.Group = "rules"
}

func (rulesTheme) Edge(edge *graph.Edge) bool {
	if edge.Type == uir.RelationshipTypeDispatch {
		return false
	}
	edge.Kind = "spawn"
	return true
}

type renamingTheme struct{ rulesTheme }

func (renamingTheme) Node(node *graph.Node) { node.ID = "renamed" }

func danglingEdges(g *graph.Graph) []string {
	present := map[string]bool{}
	for _, node := range g.Nodes {
		present[node.ID] = true
	}
	dangling := []string{}
	for _, edge := range g.Edges {
		if !present[edge.From] || !present[edge.To] {
			dangling = append(dangling, edge.ID)
		}
	}
	return dangling
}

var _ = Describe("Build", func() {
	It("ends at a cycle, keeping the back edge", func() {
		Expect(shapeOf(build(calls("a>b", "b>a"), graph.DirectionCallees, 3, "a"))).To(Equal(shape{
			Nodes: []string{"a@0 in=1 out=1", "b@1 in=1 out=1"},
			Edges: []string{"a|b|call", "b|a|call"},
		}))
	})

	It("keeps a self edge once, however many directions report it", func() {
		src := stubSource{edges: []stubEdge{{from: "a", to: "a", kind: call, sites: []graph.Site{{Path: "a.go", Line: 4}}}}}
		g := build(src, graph.DirectionBoth, 2, "a")

		Expect(shapeOf(g)).To(Equal(shape{Nodes: []string{"a@0 in=1 out=1"}, Edges: []string{"a|a|call"}}))
		Expect(g.Edges[0].Sites).To(Equal([]graph.Site{{Path: "a.go", Line: 4}}))
	})

	It("merges the call sites of one edge in source order, once", func() {
		src := stubSource{edges: []stubEdge{
			{from: "a", to: "b", kind: call, sites: []graph.Site{{Path: "b.go", Line: 20, Column: 2, Text: "b(2)"}}},
			{from: "a", to: "b", kind: call, sites: []graph.Site{{Path: "b.go", Line: 10, Column: 9, Text: "b(1)"}}},
			{from: "a", to: "b", kind: call, sites: []graph.Site{{Path: "b.go", Line: 10, Column: 3, Text: "b(0)"}}},
			{from: "a", to: "b", kind: call, sites: []graph.Site{{Path: "a.go", Line: 99, Text: "b(3)"}}},
		}}

		Expect(build(src, graph.DirectionBoth, 1, "a", "b").Edges).To(Equal([]graph.Edge{{
			ID: "a|b|call", From: "a", To: "b", Type: call,
			Sites: []graph.Site{
				{Path: "a.go", Line: 99, Text: "b(3)"},
				{Path: "b.go", Line: 10, Column: 3, Text: "b(0)"},
				{Path: "b.go", Line: 10, Column: 9, Text: "b(1)"},
				{Path: "b.go", Line: 20, Column: 2, Text: "b(2)"},
			},
		}}))
	})

	It("keeps edges of different types between the same nodes apart", func() {
		src := calls("a>b").with(stubEdge{from: "a", to: "b", kind: uir.RelationshipTypeDispatch})
		Expect(shapeOf(build(src, graph.DirectionCallees, 1, "a"))).To(Equal(shape{
			Nodes: []string{"a@0 in=0 out=2", "b@1 in=2 out=0"},
			Edges: []string{"a|b|call", "a|b|dispatch"},
		}))
	})

	It("includes an unresolved target as a leaf and counts its call sites", func() {
		src := calls("a>b", "a>?bare").with(stubEdge{
			from: "a", to: "?x", kind: call,
			sites: []graph.Site{{Path: "a.go", Line: 3, Text: "fn()"}, {Path: "a.go", Line: 8, Text: "fn()"}},
		})
		g := build(src, graph.DirectionCallees, 3, "a")

		Expect(shapeOf(g)).To(Equal(shape{
			Nodes:   []string{"a@0 in=0 out=3", "?bare@1 in=1 out=0", "?x@1 in=1 out=0", "b@1 in=1 out=0"},
			Edges:   []string{"a|?bare|call", "a|?x|call", "a|b|call"},
			Omitted: graph.Omitted{Unresolved: 3},
		}))
		Expect(g.Nodes[1].Unresolved).To(BeTrue())
	})

	It("stops at the depth, counting the frontier nodes that have more and keeping their back edges", func() {
		src := calls("a>b", "b>c", "c>d", "c>a", "d>e")
		Expect(shapeOf(build(src, graph.DirectionCallees, 2, "a"))).To(Equal(shape{
			Nodes:   []string{"a@0 in=1 out=1", "b@1 in=1 out=1", "c@2 in=1 out=2"},
			Edges:   []string{"a|b|call", "b|c|call", "c|a|call"},
			Omitted: graph.Omitted{BeyondDepth: 1},
		}))
	})

	It("reports nothing beyond the depth when the frontier has no further edges", func() {
		Expect(shapeOf(build(calls("a>b", "b>a"), graph.DirectionCallees, 1, "a"))).To(Equal(shape{
			Nodes: []string{"a@0 in=1 out=1", "b@1 in=1 out=1"},
			Edges: []string{"a|b|call", "b|a|call"},
		}))
	})

	It("stops adding nodes at the limit and never leaves an edge dangling", func() {
		g, err := graph.Build(context.Background(), calls("a>b", "a>c", "a>d", "d>b", "b>d"), []string{"a"},
			graph.Options{Direction: graph.DirectionCallees, Depth: 3, Limit: 3})
		Expect(err).NotTo(HaveOccurred())

		Expect(shapeOf(g)).To(Equal(shape{
			Nodes:   []string{"a@0 in=0 out=3", "b@1 in=1 out=1", "c@1 in=1 out=0"},
			Edges:   []string{"a|b|call", "a|c|call"},
			Omitted: graph.Omitted{NodeLimit: true},
		}))
		Expect(danglingEdges(g)).To(BeEmpty())
	})

	It("leaves out the roots that do not fit the limit", func() {
		g, err := graph.Build(context.Background(), calls("a>b", "c>d"), []string{"a", "a", "c"},
			graph.Options{Direction: graph.DirectionCallees, Depth: 1, Limit: 1})
		Expect(err).NotTo(HaveOccurred())

		Expect(g.Roots).To(Equal([]string{"a"}))
		Expect(shapeOf(g)).To(Equal(shape{
			Nodes:   []string{"a@0 in=0 out=1"},
			Edges:   []string{},
			Omitted: graph.Omitted{NodeLimit: true},
		}))
	})

	It("walks callers to negative depths and callees to positive ones, each away from the root only", func() {
		src := calls("x>a", "w>x", "a>b", "b>c", "y>b", "x>z")
		Expect(shapeOf(build(src, graph.DirectionBoth, 2, "a"))).To(Equal(shape{
			Nodes: []string{"w@-2 in=0 out=1", "x@-1 in=1 out=1", "a@0 in=1 out=1", "b@1 in=1 out=1", "c@2 in=1 out=0"},
			Edges: []string{"a|b|call", "b|c|call", "w|x|call", "x|a|call"},
		}))
	})

	It("walks only callers when asked", func() {
		Expect(shapeOf(build(calls("x>a", "w>x", "a>b"), graph.DirectionCallers, 2, "a"))).To(Equal(shape{
			Nodes: []string{"w@-2 in=0 out=1", "x@-1 in=1 out=1", "a@0 in=1 out=0"},
			Edges: []string{"w|x|call", "x|a|call"},
		}))
	})

	It("keeps the first depth of a node reached again, adding only the edge", func() {
		Expect(shapeOf(build(calls("a>b", "a>c", "b>c"), graph.DirectionCallees, 3, "a"))).To(Equal(shape{
			Nodes: []string{"a@0 in=0 out=2", "b@1 in=1 out=1", "c@1 in=2 out=0"},
			Edges: []string{"a|b|call", "a|c|call", "b|c|call"},
		}))
	})

	It("returns the same graph whatever order the source reports steps in", func() {
		src := calls("x>a", "a>d", "a>c", "a>b", "b>c", "c>a", "d>?x").with(stubEdge{
			from: "a", to: "b", kind: uir.RelationshipTypeDispatch, sites: []graph.Site{{Path: "a.go", Line: 2}},
		})
		forward := build(src, graph.DirectionBoth, 3, "a", "c")

		Expect(build(src.reversed(), graph.DirectionBoth, 3, "a", "c")).To(Equal(forward))
		Expect(shapeOf(forward)).To(Equal(shape{
			Nodes: []string{
				"x@-1 in=0 out=1",
				"a@0 in=2 out=4", "c@0 in=2 out=1",
				"b@1 in=2 out=1", "d@1 in=1 out=1",
				"?x@2 in=1 out=0",
			},
			Edges: []string{
				"a|b|call", "a|b|dispatch", "a|c|call", "a|d|call", "b|c|call", "c|a|call", "d|?x|call", "x|a|call",
			},
			Omitted: graph.Omitted{Unresolved: 1},
		}))
	})

	It("applies the theme to every node and edge, and drops the edges it declines", func() {
		src := calls("a>b").with(stubEdge{from: "a", to: "c", kind: uir.RelationshipTypeDispatch})
		g, err := graph.Build(context.Background(), src, []string{"a"},
			graph.Options{Direction: graph.DirectionCallees, Depth: 2, Limit: graph.DefaultLimit, Theme: rulesTheme{}})
		Expect(err).NotTo(HaveOccurred())

		Expect(g).To(Equal(&graph.Graph{
			Roots: []string{"a"},
			Nodes: []graph.Node{
				{ID: "a", Kind: "rule", Label: "A", Group: "rules", Depth: 0, Out: 1, Tone: "accent"},
				{ID: "b", Kind: "rule", Label: "B", Group: "rules", Depth: 1, In: 1, Tone: "accent"},
			},
			Edges:  []graph.Edge{{ID: "a|b|call", From: "a", To: "b", Type: call, Kind: "spawn", Sites: []graph.Site{}}},
			Groups: []graph.Group{{ID: "rules", Label: "rules"}},
		}))
	})

	Describe("exclusions", func() {
		callees := func(depth int, exclude func(graph.Node) bool) graph.Options {
			return graph.Options{Direction: graph.DirectionCallees, Depth: depth, Limit: graph.DefaultLimit, Exclude: exclude}
		}

		It("neither draws, counts nor walks an excluded neighbour, and tallies distinct ones per group", func() {
			src := calls("a>b", "a>fmt.Println", "a>fmt.Errorf", "b>fmt.Println", "fmt.Errorf>c", "a>os.Exit")
			Expect(shapeOf(buildWith(src, callees(3, excludeGroups("fmt", "os")), "a"))).To(Equal(shape{
				Nodes:   []string{"a@0 in=0 out=1", "b@1 in=1 out=0"},
				Edges:   []string{"a|b|call"},
				Omitted: graph.Omitted{Excluded: map[string]int{"fmt": 2, "os": 1}},
			}))
		})

		It("never excludes a root, and draws the edges back into it", func() {
			src := calls("fmt.Errorf>fmt.wrap", "fmt.Errorf>c", "c>fmt.Errorf")
			Expect(shapeOf(buildWith(src, callees(2, excludeGroups("fmt")), "fmt.Errorf"))).To(Equal(shape{
				Nodes:   []string{"fmt.Errorf@0 in=1 out=1", "c@1 in=1 out=1"},
				Edges:   []string{"c|fmt.Errorf|call", "fmt.Errorf|c|call"},
				Omitted: graph.Omitted{Excluded: map[string]int{"fmt": 1}},
			}))
		})

		It("does not tally or count as beyond the depth an excluded node only the frontier reaches", func() {
			src := calls("a>b", "b>fmt.Println", "a>fmt.Errorf")
			Expect(shapeOf(buildWith(src, callees(1, excludeGroups("fmt")), "a"))).To(Equal(shape{
				Nodes:   []string{"a@0 in=0 out=1", "b@1 in=1 out=0"},
				Edges:   []string{"a|b|call"},
				Omitted: graph.Omitted{Excluded: map[string]int{"fmt": 1}},
			}))
		})

		It("reports no exclusions when nothing reached is excluded", func() {
			Expect(buildWith(calls("a>b"), callees(1, excludeGroups("fmt")), "a").Omitted.Excluded).To(BeNil())
		})
	})

	It("refuses a theme that renames a node's id, which its edges refer to", func() {
		_, err := graph.Build(context.Background(), calls("a>b"), []string{"a"},
			graph.Options{Direction: graph.DirectionCallees, Depth: 1, Limit: graph.DefaultLimit, Theme: renamingTheme{}})
		Expect(err).To(MatchError(ContainSubstring(`changed node id "a" to "renamed"`)))
	})

	DescribeTable("refuses options it would otherwise have to guess",
		func(roots []string, opts graph.Options, want string) {
			_, err := graph.Build(context.Background(), calls("a>b"), roots, opts)
			Expect(err).To(MatchError(ContainSubstring(want)))
		},
		Entry("depth zero", []string{"a"}, graph.Options{Direction: graph.DirectionBoth, Depth: 0, Limit: 10}, "depth 0 is outside 1..8"),
		Entry("depth past the maximum", []string{"a"}, graph.Options{Direction: graph.DirectionBoth, Depth: graph.MaxDepth + 1, Limit: 10}, "depth 9 is outside 1..8"),
		Entry("no direction", []string{"a"}, graph.Options{Depth: graph.DefaultDepth, Limit: 10}, `direction ""`),
		Entry("an unknown direction", []string{"a"}, graph.Options{Direction: "sideways", Depth: graph.DefaultDepth, Limit: 10}, `direction "sideways"`),
		Entry("limit zero", []string{"a"}, graph.Options{Direction: graph.DirectionBoth, Depth: graph.DefaultDepth}, "limit 0 is outside 1..1000"),
		Entry("limit past the maximum", []string{"a"}, graph.Options{Direction: graph.DirectionBoth, Depth: graph.DefaultDepth, Limit: graph.MaxLimit + 1}, "limit 1001 is outside 1..1000"),
		Entry("no roots", nil, graph.Options{Direction: graph.DirectionBoth, Depth: graph.DefaultDepth, Limit: 10}, "at least one root"),
	)

	It("reports a root the source cannot describe", func() {
		_, err := graph.Build(context.Background(), calls("a>b"), []string{"missing"},
			graph.Options{Direction: graph.DirectionBoth, Depth: 1, Limit: 10})
		Expect(err).To(MatchError(ContainSubstring(`no such node "missing"`)))
	})

	It("reports a source failure instead of returning a partial graph", func() {
		g, err := graph.Build(context.Background(), failingSource{calls("a>b")}, []string{"a"},
			graph.Options{Direction: graph.DirectionCallees, Depth: 1, Limit: 10})
		Expect(err).To(MatchError(errSourceDown))
		Expect(g).To(BeNil())
	})

	It("stops when its context is cancelled", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := graph.Build(ctx, calls("a>b"), []string{"a"}, graph.Options{Direction: graph.DirectionCallees, Depth: 1, Limit: 10})
		Expect(err).To(MatchError(context.Canceled))
	})

	It("refuses a step that names no edge type or no node", func() {
		untyped := stubSource{edges: []stubEdge{{from: "a", to: "b"}}}
		_, err := graph.Build(context.Background(), untyped, []string{"a"}, graph.Options{Direction: graph.DirectionCallees, Depth: 1, Limit: 10})
		Expect(err).To(MatchError(ContainSubstring("no edge type")))

		unnamed := stubSource{edges: []stubEdge{{from: "a", to: "", kind: call}}}
		_, err = graph.Build(context.Background(), unnamed, []string{"a"}, graph.Options{Direction: graph.DirectionCallees, Depth: 1, Limit: 10})
		Expect(err).To(MatchError(ContainSubstring("no node id")))
	})
})

var _ = Describe("Graph JSON", func() {
	It("writes empty collections as arrays and nothing omitted as an empty object", func() {
		Expect(json.Marshal(build(calls("a>b"), graph.DirectionCallees, 1, "b"))).To(MatchJSON(`{
			"roots": ["b"],
			"nodes": [{"id": "b", "identifier": {}, "kind": "method", "label": "b", "depth": 0, "in": 0, "out": 0}],
			"edges": [],
			"omitted": {}
		}`))
	})

	It("uses the snake_case keys the web client reads", func() {
		g := graph.Graph{
			Roots: []string{"a"},
			Nodes: []graph.Node{{
				ID: "a", Identifier: uir.Identifier{Type: "Orders", Method: "Process"}, Kind: "method", Label: "Process",
				Group: "orders", Depth: -1, In: 1, Out: 2, Unresolved: true,
				Location: &graph.Location{
					RootKey: "root", CheckoutPath: "/src/shop", SnapshotID: "snap", SourceID: "file",
					Path: "orders.go", IdentityKey: "key", Line: 3, Column: 1,
				},
				Tone: "accent", Icon: "method", Properties: map[string]string{"owner": "billing"},
			}},
			Edges: []graph.Edge{{
				ID: "a|a|call", From: "a", To: "a", Type: call, Kind: "spawn",
				Sites: []graph.Site{{Path: "orders.go", Line: 9, Column: 2, Text: "a()", Guards: []string{"ok"}}},
			}},
			Groups:  []graph.Group{{ID: "orders", Label: "Orders", Parent: "shop"}},
			Omitted: graph.Omitted{NodeLimit: true, BeyondDepth: 1, Unresolved: 2, UnreadableSource: []string{"gone.go"}, Excluded: map[string]int{"fmt": 2}},
		}

		Expect(json.Marshal(g)).To(MatchJSON(`{
			"roots": ["a"],
			"nodes": [{
				"id": "a", "identifier": {"type": "Orders", "method": "Process"}, "kind": "method", "label": "Process",
				"group": "orders", "depth": -1, "in": 1, "out": 2, "unresolved": true,
				"location": {
					"root_key": "root", "checkout_path": "/src/shop", "snapshot_id": "snap", "source_id": "file",
					"path": "orders.go", "identity_key": "key", "line": 3, "column": 1
				},
				"tone": "accent", "icon": "method", "properties": {"owner": "billing"}
			}],
			"edges": [{
				"id": "a|a|call", "from": "a", "to": "a", "type": "call", "kind": "spawn",
				"sites": [{"path": "orders.go", "line": 9, "column": 2, "text": "a()", "guards": ["ok"]}]
			}],
			"groups": [{"id": "orders", "label": "Orders", "parent": "shop"}],
			"omitted": {"node_limit": true, "beyond_depth": 1, "unresolved": 2, "unreadable_source": ["gone.go"], "excluded": {"fmt": 2}}
		}`))
	})
})
