package main

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/query"
)

func (result moduleGraphResult) Pretty() api.Text {
	return api.Text{}.Add(api.NewTree(result.tree()))
}

func (result moduleGraphResult) tree() *moduleQueryTreeNode {
	var root *moduleQueryTreeNode
	switch {
	case len(result.Candidates) > 0:
		root = &moduleQueryTreeNode{label: clicky.Text(fmt.Sprintf("%d candidates: narrow the selector or pass --symbol", len(result.Candidates)), uir.StyleBold)}
		for _, candidate := range result.Candidates {
			kind := querySymbolType(moduleQueryRow{Kind: candidate.Kind})
			root.children = append(root.children, &moduleQueryTreeNode{label: api.Text{}.Add(kind.Icon()).Space().Append(candidate.QueryName, kind.Color()).Space().Append(candidate.ID, uir.StyleMuted)})
		}
	case len(result.Roots) == 0:
		root = &moduleQueryTreeNode{label: clicky.Text("no call graph", uir.StyleBold)}
	default:
		root = newGraphTree(result.Graph).root(result.Roots[0])
		if omitted := omittedSummary(result.Omitted); omitted != "" {
			root.children = append(root.children, &moduleQueryTreeNode{label: clicky.Text("omitted: "+omitted, uir.StyleWarning)})
		}
		if len(result.Omitted.Excluded) > 0 {
			root.children = append(root.children, &moduleQueryTreeNode{label: clicky.Text("excluded: "+excludedSummary(result.Omitted.Excluded), uir.StyleMuted)})
		}
	}
	for _, warning := range result.Warnings {
		root.children = append(root.children, missingHeadNode(warning))
	}
	return root
}

func missingHeadNode(warning query.MissingHeadWarning) *moduleQueryTreeNode {
	return &moduleQueryTreeNode{label: api.Text{}.Add(icons.Warning).Space().Append(warning.RootKey, uir.StyleWarning).Space().Append(warning.Message, uir.StyleMuted)}
}

func omittedSummary(omitted graph.Omitted) string {
	var parts []string
	if omitted.NodeLimit {
		parts = append(parts, "node limit reached")
	}
	if omitted.BeyondDepth > 0 {
		parts = append(parts, fmt.Sprintf("%d beyond depth", omitted.BeyondDepth))
	}
	if omitted.Unresolved > 0 {
		parts = append(parts, fmt.Sprintf("%d unresolved", omitted.Unresolved))
	}
	if len(omitted.UnreadableSource) > 0 {
		parts = append(parts, "guards unreadable in "+strings.Join(omitted.UnreadableSource, ", "))
	}
	return strings.Join(parts, "; ")
}

// excludedSummary tallies the excluded nodes per package, most first.
func excludedSummary(excluded map[string]int) string {
	groups := slices.SortedFunc(maps.Keys(excluded), func(left, right string) int {
		return cmp.Or(cmp.Compare(excluded[right], excluded[left]), cmp.Compare(left, right))
	})
	tallies := make([]string, 0, len(groups))
	for _, group := range groups {
		tallies = append(tallies, fmt.Sprintf("%s %d", group, excluded[group]))
	}
	return strings.Join(tallies, ", ")
}

// graphTree draws a graph as the two trees that hang off its root: the nodes that call it, and the
// nodes it calls. A graph is not a tree, so a node is expanded once, one call further from the root
// than its parent, and marked wherever else an edge reaches it.
type graphTree struct {
	nodes    map[string]graph.Node
	out, in  map[string][]graph.Edge
	expanded map[string]bool
}

func newGraphTree(drawn graph.Graph) *graphTree {
	tree := &graphTree{nodes: make(map[string]graph.Node, len(drawn.Nodes)), out: map[string][]graph.Edge{}, in: map[string][]graph.Edge{}}
	for _, node := range drawn.Nodes {
		tree.nodes[node.ID] = node
	}
	for _, edge := range drawn.Edges {
		tree.out[edge.From] = append(tree.out[edge.From], edge)
		tree.in[edge.To] = append(tree.in[edge.To], edge)
	}
	return tree
}

