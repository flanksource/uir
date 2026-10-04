package graph_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
)

// mapSource is a Source of nodes by id and the steps each makes; In is the
// reverse of Out.
type mapSource struct {
	nodes map[string]graph.Node
	out   map[string][]graph.Step
}

func (s mapSource) Describe(_ context.Context, id string) (graph.Node, error) {
	node, ok := s.nodes[id]
	if !ok {
		return graph.Node{}, fmt.Errorf("no such node %q", id)
	}
	return node, nil
}

func (s mapSource) Out(_ context.Context, id string) ([]graph.Step, error) {
	return s.out[id], nil
}

func (s mapSource) In(_ context.Context, id string) ([]graph.Step, error) {
	var steps []graph.Step
	for _, from := range slices.Sorted(maps.Keys(s.out)) {
		for _, step := range s.out[from] {
			if step.Node.ID == id {
				steps = append(steps, graph.Step{Node: s.nodes[from], Edge: step.Edge})
			}
		}
	}
	return steps, nil
}

var _ = Describe("Absorb", func() {
	// txn runs a packet and a StatusChange attached to it, and calls fee; the
	// packet calls fee, runs the StatusChange too, and writes field; the
	// StatusChange writes field. Packets and StatusChanges are absorbed into
	// the transaction they are attached to, unless one is the root.
	node := func(id, kind string) graph.Node {
		return graph.Node{ID: id, Kind: kind, Label: id, Properties: map[string]string{"transaction": "txn"}}
	}
	nodes := map[string]graph.Node{
		"txn": {ID: "txn", Kind: "transaction", Label: "txn"}, "packet": node("packet", "packet"), "status": node("status", "statuschange"),
		"fee": {ID: "fee", Kind: "function", Label: "fee"}, "field": {ID: "field", Kind: "field", Label: "field"},
	}
	step := func(to string, typ uir.RelationshipType, path string, line int) graph.Step {
		return graph.Step{Node: nodes[to], Edge: graph.Edge{Type: typ, Sites: []graph.Site{{Path: path, Line: line}}}}
	}
	call, write := uir.RelationshipTypeCall, uir.RelationshipTypeWrite
	src := mapSource{nodes: nodes, out: map[string][]graph.Step{
		"txn":    {step("packet", call, "txn", 2), step("status", call, "txn", 3), step("fee", call, "txn", 5)},
		"packet": {step("fee", call, "packet", 4), step("status", call, "packet", 6), step("field", write, "packet", 8)},
		"status": {step("field", write, "status", 4)},
	}}
	absorbedInto := func(root string) func(graph.Node) (string, bool, error) {
		return func(n graph.Node) (string, bool, error) {
			if n.ID == root || (n.Kind != "packet" && n.Kind != "statuschange") {
				return "", false, nil
			}
			return n.Properties["transaction"], true, nil
		}
	}
	absorbed := func(direction graph.Direction, root string) *graph.Graph {
		GinkgoHelper()
		return build(graph.Absorb(src, absorbedInto(root)), direction, 1, root)
	}
	sitesOf := func(g *graph.Graph) map[string][]graph.Site {
		sites := map[string][]graph.Site{}
		for _, edge := range g.Edges {
			sites[edge.ID] = edge.Sites
		}
		return sites
	}

	It("draws what an absorbed node does as its caller's own edges, each site once though it is reached twice", func() {
		g := absorbed(graph.DirectionCallees, "txn")

		Expect(shapeOf(g).Nodes).To(Equal([]string{"txn@0 in=0 out=2", "fee@1 in=1 out=0", "field@1 in=1 out=0"}))
		Expect(sitesOf(g)).To(Equal(map[string][]graph.Site{
			"txn|fee|call":    {{Path: "packet", Line: 4}, {Path: "txn", Line: 5}},
			"txn|field|write": {{Path: "packet", Line: 8}, {Path: "status", Line: 4}},
		}))
	})

	It("draws an absorbed caller as the node it is absorbed into", func() {
		Expect(shapeOf(absorbed(graph.DirectionCallers, "field")).Edges).To(Equal([]string{"txn|field|write"}))
	})

	It("draws an absorbed node as itself when it is the root", func() {
		Expect(shapeOf(absorbed(graph.DirectionCallees, "packet")).Edges).To(Equal([]string{"packet|fee|call", "packet|field|write"}))
	})

	It("absorbs a node with no parent among callees, and fails when it is met as a caller", func() {
		orphaned := graph.Absorb(src, func(n graph.Node) (string, bool, error) {
			return "", n.Kind == "statuschange", nil
		})
		callees := build(orphaned, graph.DirectionCallees, 1, "txn")
		_, err := graph.Build(context.Background(), orphaned, []string{"field"}, graph.Options{Direction: graph.DirectionCallers, Depth: 1, Limit: 10})

		// The packet, drawn now, has its own edges back to what the transaction reaches.
		Expect(shapeOf(callees).Edges).To(Equal([]string{"packet|fee|call", "packet|field|write", "txn|fee|call", "txn|field|write", "txn|packet|call"}))
		Expect(err).To(MatchError(ContainSubstring(`graph: "status", a caller of "field", is absorbed into no node it can be drawn as`)))
	})

	It("fails on absorbed nodes whose callees absorb each other, naming the cycle", func() {
		cyclic := mapSource{nodes: nodes, out: map[string][]graph.Step{
			"txn":    {step("packet", call, "txn", 2), step("fee", call, "txn", 5)},
			"packet": {step("status", call, "packet", 6)},
			"status": {step("packet", call, "status", 3), step("field", write, "status", 4)},
		}}
		_, err := graph.Build(context.Background(), graph.Absorb(cyclic, absorbedInto("txn")), []string{"txn"},
			graph.Options{Direction: graph.DirectionCallees, Depth: 1, Limit: 10})

		Expect(err).To(MatchError(ContainSubstring(`graph: absorbing "packet" into "status": it is absorbed into itself through txn > packet > status > packet`)))
	})

	It("fails when the absorbing function does, naming the node", func() {
		broken := errors.New("no catalog entry")
		failing := graph.Absorb(src, func(n graph.Node) (string, bool, error) {
			if n.ID == "status" {
				return "", false, broken
			}
			return "", false, nil
		})
		_, err := graph.Build(context.Background(), failing, []string{"txn"}, graph.Options{Direction: graph.DirectionCallees, Depth: 1, Limit: 10})

		Expect(err).To(MatchError(broken))
		Expect(err).To(MatchError(ContainSubstring(`graph: absorbing "status"`)))
	})
})
