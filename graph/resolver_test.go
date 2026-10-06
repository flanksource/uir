package graph_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
)

// stubResolver resolves the references each body makes as listed.
type stubResolver struct {
	nodes map[string]graph.Node
	refs  map[string][]graph.Reference
}

func (r stubResolver) Describe(_ context.Context, id string) (graph.Node, error) {
	node, ok := r.nodes[id]
	if !ok {
		return graph.Node{}, fmt.Errorf("no such node %q", id)
	}
	return node, nil
}

func (r stubResolver) References(_ context.Context, id string) ([]graph.Reference, error) {
	return r.refs[id], nil
}

func (r stubResolver) Referrers(_ context.Context, id string) ([]graph.Referrer, error) {
	var referrers []graph.Referrer
	for _, from := range []string{"charge", "other"} {
		for _, ref := range r.refs[from] {
			referrers = append(referrers, graph.Referrer{Node: r.nodes[from], Reference: ref})
		}
	}
	return referrers, nil
}

var _ = Describe("FromResolver", func() {
	node := func(id string) graph.Node { return graph.Node{ID: id, Label: id, Kind: "rule"} }
	target := func(id string, properties map[string]string) graph.Target {
		return graph.Target{Node: node(id), Properties: properties}
	}
	planA, planB := map[string]string{"plans": "PlanA"}, map[string]string{"plans": "PlanB"}
	site := func(line int) []graph.Site { return []graph.Site{{Path: "charge", Line: line}} }
	resolver := stubResolver{
		nodes: map[string]graph.Node{
			"charge": node("charge"), "other": node("other"), "leaf": node("leaf"),
			"fee-a": node("fee-a"), "fee-b": node("fee-b"), "dup-1": node("dup-1"), "dup-2": node("dup-2"),
		},
		refs: map[string][]graph.Reference{
			"charge": {
				{Kind: "copybook", Name: "Leaf", Sites: site(2), Resolution: graph.Resolution{Status: graph.ResolutionStatic, Targets: []graph.Target{target("leaf", nil)}}},
				{Kind: "function", Name: "Fee", Sites: site(4), Resolution: graph.Resolution{Status: graph.ResolutionDynamic, Targets: []graph.Target{target("fee-a", planA), target("fee-b", planB)}}},
				{Kind: "transaction", Name: "Dup", Sites: site(6), Resolution: graph.Resolution{Status: graph.ResolutionAmbiguous, Candidates: []graph.Target{target("dup-1", nil), target("dup-2", nil)}}},
				{Kind: "function", Name: "Function-Missing", Sites: site(8), Resolution: graph.Resolution{Status: graph.ResolutionUnresolved}},
			},
			"other": {
				{Kind: "function", Name: "Fee", Resolution: graph.Resolution{Status: graph.ResolutionStatic, Targets: []graph.Target{target("fee-b", nil)}}},
			},
		},
	}
	edges := func(g *graph.Graph) map[string]graph.Edge {
		byID := map[string]graph.Edge{}
		for _, edge := range g.Edges {
			edge.Sites = nil
			byID[edge.ID] = edge
		}
		return byID
	}
	call, dispatch := uir.RelationshipTypeCall, uir.RelationshipTypeDispatch

	It("draws a static reference as a call, a dynamic or ambiguous one as a dispatch to each target, and an unresolved one to a leaf", func() {
		g := build(graph.FromResolver(resolver), graph.DirectionCallees, 1, "charge")
		missing := graph.UnresolvedID("function", "Function-Missing")

		Expect(edges(g)).To(Equal(map[string]graph.Edge{
			"charge|leaf|call":            {ID: "charge|leaf|call", From: "charge", To: "leaf", Type: call, Kind: "copybook"},
			"charge|fee-a|dispatch":       {ID: "charge|fee-a|dispatch", From: "charge", To: "fee-a", Type: dispatch, Kind: "function", Properties: planA},
			"charge|fee-b|dispatch":       {ID: "charge|fee-b|dispatch", From: "charge", To: "fee-b", Type: dispatch, Kind: "function", Properties: planB},
			"charge|dup-1|dispatch":       {ID: "charge|dup-1|dispatch", From: "charge", To: "dup-1", Type: dispatch, Kind: "transaction", Properties: map[string]string{"status": "ambiguous"}},
			"charge|dup-2|dispatch":       {ID: "charge|dup-2|dispatch", From: "charge", To: "dup-2", Type: dispatch, Kind: "transaction", Properties: map[string]string{"status": "ambiguous"}},
			"charge|" + missing + "|call": {ID: "charge|" + missing + "|call", From: "charge", To: missing, Type: call, Kind: "function"},
		}))
		Expect(g.Nodes).To(ContainElement(graph.Node{ID: missing, Kind: "function", Label: "Function-Missing", Depth: 1, In: 1, Unresolved: true}))
		Expect(g.Edges[0].Sites).NotTo(BeEmpty(), "every edge carries its reference's sites")
	})

	It("draws each referrer whose reference reaches the node, as its callees draw it", func() {
		g := build(graph.FromResolver(resolver), graph.DirectionCallers, 1, "fee-b")

		Expect(edges(g)).To(Equal(map[string]graph.Edge{
			"charge|fee-b|dispatch": {ID: "charge|fee-b|dispatch", From: "charge", To: "fee-b", Type: dispatch, Kind: "function", Properties: planB},
			"other|fee-b|call":      {ID: "other|fee-b|call", From: "other", To: "fee-b", Type: call, Kind: "function"},
		}))
	})

	It("draws no edge from an ambiguous reference to a node none of its candidates is", func() {
		ambiguous := graph.Reference{Kind: "transaction", Name: "Dup", Resolution: graph.Resolution{
			Status: graph.ResolutionAmbiguous, Candidates: []graph.Target{target("dup-1", nil), target("dup-2", nil)},
		}}
		_, reachesOther, err := ambiguous.EdgeTo("leaf")
		Expect(err).NotTo(HaveOccurred())
		edge, reachesCandidate, err := ambiguous.EdgeTo("dup-2")
		Expect(err).NotTo(HaveOccurred())

		Expect([]any{reachesOther, reachesCandidate, edge.Type, edge.Properties}).To(Equal([]any{
			false, true, dispatch, map[string]string{"status": "ambiguous"},
		}))
	})

	DescribeTable("refuses a resolution its status cannot draw",
		func(resolution graph.Resolution, message string) {
			_, err := graph.Reference{Kind: "copybook", Name: "Leaf", Resolution: resolution}.Steps()
			Expect(err).To(MatchError(message))
		},
		Entry("a static reference with no target", graph.Resolution{Status: graph.ResolutionStatic},
			`graph: copybook "Leaf" is static with 0 targets, not one`),
		Entry("a static reference with two", graph.Resolution{Status: graph.ResolutionStatic, Targets: []graph.Target{{Node: node("a")}, {Node: node("b")}}},
			`graph: copybook "Leaf" is static with 2 targets, not one`),
		Entry("an unknown status", graph.Resolution{Status: "guessed"},
			`graph: copybook "Leaf" has resolution status "guessed", not static, dynamic, ambiguous or unresolved`),
	)
})
