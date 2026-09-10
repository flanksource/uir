package render

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"

	"github.com/flanksource/uir"
)

func BuildHierarchyTree(u *uir.UIR) api.TreeNode {
	if u == nil || u.Hierarchy == nil || len(u.Hierarchy.Nodes) == 0 {
		return &groupNode{label: clicky.Text("(empty hierarchy)"), children: nil}
	}

	nodeByID := make(map[string]uir.HierarchyNode, len(u.Hierarchy.Nodes))
	incomingContainment := make(map[string]int, len(u.Hierarchy.Nodes))
	for _, node := range u.Hierarchy.Nodes {
		if node.ID == "" {
			continue
		}
		nodeByID[node.ID] = node
	}
	for _, node := range u.Hierarchy.Nodes {
		for _, edge := range node.Children {
			if !isContainmentHierarchyEdge(edge) || edge.TargetID == "" {
				continue
			}
			if _, ok := nodeByID[edge.TargetID]; !ok {
				continue
			}
			incomingContainment[edge.TargetID]++
		}
	}

	symbols := indexHierarchySymbols(u)
	roots := make([]uir.HierarchyNode, 0, len(nodeByID))
	for _, node := range nodeByID {
		if incomingContainment[node.ID] == 0 {
			roots = append(roots, node)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		return compareHierarchyNodes(roots[i], roots[j]) < 0
	})

	children := make([]api.TreeNode, 0, len(roots))
	for _, root := range roots {
		children = append(children, &hierarchyNodeTree{
			node:     root,
			nodeByID: nodeByID,
			symbols:  symbols,
		})
	}

	return &groupNode{
		label:    hierarchySummaryLabel(u.Hierarchy),
		children: children,
	}
}

type hierarchyNodeTree struct {
	node     uir.HierarchyNode
	nodeByID map[string]uir.HierarchyNode
	symbols  map[string]api.TreeNode
}

func (n *hierarchyNodeTree) Pretty() api.Text {
	label := clicky.Text("")
	switch strings.ToLower(n.node.Kind) {
	case "company", "sub_company", "product", "plan", "transaction", "segment", "business_rule", "collection", "product_group":
		label = label.Add(icons.Package).Append(" ")
	default:
		label = label.Add(icons.Type).Append(" ")
	}

	name := n.node.DisplayName
	if name == "" {
		name = n.node.Name
	}
	if name == "" {
		name = n.node.ID
	}
	label = label.Append(name, "text-green-600 font-bold")
	if n.node.Kind != "" {
		label = label.Append(" ", "").Append("("+n.node.Kind+")", "text-gray-500")
	}
	return label
}

func (n *hierarchyNodeTree) GetChildren() []api.TreeNode {
	children := make([]api.TreeNode, 0)

	for _, edge := range sortedHierarchyEdges(n.node.Children, true) {
		target, ok := n.nodeByID[edge.TargetID]
		if !ok {
			children = append(children, hierarchyLeaf(fmt.Sprintf("%s -> %s", hierarchyEdgeLabel(edge), edge.TargetID)))
			continue
		}
		children = append(children, &hierarchyNodeTree{
			node:     target,
			nodeByID: n.nodeByID,
			symbols:  n.symbols,
		})
	}

	for _, attachment := range sortedHierarchyAttachments(n.node.Attachments) {
		key := hierarchySymbolKey(attachment.SymbolRef)
		if symbol, ok := n.symbols[key]; ok {
			children = append(children, &attachedSymbolTreeNode{role: attachment.Role, symbol: symbol})
			continue
		}
		children = append(children, hierarchyLeafText(
			clicky.Text("attach ", "text-gray-500").Append(hierarchySymbolSummary(attachment.SymbolRef), "text-blue-500"),
		))
	}

	for _, edge := range sortedHierarchyEdges(n.node.Children, false) {
		children = append(children, hierarchyLeafText(
			clicky.Text("ref ", "text-gray-500").
				Append(hierarchyEdgeLabel(edge), "text-purple-600").
				Append(" -> ", "text-gray-500").
				Append(hierarchyTargetLabel(edge.TargetID, n.nodeByID), "text-blue-500"),
		))
	}

	if len(n.node.Values) > 0 {
		children = append(children, hierarchyCollapsedLeaf("values", n.node.Values))
	}
	if len(n.node.Imports) > 0 {
		children = append(children, hierarchyCollapsedLeaf("imports", n.node.Imports))
	}
	if len(n.node.Exports) > 0 {
		children = append(children, hierarchyCollapsedLeaf("exports", n.node.Exports))
	}

	return children
}

