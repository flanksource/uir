package graph_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
)

func TestGraphSpecs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Graph")
}

// stubSource is a call graph written as "caller>callee" edges. A name starting
// with "?" is an unresolved target, and asking for its neighbours is an error,
// since Build must treat it as a leaf.
type stubSource struct {
	edges   []stubEdge
	reverse bool
}

type stubEdge struct {
	from, to string
	kind     uir.RelationshipType
	sites    []graph.Site
}

func calls(edges ...string) stubSource {
	var src stubSource
	for _, edge := range edges {
		from, to, ok := strings.Cut(edge, ">")
		if !ok {
			panic(fmt.Sprintf("stub edge %q is not caller>callee", edge))
		}
		src.edges = append(src.edges, stubEdge{from: from, to: to, kind: uir.RelationshipTypeCall})
	}
	return src
}

func (s stubSource) with(edge stubEdge) stubSource {
	s.edges = append(append([]stubEdge{}, s.edges...), edge)
	return s
}

// reversed reports the same steps in the opposite order, which must not change
// the graph Build returns.
func (s stubSource) reversed() stubSource {
	s.reverse = true
	return s
}

func stubNode(id string) graph.Node {
	return graph.Node{ID: id, Label: id, Kind: string(uir.NodeTypeMethod), Unresolved: strings.HasPrefix(id, "?")}
}

func (s stubSource) Describe(_ context.Context, id string) (graph.Node, error) {
	for _, edge := range s.edges {
		if edge.from == id || edge.to == id {
			return stubNode(id), nil
		}
	}
	return graph.Node{}, fmt.Errorf("no such node %q", id)
}

func (s stubSource) steps(id string, neighbour func(stubEdge) (string, bool)) ([]graph.Step, error) {
	if strings.HasPrefix(id, "?") {
		return nil, fmt.Errorf("unresolved node %q was expanded", id)
	}
	var steps []graph.Step
	for _, edge := range s.edges {
		other, ok := neighbour(edge)
		if !ok {
			continue
		}
		step := graph.Step{Node: stubNode(other), Edge: graph.Edge{Type: edge.kind, Sites: edge.sites}}
		if s.reverse {
			steps = append([]graph.Step{step}, steps...)
			continue
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func (s stubSource) Out(_ context.Context, id string) ([]graph.Step, error) {
	return s.steps(id, func(edge stubEdge) (string, bool) { return edge.to, edge.from == id })
}

func (s stubSource) In(_ context.Context, id string) ([]graph.Step, error) {
	return s.steps(id, func(edge stubEdge) (string, bool) { return edge.from, edge.to == id })
}

// shape is a graph reduced to what most specs assert: each node as id@depth
// with its in/out totals, each edge by id, and what was left out.
type shape struct {
	Nodes   []string
	Edges   []string
	Omitted graph.Omitted
}

func shapeOf(g *graph.Graph) shape {
	GinkgoHelper()
	Expect(g).NotTo(BeNil())
	out := shape{Nodes: []string{}, Edges: []string{}, Omitted: g.Omitted}
	for _, node := range g.Nodes {
		out.Nodes = append(out.Nodes, fmt.Sprintf("%s@%d in=%d out=%d", node.ID, node.Depth, node.In, node.Out))
	}
	for _, edge := range g.Edges {
		out.Edges = append(out.Edges, edge.ID)
	}
	return out
}

func build(src graph.Source, direction graph.Direction, depth int, roots ...string) *graph.Graph {
	GinkgoHelper()
	g, err := graph.Build(context.Background(), src, roots, graph.Options{Direction: direction, Depth: depth, Limit: graph.DefaultLimit})
	Expect(err).NotTo(HaveOccurred())
	return g
}
