package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
	"github.com/flanksource/uir"
)

type moduleQueryDisplay struct {
	result moduleQueryResult
	rows   any
}

func (result moduleQueryResult) Pretty() api.Text {
	return api.Text{}.Add(moduleQueryDisplay{result: result}.textTree())
}

func parseQueryGroupBy(value string) ([]string, error) {
	if value == "" {
		return []string{"module", "package", "file"}, nil
	}
	groups := strings.Split(value, ",")
	seen := make(map[string]bool, len(groups))
	for _, group := range groups {
		if group != "module" && group != "package" && group != "file" {
			return nil, fmt.Errorf("invalid --group-by level %q: expected module, package, or file", group)
		}
		if seen[group] {
			return nil, fmt.Errorf("duplicate --group-by level %q", group)
		}
		seen[group] = true
	}
	return groups, nil
}

func (display moduleQueryDisplay) Tree() api.TreeNode {
	count := fmt.Sprintf("%d matches", display.result.Total)
	if display.result.Path == nil && len(display.result.Matches) < display.result.Total {
		count = fmt.Sprintf("%d of %d matches", len(display.result.Matches), display.result.Total)
	}
	root := &moduleQueryTreeNode{label: clicky.Text(count, "font-bold")}
	if display.result.Path != nil {
		appendCallPath(root, display.result.Path)
	} else {
		for _, row := range display.result.Matches {
			appendQueryMatch(root, display.result.groupBy, row)
		}
	}
	for _, coverage := range display.result.Coverage {
		root.children = append(root.children, &moduleQueryTreeNode{label: api.Text{}.Add(icons.Warning).Space().Append(coverage.PackagePath, "text-amber-500").Space().Append(coverage.Coverage, "muted")})
	}
	for _, warning := range display.result.Warnings {
		root.children = append(root.children, &moduleQueryTreeNode{label: api.Text{}.Add(icons.Warning).Space().Append(warning.RootKey, "text-amber-500").Space().Append(warning.Message, "muted")})
	}
	return root
}

func (display moduleQueryDisplay) MarshalJSON() ([]byte, error) {
	return json.Marshal(display.rows)
}

func (display moduleQueryDisplay) MarshalYAML() (any, error) {
	return display.rows, nil
}

func (display moduleQueryDisplay) String() string {
	return display.textTree().String()
}

func (display moduleQueryDisplay) ANSI() string {
	return display.textTree().ANSI()
}

func (display moduleQueryDisplay) HTML() string {
	return display.textTree().HTML()
}

func (display moduleQueryDisplay) Markdown() string {
	return display.textTree().Markdown()
}

func (display moduleQueryDisplay) textTree() api.TextTree {
	return api.NewTree(display.Tree())
}

type moduleQueryTreeNode struct {
	label    api.Text
	key      string
	children []*moduleQueryTreeNode
}

func (node *moduleQueryTreeNode) Pretty() api.Text { return node.label }

func (node *moduleQueryTreeNode) GetChildren() []api.TreeNode {
	children := make([]api.TreeNode, len(node.children))
	for index, child := range node.children {
		children[index] = child
	}
	return children
}

func (node *moduleQueryTreeNode) addChild(key string, label api.Textable) *moduleQueryTreeNode {
	for _, child := range node.children {
		if child.key == key {
			return child
		}
	}
	child := &moduleQueryTreeNode{key: key, label: api.Text{}.Add(label)}
	node.children = append(node.children, child)
	return child
}

