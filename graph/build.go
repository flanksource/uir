package graph

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/flanksource/uir"
)

// Build walks src breadth-first from roots. Callees are walked from the roots
// and from nodes at positive depth, callers from the roots and from nodes at
// negative depth, so a caller's other callees are not pulled in. A node reached
// again keeps its first depth and only gains the edge, which is how a cycle
// ends. Nodes at Options.Depth are still asked for their neighbours, to draw
// the edges back into the graph and to count what lies beyond.
func Build(ctx context.Context, src Source, roots []string, opts Options) (*Graph, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return nil, errors.New("graph: at least one root is required")
	}
	b := &builder{src: src, opts: opts, nodes: map[string]*Node{}, edges: map[string]Edge{}}
	for _, root := range roots {
		if err := b.addRoot(ctx, root); err != nil {
			return nil, err
		}
	}
	for len(b.queue) > 0 {
		id := b.queue[0]
		b.queue = b.queue[1:]
		if err := b.expand(ctx, b.nodes[id]); err != nil {
			return nil, err
		}
	}
	return b.graph()
}

func (o Options) validate() error {
	switch o.Direction {
	case DirectionCallees, DirectionCallers, DirectionBoth:
	default:
		return fmt.Errorf("graph: direction %q is not one of %s, %s, %s", o.Direction, DirectionCallees, DirectionCallers, DirectionBoth)
	}
	if o.Depth < 1 || o.Depth > MaxDepth {
		return fmt.Errorf("graph: depth %d is outside 1..%d", o.Depth, MaxDepth)
	}
	if o.Limit < 1 || o.Limit > MaxLimit {
		return fmt.Errorf("graph: limit %d is outside 1..%d", o.Limit, MaxLimit)
	}
	return nil
}

type builder struct {
	src     Source
	opts    Options
	nodes   map[string]*Node
	edges   map[string]Edge
	roots   []string
	queue   []string
	omitted Omitted
}

func (b *builder) addRoot(ctx context.Context, id string) error {
	if _, seen := b.nodes[id]; seen {
		return nil
	}
	if len(b.nodes) >= b.opts.Limit {
		b.omitted.NodeLimit = true
		return nil
	}
	node, err := b.src.Describe(ctx, id)
	if err != nil {
		return fmt.Errorf("graph: describing root %q: %w", id, err)
	}
	if node.ID != id {
		return fmt.Errorf("graph: the source described root %q as %q", id, node.ID)
	}
	b.add(node, 0)
	b.roots = append(b.roots, id)
	return nil
}

// add takes a node into the graph at depth. An unresolved node is a leaf, so it
// is never queued for its neighbours.
func (b *builder) add(node Node, depth int) {
	node.Depth, node.In, node.Out = depth, 0, 0
	b.nodes[node.ID] = &node
	if !node.Unresolved {
		b.queue = append(b.queue, node.ID)
	}
}

func (b *builder) expand(ctx context.Context, node *Node) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.opts.Direction != DirectionCallers && node.Depth >= 0 {
		steps, err := b.src.Out(ctx, node.ID)
		if err != nil {
			return fmt.Errorf("graph: callees of %q: %w", node.ID, err)
		}
		if node.Out, err = b.follow(node, steps, 1); err != nil {
			return err
		}
	}
	if b.opts.Direction != DirectionCallees && node.Depth <= 0 {
		steps, err := b.src.In(ctx, node.ID)
		if err != nil {
			return fmt.Errorf("graph: callers of %q: %w", node.ID, err)
		}
		if node.In, err = b.follow(node, steps, -1); err != nil {
			return err
		}
	}
	return nil
}

