package main

import (
	"fmt"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/query"
)

func (result moduleGraphResult) Pretty() api.Text {
	return api.Text{}.Add(api.NewTree(result.tree()))
}

func (result moduleGraphResult) tree() *graph.TreeNode {
	var root *graph.TreeNode
	switch {
	case len(result.Candidates) > 0:
		root = &graph.TreeNode{Label: clicky.Text(fmt.Sprintf("%d candidates: narrow the selector or pass --symbol", len(result.Candidates)), uir.StyleBold)}
		for _, candidate := range result.Candidates {
			kind := querySymbolType(moduleQueryRow{Kind: candidate.Kind})
			root.Children = append(root.Children, &graph.TreeNode{Label: api.Text{}.Add(kind.Icon()).Space().Append(candidate.QueryName, kind.Color()).Space().Append(candidate.ID, uir.StyleMuted)})
		}
	case result.Since == nil:
		root = result.Tree(graph.TreeOptions{Name: goNodeName})
	default:
		root = result.Tree(graph.TreeOptions{Name: changedNodeName(result.Since)})
		root.Children = append(root.Children, sinceTree(result.GraphResult))
	}
	return root
}

// changedNodeName draws a node as goNodeName does, followed by why it changed.
func changedNodeName(changes *query.GraphChanges) func(graph.Node) api.Text {
	reasons := map[string]query.GraphChangeReason{}
	for _, node := range changes.Nodes {
		reasons[node.ID] = node.Reason
	}
	return func(node graph.Node) api.Text {
		name := goNodeName(node)
		if reason, changed := reasons[node.ID]; changed {
			name = name.Space().Append("changed: "+string(reason), uir.StyleWarning)
		}
		return name
	}
}

// sinceTree lists each changed node with the path to it, and each changed dependency.
func sinceTree(result query.GraphResult) *graph.TreeNode {
	changes := result.Since
	if len(changes.Nodes) == 0 && len(changes.Dependencies) == 0 {
		return &graph.TreeNode{Label: clicky.Text("unchanged since "+changes.Revision, uir.StyleMuted)}
	}
	labels := map[string]string{}
	for _, node := range result.Nodes {
		labels[node.ID] = node.Label
	}
	tree := &graph.TreeNode{Label: clicky.Text("changed since "+changes.Revision, uir.StyleWarning)}
	for _, node := range changes.Nodes {
		path := make([]string, 0, len(node.Path))
		for _, id := range node.Path {
			path = append(path, labels[id])
		}
		tree.Children = append(tree.Children, &graph.TreeNode{Label: clicky.Text(string(node.Reason), uir.StyleWarning).Space().
			Append(labels[node.ID] + ": " + strings.Join(path, " → "))})
	}
	for _, dependency := range changes.Dependencies {
		line := fmt.Sprintf("%s %s → %s", dependency.Module, dependency.From, dependency.To)
		if dependency.Excluded {
			line += ", excluded from the walk"
		}
		tree.Children = append(tree.Children, &graph.TreeNode{Label: clicky.Text("dependency", uir.StyleWarning).Space().Append(line)})
	}
	return tree
}

// goNodeName draws Go symbol icons and preserves native entity kinds.
func goNodeName(node graph.Node) api.Text {
	switch {
	case node.Unresolved:
		return clicky.Text(node.Label, uir.NodeTypeUnknown.Color())
	case node.Kind == "builtin":
		return clicky.Text(node.Label, uir.StyleKeyword)
	case strings.Contains(node.Kind, "."):
		return clicky.Text(node.Kind, uir.StyleDim).Space().Append(node.Label)
	}
	kind := querySymbolType(moduleQueryRow{Kind: node.Kind, identifier: node.Identifier})
	return api.Text{}.Add(kind.Icon()).Space().Append(node.Label, kind.Color())
}