func appendQueryMatch(root *moduleQueryTreeNode, groups []string, row moduleQueryRow) {
	if row.Kind == "module" || row.Kind == "package" {
		parent := root
		for _, group := range groups {
			if group == "file" || row.Kind == "module" && group == "package" {
				continue
			}
			key, label := queryGroup(row, group)
			parent = parent.addChild(group+":"+key, label)
		}
		if parent == root {
			parent.addChild(row.Kind+":"+row.Symbol, api.Text{}.Add(querySymbolType(row).Icon()).Space().Append(row.Source, querySymbolType(row).Color()))
		}
		return
	}
	parent := root
	for index, group := range groups {
		if index > 0 && group == "package" && groups[index-1] == "module" && row.PackagePath == row.Root {
			continue
		}
		key, label := queryGroup(row, group)
		if index > 0 {
			switch {
			case group == "package" && groups[index-1] == "module":
				name := strings.TrimPrefix(row.PackagePath, row.Root+"/")
				label = api.Text{}.Add(uir.NodeTypePackage.Icon()).Space().Append(name, uir.NodeTypePackage.Color())
			case group == "file" && groups[index-1] == "package":
				packagePath := strings.TrimPrefix(row.PackagePath, row.Root+"/")
				name := strings.TrimPrefix(row.Path, packagePath+"/")
				label = api.Text{}.Add(icons.Filename(row.Path)).Space().Append(name, "text-blue-500")
			}
		}
		if index == 0 && group != "module" {
			label = api.Text{}.Add(label).Space().Append(row.Root, "muted").Space().Append(row.Location, "muted").Space().Append(row.SnapshotID, "muted")
		}
		parent = parent.addChild(group+":"+key, label)
	}
	name := querySymbolName(row)
	label := api.Text{}.Add(querySymbolType(row).Icon()).Space()
	if row.declaration != "" {
		if row.definitionLine != nil {
			label = label.Append(*row.definitionLine, "text-muted").Space()
		}
		label = label.Add(api.CodeBlock("go", row.declaration))
	} else {
		label = label.Append(name, querySymbolType(row).Color())
	}
	symbol := parent.addChild("symbol:"+row.Symbol, label)
	if row.Role == "definition" && row.declaration != "" && row.Line != nil && row.definitionLine != nil && *row.Line == *row.definitionLine {
		return
	}
	label = api.Text{}
	if row.Line != nil {
		label = label.Append(*row.Line, "text-muted").Space()
	}
	if row.usage != "" {
		label = label.Add(api.CodeBlock("go", row.usage))
	} else {
		label = label.Append(row.Source, "text-muted")
	}
	symbol.children = append(symbol.children, &moduleQueryTreeNode{label: label})
}

func queryGroup(row moduleQueryRow, group string) (string, api.Textable) {
	switch group {
	case "module":
		key := row.Root + "\x00" + row.Location + "\x00" + row.SnapshotID
		return key, api.Text{}.Add(uir.NodeTypeModule.Icon()).Space().Append(row.Root, uir.NodeTypeModule.Color()).Space().Append(row.Location, "muted").Space().Append(row.SnapshotID, "muted")
	case "package":
		key := row.Root + "\x00" + row.Location + "\x00" + row.SnapshotID + "\x00" + row.PackagePath
		return key, api.Text{}.Add(uir.NodeTypePackage.Icon()).Space().Append(row.PackagePath, uir.NodeTypePackage.Color())
	case "file":
		key := row.Root + "\x00" + row.Location + "\x00" + row.SnapshotID + "\x00" + row.Path
		return key, api.Text{}.Add(icons.Filename(row.Path)).Space().Append(row.Path, "text-blue-500")
	default:
		panic("unexpected query group: " + group)
	}
}

func querySymbolType(row moduleQueryRow) uir.NodeType {
	switch row.Kind {
	case "method":
		return uir.NodeTypeMethod
	case "func", "function":
		return uir.NodeTypeFunction
	case "type":
		return uir.NodeTypeType
	case "field":
		return uir.NodeTypeField
	case "var", "const":
		return uir.NodeTypePackageVariable
	case "package":
		return uir.NodeTypePackage
	case "module":
		return uir.NodeTypeModule
	default:
		return row.identifier.GetNodeType()
	}
}

func querySymbolName(row moduleQueryRow) string {
	if row.identifier.GetName() != "" {
		name := row.identifier.GetName()
		if row.identifier.Type != "" && row.identifier.Method != "" {
			name = row.identifier.Type + "." + name
		}
		return name + row.identifier.Signature
	}
	name := row.Symbol
	if index := strings.LastIndex(name, ":"); index >= 0 {
		name = name[index+1:]
	}
	return name
}

func appendCallPath(root *moduleQueryTreeNode, path *moduleCallPath) {
	parent := root
	for index, symbol := range path.Symbols {
		nodeType := querySymbolType(moduleQueryRow{Kind: symbol.Kind})
		label := api.Text{}.Add(nodeType.Icon()).Space().Append(symbol.QueryName, nodeType.Color())
		if index > 0 && index-1 < len(path.Calls) {
			label = label.Space().Append(path.Calls[index-1].Source, "muted")
		}
		child := &moduleQueryTreeNode{label: label}
		parent.children = append(parent.children, child)
		parent = child
	}
}
