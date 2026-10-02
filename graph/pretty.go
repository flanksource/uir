package graph

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/clicky/api"

	"github.com/flanksource/uir"
)

// TreeOptions shape the text tree of a graph.
type TreeOptions struct {
	// Name draws a node's name. Nil draws the label, after the icon and in the
	// colour of its Kind when Kind is a uir.NodeType.
	Name func(Node) api.Text
}

// TreeNode is one line of a drawn graph and the lines under it.
type TreeNode struct {
	Label    api.Text
	Children []*TreeNode
}

func (node *TreeNode) Pretty() api.Text { return node.Label }

func (node *TreeNode) GetChildren() []api.TreeNode {
	children := make([]api.TreeNode, len(node.Children))
	for index, child := range node.Children {
		children[index] = child
	}
	return children
}

// Pretty draws the graph as Tree does with no options.
func (g Graph) Pretty() api.Text {
	return api.Text{}.Add(api.NewTree(g.Tree(TreeOptions{})))
}

// Tree draws the graph as the two trees that hang off its first root: the nodes
// that call it and the nodes it calls, then what the graph left out. A graph is
// not a tree, so a node is expanded once, one call further from the root than
// its parent, and marked wherever else an edge reaches it.
func (g Graph) Tree(opts TreeOptions) *TreeNode {
	if len(g.Roots) == 0 {
		return &TreeNode{Label: api.Text{Content: "no call graph", Style: uir.StyleBold}}
	}
	if opts.Name == nil {
		opts.Name = nodeName
	}
	root := newGraphTree(g, opts).root(g.Roots[0])
	if omitted := omittedSummary(g.Omitted); omitted != "" {
		root.Children = append(root.Children, &TreeNode{Label: api.Text{Content: "omitted: " + omitted, Style: uir.StyleWarning}})
	}
	if len(g.Omitted.Excluded) > 0 {
		root.Children = append(root.Children, &TreeNode{Label: api.Text{Content: "excluded: " + excludedSummary(g.Omitted.Excluded), Style: uir.StyleMuted}})
	}
	return root
}

func nodeName(node Node) api.Text {
	if node.Unresolved {
		return api.Text{Content: node.Label, Style: uir.NodeTypeUnknown.Color()}
	}
	kind := uir.NodeType(node.Kind)
	if icon := kind.Icon(); icon.String() != "" {
		return api.Text{}.Add(icon).Space().Append(node.Label, kind.Color())
	}
	return api.Text{Content: node.Label, Style: kind.Color()}
}

func omittedSummary(omitted Omitted) string {
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

// excludedSummary tallies the excluded nodes per group, most first.
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

type graphTree struct {
	name     func(Node) api.Text
	nodes    map[string]Node
	out, in  map[string][]Edge
	expanded map[string]bool
}

func newGraphTree(g Graph, opts TreeOptions) *graphTree {
	tree := &graphTree{name: opts.Name, nodes: make(map[string]Node, len(g.Nodes)), out: map[string][]Edge{}, in: map[string][]Edge{}}
	for _, node := range g.Nodes {
		tree.nodes[node.ID] = node
	}
	for _, edge := range g.Edges {
		tree.out[edge.From] = append(tree.out[edge.From], edge)
		tree.in[edge.To] = append(tree.in[edge.To], edge)
	}
	return tree
}

func (tree *graphTree) root(id string) *TreeNode {
	node := tree.nodes[id]
	label := tree.name(node)
	if node.Group != "" {
		label = label.Space().Append(node.Group, uir.StyleMuted)
	}
	if node.Location != nil {
		label = label.Space().Append(fmt.Sprintf("%s:%d", node.Location.Path, node.Location.Line), uir.StyleDim)
	}
	root := &TreeNode{Label: label}
	for _, side := range []struct {
		heading string
		edges   map[string][]Edge
		step    int
	}{{"callers", tree.in, -1}, {"callees", tree.out, 1}} {
		tree.expanded = map[string]bool{id: true}
		if children := tree.children(node, side.edges, side.step); len(children) > 0 {
			root.Children = append(root.Children, &TreeNode{Label: api.Text{Content: side.heading, Style: uir.StyleBold}, Children: children})
		}
	}
	return root
}

// children are the neighbours of parent over edges, in the order of their first call site.
func (tree *graphTree) children(parent Node, edges map[string][]Edge, step int) []*TreeNode {
	drawn := slices.Clone(edges[parent.ID])
	slices.SortStableFunc(drawn, func(left, right Edge) int {
		return cmp.Or(compareFirstSites(left, right), cmp.Compare(left.Type, right.Type))
	})
	children := make([]*TreeNode, 0, len(drawn))
	for _, edge := range drawn {
		neighbour := tree.nodes[edge.To]
		if step < 0 {
			neighbour = tree.nodes[edge.From]
		}
		child := &TreeNode{Label: tree.edgeLabel(parent, neighbour, edge)}
		switch {
		case !tree.expanded[neighbour.ID] && neighbour.Depth == parent.Depth+step:
			tree.expanded[neighbour.ID] = true
			child.Children = tree.children(neighbour, edges, step)
		case len(edges[neighbour.ID]) > 0:
			child.Label = child.Label.Space().Append("↩ expanded elsewhere", uir.StyleFaint)
		}
		children = append(children, child)
	}
	return children
}

// compareFirstSites orders edges by their first call site, an edge without sites last.
func compareFirstSites(left, right Edge) int {
	if len(left.Sites) == 0 || len(right.Sites) == 0 {
		return cmp.Compare(len(right.Sites), len(left.Sites))
	}
	return compareSites(left.Sites[0], right.Sites[0])
}

// edgeLabel is one line of the tree: the neighbour, how many call sites the edge has when more than
// one, and the guards and position of the first.
func (tree *graphTree) edgeLabel(parent, neighbour Node, edge Edge) api.Text {
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
