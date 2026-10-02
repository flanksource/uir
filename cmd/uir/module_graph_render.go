package main

import (
	"fmt"

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

func (result moduleGraphResult) tree() *graph.TreeNode {
	var root *graph.TreeNode
	if len(result.Candidates) > 0 {
		root = &graph.TreeNode{Label: clicky.Text(fmt.Sprintf("%d candidates: narrow the selector or pass --symbol", len(result.Candidates)), uir.StyleBold)}
		for _, candidate := range result.Candidates {
			kind := querySymbolType(moduleQueryRow{Kind: candidate.Kind})
			root.Children = append(root.Children, &graph.TreeNode{Label: api.Text{}.Add(kind.Icon()).Space().Append(candidate.QueryName, kind.Color()).Space().Append(candidate.ID, uir.StyleMuted)})
		}
	} else {
		root = result.Tree(graph.TreeOptions{Name: goNodeName})
	}
	for _, warning := range result.Warnings {
		root.Children = append(root.Children, &graph.TreeNode{Label: missingHeadLabel(warning)})
	}
	return root
}

func missingHeadLabel(warning query.MissingHeadWarning) api.Text {
	return api.Text{}.Add(icons.Warning).Space().Append(warning.RootKey, uir.StyleWarning).Space().Append(warning.Message, uir.StyleMuted)
}

// goNodeName draws a node with the icon and colour of its Go symbol kind.
func goNodeName(node graph.Node) api.Text {
	switch {
	case node.Unresolved:
		return clicky.Text(node.Label, uir.NodeTypeUnknown.Color())
	case node.Kind == "builtin":
		return clicky.Text(node.Label, uir.StyleKeyword)
	}
	kind := querySymbolType(moduleQueryRow{Kind: node.Kind, identifier: node.Identifier})
	return api.Text{}.Add(kind.Icon()).Space().Append(node.Label, kind.Color())
}