type attachedSymbolTreeNode struct {
	role   string
	symbol api.TreeNode
}

func (n *attachedSymbolTreeNode) Pretty() api.Text {
	return clicky.Text("attach ", "text-gray-500").
		Append(n.role, "text-purple-600").
		Append(" ", "").
		Add(n.symbol.Pretty())
}

func (n *attachedSymbolTreeNode) GetChildren() []api.TreeNode {
	return n.symbol.GetChildren()
}

type hierarchyLeafNode struct {
	label api.Text
}

func (n *hierarchyLeafNode) Pretty() api.Text {
	return n.label
}

func (n *hierarchyLeafNode) GetChildren() []api.TreeNode {
	return nil
}

func hierarchyLeaf(text string) api.TreeNode {
	return &hierarchyLeafNode{label: clicky.Text(text, "text-gray-600")}
}

func hierarchyLeafText(text api.Text) api.TreeNode {
	return &hierarchyLeafNode{label: text}
}

func hierarchyCollapsedLeaf(name string, value any) api.TreeNode {
	raw, err := json.Marshal(value)
	if err != nil {
		return hierarchyLeaf(name)
	}
	return &hierarchyLeafNode{
		label: clicky.Text("").Add(clicky.Collapsed(name, clicky.Text(string(raw), "text-gray-600"))),
	}
}

func hierarchySummaryLabel(graph *uir.HierarchyGraph) api.Text {
	fileSet := make(map[string]struct{})
	attachments := 0
	for _, node := range graph.Nodes {
		if node.SourceCode.Path != "" {
			fileSet[node.SourceCode.Path] = struct{}{}
		}
		attachments += len(node.Attachments)
	}
	return clicky.Text("hierarchy", "font-bold").
		Append(fmt.Sprintf(" nodes=%d", len(graph.Nodes)), "text-gray-600").
		Append(fmt.Sprintf(" attachments=%d", attachments), "text-gray-600").
		Append(fmt.Sprintf(" files=%d", len(fileSet)), "text-gray-600")
}

func sortedHierarchyEdges(edges []uir.HierarchyEdge, containment bool) []uir.HierarchyEdge {
	out := make([]uir.HierarchyEdge, 0, len(edges))
	for _, edge := range edges {
		if isContainmentHierarchyEdge(edge) != containment {
			continue
		}
		out = append(out, edge)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].TargetID < out[j].TargetID
	})
	return out
}

func sortedHierarchyAttachments(attachments []uir.HierarchyAttachment) []uir.HierarchyAttachment {
	out := append([]uir.HierarchyAttachment(nil), attachments...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role < out[j].Role
		}
		return hierarchySymbolKey(out[i].SymbolRef) < hierarchySymbolKey(out[j].SymbolRef)
	})
	return out
}

func isContainmentHierarchyEdge(edge uir.HierarchyEdge) bool {
	kind := strings.ToLower(strings.TrimSpace(edge.EdgeKind))
	return kind == "" || kind == "containment" || kind == "child"
}

func hierarchyEdgeLabel(edge uir.HierarchyEdge) string {
	if edge.Name != "" {
		return edge.Name
	}
	if edge.EdgeKind != "" {
		return edge.EdgeKind
	}
	return "child"
}