// follow takes node's neighbours one step in direction (1 to callees, -1 to
// callers) and returns how many edges the source reported that way.
func (b *builder) follow(node *Node, steps []Step, direction int) (int, error) {
	merged, err := b.merge(node.ID, steps, direction)
	if err != nil {
		return 0, err
	}
	atDepth := node.Depth == direction*b.opts.Depth
	beyond := false
	for _, step := range merged {
		if _, present := b.nodes[step.Node.ID]; !present {
			if atDepth {
				beyond = true
				continue
			}
			if len(b.nodes) >= b.opts.Limit {
				b.omitted.NodeLimit = true
				continue
			}
			b.add(step.Node, node.Depth+direction)
		}
		b.record(step.Edge)
	}
	if beyond {
		b.omitted.BeyondDepth++
	}
	return len(merged), nil
}

// record keeps an edge the first time it is reported. The callees of its source
// and the callers of its target both report it, each with all of its sites.
func (b *builder) record(edge Edge) {
	if _, seen := b.edges[edge.ID]; seen {
		return
	}
	b.edges[edge.ID] = edge
	if b.nodes[edge.To].Unresolved {
		b.omitted.Unresolved += max(1, len(edge.Sites))
	}
}

// merge folds the steps from id into one per neighbour and edge type, ordered by
// edge id so the graph does not depend on the order the source reports them in,
// and leaves out the edges the theme drops.
func (b *builder) merge(id string, steps []Step, direction int) ([]Step, error) {
	byEdge := map[string]*Step{}
	for _, step := range steps {
		if step.Node.ID == "" {
			return nil, fmt.Errorf("graph: the source returned a step from %q with no node id", id)
		}
		if step.Edge.Type == uir.RelationshipTypeNA {
			return nil, fmt.Errorf("graph: the source returned a step between %q and %q with no edge type", id, step.Node.ID)
		}
		edge := step.Edge
		edge.From, edge.To = id, step.Node.ID
		if direction < 0 {
			edge.From, edge.To = edge.To, edge.From
		}
		edge.ID = edge.From + "|" + edge.To + "|" + string(edge.Type)
		if existing, ok := byEdge[edge.ID]; ok {
			existing.Edge.Sites = append(existing.Edge.Sites, edge.Sites...)
			continue
		}
		edge.Sites = append([]Site{}, edge.Sites...)
		byEdge[edge.ID] = &Step{Node: step.Node, Edge: edge}
	}
	merged := make([]Step, 0, len(byEdge))
	for _, edgeID := range slices.Sorted(maps.Keys(byEdge)) {
		step := *byEdge[edgeID]
		slices.SortStableFunc(step.Edge.Sites, compareSites)
		if b.opts.Theme != nil && !b.opts.Theme.Edge(&step.Edge) {
			continue
		}
		merged = append(merged, step)
	}
	return merged, nil
}

func compareSites(a, b Site) int {
	return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column))
}

func (b *builder) graph() (*Graph, error) {
	g := &Graph{Roots: b.roots, Nodes: make([]Node, 0, len(b.nodes)), Edges: make([]Edge, 0, len(b.edges)), Omitted: b.omitted}
	drawnIn, drawnOut := map[string]int{}, map[string]int{}
	for _, edge := range b.edges {
		g.Edges = append(g.Edges, edge)
		drawnIn[edge.To]++
		drawnOut[edge.From]++
	}
	groups := map[string]bool{}
	for _, id := range slices.Sorted(maps.Keys(b.nodes)) {
		node := *b.nodes[id]
		node.In, node.Out = max(node.In, drawnIn[id]), max(node.Out, drawnOut[id])
		if b.opts.Theme != nil {
			b.opts.Theme.Node(&node)
		}
		if node.ID != id {
			return nil, fmt.Errorf("graph: the theme changed node id %q to %q", id, node.ID)
		}
		g.Nodes = append(g.Nodes, node)
		if node.Group != "" {
			groups[node.Group] = true
		}
	}
	for _, group := range slices.Sorted(maps.Keys(groups)) {
		g.Groups = append(g.Groups, Group{ID: group, Label: group})
	}
	slices.SortStableFunc(g.Nodes, func(a, b Node) int { return cmp.Compare(a.Depth, b.Depth) })
	slices.SortFunc(g.Edges, func(a, b Edge) int {
		return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.To, b.To), cmp.Compare(a.Type, b.Type))
	})
	return g, nil
}
