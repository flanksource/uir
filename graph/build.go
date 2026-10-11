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
// and callers are separate walks, so a caller's other callees are not pulled in.
// A node reached again keeps its first display depth; each direction expands it
// once, which ends cycles without losing an overlapping walk.
// Nodes at Options.Depth are still asked for their neighbours, to draw
// the edges back into the graph and to count what lies beyond.
func Build(ctx context.Context, src Source, roots []string, opts Options) (*Graph, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return nil, errors.New("graph: at least one root is required")
	}
	b := &builder{src: src, opts: opts, nodes: map[string]*Node{}, edges: map[string]Edge{}, excluded: map[string]map[string]bool{}, walked: map[walkSide]bool{}}
	for _, root := range roots {
		if err := b.addRoot(ctx, root); err != nil {
			return nil, err
		}
	}
	for len(b.queue) > 0 {
		walk := b.queue[0]
		b.queue = b.queue[1:]
		node := *b.nodes[walk.id]
		node.Depth = walk.depth
		if err := b.expandWalk(ctx, &node, walk.direction); err != nil {
			return nil, err
		}
		b.nodes[walk.id].In = max(b.nodes[walk.id].In, node.In)
		b.nodes[walk.id].Out = max(b.nodes[walk.id].Out, node.Out)
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
	return validateAccess(o.Access)
}

type builder struct {
	src     Source
	opts    Options
	nodes   map[string]*Node
	edges   map[string]Edge
	roots   []string
	queue   []graphWalk
	walked  map[walkSide]bool
	omitted Omitted
	// excluded holds the ids of the excluded nodes by group.
	excluded map[string]map[string]bool
}

type graphWalk struct {
	id               string
	depth, direction int
}
type walkSide struct {
	id        string
	direction int
}

func (b *builder) enqueue(id string, depth, direction int) {
	key := walkSide{id: id, direction: direction}
	if b.walked[key] || b.nodes[id].Unresolved {
		return
	}
	b.walked[key] = true
	b.queue = append(b.queue, graphWalk{id: id, depth: depth, direction: direction})
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
		if depth >= 0 && b.opts.Direction != DirectionCallers {
			b.enqueue(node.ID, depth, 1)
		}
		if depth <= 0 && b.opts.Direction != DirectionCallees {
			b.enqueue(node.ID, depth, -1)
		}
	}
}

func (b *builder) expandWalk(ctx context.Context, node *Node, direction int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if direction > 0 {
		steps, err := b.src.Out(ctx, node.ID)
		if err != nil {
			return fmt.Errorf("graph: callees of %q: %w", node.ID, err)
		}
		if node.Out, err = b.follow(node, steps, 1); err != nil {
			return err
		}
	}
	if direction < 0 {
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
	merged = b.withoutExcluded(merged, atDepth)
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
		if !atDepth {
			b.enqueue(step.Node.ID, node.Depth+direction, direction)
		}
		b.record(step.Edge)
	}
	if beyond {
		b.omitted.BeyondDepth++
	}
	return len(merged), nil
}

// withoutExcluded drops the steps to neighbours Options.Exclude leaves out, tallying each one unless
// only a node at the depth bound reaches it. A neighbour already in the graph is kept.
func (b *builder) withoutExcluded(steps []Step, atDepth bool) []Step {
	if b.opts.Exclude == nil {
		return steps
	}
	kept := steps[:0]
	for _, step := range steps {
		if _, present := b.nodes[step.Node.ID]; present || !b.opts.Exclude(step.Node) {
			kept = append(kept, step)
			continue
		}
		if atDepth {
			continue
		}
		if b.excluded[step.Node.Group] == nil {
			b.excluded[step.Node.Group] = map[string]bool{}
		}
		b.excluded[step.Node.Group][step.Node.ID] = true
	}
	return kept
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
// and leaves out the edges Options.Access does not follow and the theme drops.
func (b *builder) merge(id string, steps []Step, direction int) ([]Step, error) {
	byEdge := map[string]*Step{}
	for _, step := range steps {
		if step.Node.ID == "" {
			return nil, fmt.Errorf("graph: the source returned a step from %q with no node id", id)
		}
		if step.Edge.Type == uir.RelationshipTypeNA {
			return nil, fmt.Errorf("graph: the source returned a step between %q and %q with no edge type", id, step.Node.ID)
		}
		if !Follows(b.opts.Access, step.Edge.Type) {
			continue
		}
		edge := step.Edge
		edge.From, edge.To = id, step.Node.ID
		if direction < 0 {
			edge.From, edge.To = edge.To, edge.From
		}
		edge.ID = edge.From + "|" + edge.To + "|" + string(edge.Type)
		if edge.Kind != "" {
			edge.ID += "|" + edge.Kind
		}
		if existing, ok := byEdge[edge.ID]; ok {
			existing.Edge.Sites = append(existing.Edge.Sites, edge.Sites...)
			if len(existing.Edge.Properties) == 0 {
				existing.Edge.Properties = edge.Properties
			}
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
	for group, ids := range b.excluded {
		if g.Omitted.Excluded == nil {
			g.Omitted.Excluded = map[string]int{}
		}
		g.Omitted.Excluded[group] = len(ids)
	}
	slices.SortStableFunc(g.Nodes, func(a, b Node) int { return cmp.Compare(a.Depth, b.Depth) })
	slices.SortFunc(g.Edges, func(a, b Edge) int {
		return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.To, b.To), cmp.Compare(a.Type, b.Type), cmp.Compare(a.Kind, b.Kind))
	})
	return g, nil
}