func (tree *graphTree) root(id string) *moduleQueryTreeNode {
	node := tree.nodes[id]
	label := tree.name(node)
	if node.Group != "" {
		label = label.Space().Append(node.Group, uir.StyleMuted)
	}
	if node.Location != nil {
		label = label.Space().Append(fmt.Sprintf("%s:%d", node.Location.Path, node.Location.Line), uir.StyleDim)
	}
	root := &moduleQueryTreeNode{label: label}
	for _, side := range []struct {
		heading string
		edges   map[string][]graph.Edge
		step    int
	}{{"callers", tree.in, -1}, {"callees", tree.out, 1}} {
		tree.expanded = map[string]bool{id: true}
		if children := tree.children(node, side.edges, side.step); len(children) > 0 {
			root.children = append(root.children, &moduleQueryTreeNode{label: clicky.Text(side.heading, uir.StyleBold), children: children})
		}
	}
	return root
}

// children are the neighbours of parent over edges, in the order of their first call site.
func (tree *graphTree) children(parent graph.Node, edges map[string][]graph.Edge, step int) []*moduleQueryTreeNode {
	drawn := slices.Clone(edges[parent.ID])
	slices.SortStableFunc(drawn, func(left, right graph.Edge) int {
		return cmp.Or(compareSites(left, right), cmp.Compare(left.Type, right.Type))
	})
	children := make([]*moduleQueryTreeNode, 0, len(drawn))
	for _, edge := range drawn {
		neighbour := tree.nodes[edge.To]
		if step < 0 {
			neighbour = tree.nodes[edge.From]
		}
		child := &moduleQueryTreeNode{label: tree.edgeLabel(parent, neighbour, edge)}
		switch {
		case !tree.expanded[neighbour.ID] && neighbour.Depth == parent.Depth+step:
			tree.expanded[neighbour.ID] = true
			child.children = tree.children(neighbour, edges, step)
		case len(edges[neighbour.ID]) > 0:
			child.label = child.label.Space().Append("↩ expanded elsewhere", uir.StyleFaint)
		}
		children = append(children, child)
	}
	return children
}

func compareSites(left, right graph.Edge) int {
	if len(left.Sites) == 0 || len(right.Sites) == 0 {
		return cmp.Compare(len(right.Sites), len(left.Sites))
	}
	first, second := left.Sites[0], right.Sites[0]
	return cmp.Or(cmp.Compare(first.Path, second.Path), cmp.Compare(first.Line, second.Line), cmp.Compare(first.Column, second.Column))
}

// edgeLabel is one line of the tree: the neighbour, how many call sites the edge has when more than
// one, and the guards and position of the first.
func (tree *graphTree) edgeLabel(parent, neighbour graph.Node, edge graph.Edge) api.Text {
	label := api.Text{}
	if edge.Type == uir.RelationshipTypeDispatch {
		label = label.Add(edge.Type.Pretty()).Space()
	}
	label = label.Add(tree.name(neighbour))
	if neighbour.Group != "" && neighbour.Group != parent.Group {
		label = label.Space().Append(neighbour.Group, uir.StyleMuted)
	}
	if neighbour.Unresolved {
		label = label.Space().Append("unresolved", uir.StyleWarning)
	}
	if len(edge.Sites) > 1 {
		label = label.Space().Append(fmt.Sprintf("×%d", len(edge.Sites)), uir.StyleDim)
	}
	if len(edge.Sites) == 0 {
		return label
	}
	site := edge.Sites[0]
	for _, guard := range site.Guards {
		label = label.Space().Append("["+guard+"]", uir.StyleSecondary)
	}
	if site.Path != "" {
		label = label.Space().Append(fmt.Sprintf("%s:%d", site.Path, site.Line), uir.StyleDim)
	}
	return label
}

func (tree *graphTree) name(node graph.Node) api.Text {
	switch {
	case node.Unresolved:
		return clicky.Text(node.Label, uir.NodeTypeUnknown.Color())
	case node.Kind == "builtin":
		return clicky.Text(node.Label, uir.StyleKeyword)
	}
	kind := querySymbolType(moduleQueryRow{Kind: node.Kind, identifier: node.Identifier})
	return api.Text{}.Add(kind.Icon()).Space().Append(node.Label, kind.Color())
}