func hierarchyTargetLabel(targetID string, nodeByID map[string]uir.HierarchyNode) string {
	if node, ok := nodeByID[targetID]; ok {
		if node.DisplayName != "" {
			return node.DisplayName
		}
		if node.Name != "" {
			return node.Name
		}
	}
	return targetID
}

func compareHierarchyNodes(left, right uir.HierarchyNode) int {
	leftName := left.DisplayName
	if leftName == "" {
		leftName = left.Name
	}
	if leftName == "" {
		leftName = left.ID
	}
	rightName := right.DisplayName
	if rightName == "" {
		rightName = right.Name
	}
	if rightName == "" {
		rightName = right.ID
	}
	if leftName != rightName {
		if leftName < rightName {
			return -1
		}
		return 1
	}
	if left.Kind != right.Kind {
		if left.Kind < right.Kind {
			return -1
		}
		return 1
	}
	switch {
	case left.ID < right.ID:
		return -1
	case left.ID > right.ID:
		return 1
	default:
		return 0
	}
}

func hierarchySymbolKey(ref uir.UIRSymbolRef) string {
	return strings.Join([]string{
		strings.ToLower(ref.SymbolKind),
		ref.Module,
		ref.Package,
		ref.Type,
		ref.Method,
		ref.Name,
	}, "|")
}

func hierarchySymbolSummary(ref uir.UIRSymbolRef) string {
	parts := make([]string, 0, 4)
	if ref.Package != "" {
		parts = append(parts, ref.Package)
	}
	if ref.Type != "" {
		parts = append(parts, ref.Type)
	}
	if ref.Method != "" {
		parts = append(parts, ref.Method)
	}
	if ref.Name != "" && ref.Name != ref.Method && ref.Name != ref.Type {
		parts = append(parts, ref.Name)
	}
	if len(parts) == 0 {
		return ref.SymbolKind
	}
	return strings.Join(parts, ".")
}

func indexHierarchySymbols(u *uir.UIR) map[string]api.TreeNode {
	index := make(map[string]api.TreeNode)
	add := func(ref uir.UIRSymbolRef, node api.TreeNode) {
		key := hierarchySymbolKey(ref)
		if key == "" || key == "|||||" {
			return
		}
		index[key] = node
	}

	for i := range u.Packages {
		pkg := u.Packages[i]
		for j := range pkg.Types {
			add(uir.SymbolRefForNode(pkg.Types[j]), &typeTreeNode{typ: pkg.Types[j]})
		}
		for j := range pkg.Functions {
			add(uir.SymbolRefForNode(pkg.Functions[j]), &methodTreeNode{m: pkg.Functions[j]})
		}
		for j := range pkg.Records {
			add(uir.SymbolRefForNode(pkg.Records[j]), &recordTreeNode{rec: pkg.Records[j]})
		}
		for j := range pkg.Tables {
			add(uir.SymbolRefForNode(pkg.Tables[j]), &tableTreeNode{tbl: pkg.Tables[j]})
		}
		for j := range pkg.Endpoints {
			add(uir.SymbolRefForNode(pkg.Endpoints[j]), &endpointTreeNode{ep: pkg.Endpoints[j]})
		}
	}
	for i := range u.Types {
		add(uir.SymbolRefForNode(u.Types[i]), &typeTreeNode{typ: u.Types[i]})
	}
	for i := range u.Functions {
		add(uir.SymbolRefForNode(u.Functions[i]), &methodTreeNode{m: u.Functions[i]})
	}
	for i := range u.Records {
		add(uir.SymbolRefForNode(u.Records[i]), &recordTreeNode{rec: u.Records[i]})
	}
	for i := range u.Tables {
		add(uir.SymbolRefForNode(u.Tables[i]), &tableTreeNode{tbl: u.Tables[i]})
	}
	for i := range u.Endpoints {
		add(uir.SymbolRefForNode(u.Endpoints[i]), &endpointTreeNode{ep: u.Endpoints[i]})
	}
	return index
}
