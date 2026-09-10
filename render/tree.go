// Package render adapts UIR documents to clicky's api.TreeNode so they can be
// printed as grouped trees.
package render

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"

	"github.com/flanksource/uir"
)

// ParseGroupBy splits a comma-separated group-by string into levels.
func ParseGroupBy(s string) []string {
	if s == "" {
		return []string{"git_root", "folder", "file"}
	}
	var levels []string
	for _, l := range strings.Split(s, ",") {
		l = strings.TrimSpace(l)
		if l != "" {
			levels = append(levels, l)
		}
	}
	return levels
}

// treeItem is a flattened node with metadata for grouping.
type treeItem struct {
	node     api.TreeNode
	filePath string
	pkg      string
	language string
}

// BuildTree creates a hierarchical tree from a uir.UIR using the given grouping levels.
func BuildTree(u *uir.UIR, groupBy []string) api.TreeNode {
	if u == nil {
		return &groupNode{label: clicky.Text("(empty)"), children: nil}
	}

	items := flattenUIR(u)

	children := buildGroupLevel(items, groupBy, "")
	root := &groupNode{
		label:    u.Pretty(),
		children: children,
	}
	return root
}

// flattenUIR collects all top-level declarations as treeItems.
func flattenUIR(u *uir.UIR) []treeItem {
	var items []treeItem

	for i := range u.Packages {
		pkg := u.Packages[i]
		// Package-level types
		for j := range pkg.Types {
			items = append(items, treeItem{
				node:     &typeTreeNode{typ: pkg.Types[j]},
				filePath: pkg.Types[j].GetLocation().Path,
				pkg:      pkg.Package,
				language: pkg.GetLanguage(),
			})
		}
		// Package-level functions
		for j := range pkg.Functions {
			items = append(items, treeItem{
				node:     &methodTreeNode{m: pkg.Functions[j]},
				filePath: pkg.Functions[j].GetLocation().Path,
				pkg:      pkg.Package,
				language: pkg.GetLanguage(),
			})
		}
		// Package-level records
		for j := range pkg.Records {
			items = append(items, treeItem{
				node:     &recordTreeNode{rec: pkg.Records[j]},
				filePath: pkg.Records[j].GetLocation().Path,
				pkg:      pkg.Package,
				language: pkg.GetLanguage(),
			})
		}
		// Package-level tables
		for j := range pkg.Tables {
			items = append(items, treeItem{
				node:     &tableTreeNode{tbl: pkg.Tables[j]},
				filePath: pkg.Tables[j].GetLocation().Path,
				pkg:      pkg.Package,
				language: pkg.GetLanguage(),
			})
		}
		// Package-level endpoints
		for j := range pkg.Endpoints {
			items = append(items, treeItem{
				node:     &endpointTreeNode{ep: pkg.Endpoints[j]},
				filePath: pkg.Endpoints[j].GetLocation().Path,
				pkg:      pkg.Package,
				language: pkg.GetLanguage(),
			})
		}
		// Package-level variables
		for j := range pkg.Variables {
			items = append(items, treeItem{
				node:     &fieldTreeNode{f: pkg.Variables[j]},
				filePath: pkg.Variables[j].GetLocation().Path,
				pkg:      pkg.Package,
				language: pkg.GetLanguage(),
			})
		}
	}

	// Top-level items not in any package
	for i := range u.Types {
		items = append(items, treeItem{
			node:     &typeTreeNode{typ: u.Types[i]},
			filePath: u.Types[i].GetLocation().Path,
		})
	}
	for i := range u.Functions {
		items = append(items, treeItem{
			node:     &methodTreeNode{m: u.Functions[i]},
			filePath: u.Functions[i].GetLocation().Path,
		})
	}
	for i := range u.Records {
		items = append(items, treeItem{
			node:     &recordTreeNode{rec: u.Records[i]},
			filePath: u.Records[i].GetLocation().Path,
		})
	}
	for i := range u.Tables {
		items = append(items, treeItem{
			node:     &tableTreeNode{tbl: u.Tables[i]},
			filePath: u.Tables[i].GetLocation().Path,
		})
	}
	for i := range u.Endpoints {
		items = append(items, treeItem{
			node:     &endpointTreeNode{ep: u.Endpoints[i]},
			filePath: u.Endpoints[i].GetLocation().Path,
		})
	}

	return items
}

// buildGroupLevel recursively groups items by the first level, then recurses.
func buildGroupLevel(items []treeItem, levels []string, gitRoot string) []api.TreeNode {
	if len(items) == 0 {
		return nil
	}

	if len(levels) == 0 {
		// Leaf level: return items directly
		var nodes []api.TreeNode
		for _, item := range items {
			nodes = append(nodes, item.node)
		}
		return nodes
	}

	level := levels[0]
	remaining := levels[1:]

	// Determine git root once for path-based groupings
	if gitRoot == "" && (level == "git_root" || level == "folder") {
		for _, item := range items {
			if item.filePath != "" {
				gitRoot = findGitRootCached(filepath.Dir(item.filePath))
				break
			}
		}
	}

	// Group items by key
	groups := make(map[string][]treeItem)
	var order []string

	for _, item := range items {
		key := groupKey(item, level, gitRoot)
		if _, exists := groups[key]; !exists {
			order = append(order, key)
		}
		groups[key] = append(groups[key], item)
	}

	sort.Strings(order)

	// Skip this level if all items have the same key
	if len(order) == 1 && level != "git_root" {
		return buildGroupLevel(items, remaining, gitRoot)
	}

	var nodes []api.TreeNode
	for _, key := range order {
		children := buildGroupLevel(groups[key], remaining, gitRoot)
		label := groupLabel(key, level)
		nodes = append(nodes, &groupNode{label: label, children: children})
	}
	return nodes
}

func groupKey(item treeItem, level string, gitRoot string) string {
	switch level {
	case "git_root":
		if gitRoot != "" {
			return filepath.Base(gitRoot)
		}
		return "(unknown)"
	case "folder":
		if item.filePath == "" {
			return "(unknown)"
		}
		rel := item.filePath
		if gitRoot != "" {
			if r, err := filepath.Rel(gitRoot, filepath.Dir(item.filePath)); err == nil {
				rel = r
			}
		} else {
			rel = filepath.Dir(item.filePath)
		}
		if rel == "." {
			return "."
		}
		return rel
	case "file":
		if item.filePath == "" {
			return "(unknown)"
		}
		return filepath.Base(item.filePath)
	case "package":
		if item.pkg == "" {
			return "(default)"
		}
		return item.pkg
	case "language":
		if item.language == "" {
			return "(unknown)"
		}
		return item.language
	default:
		return "(unknown)"
	}
}

func groupLabel(key string, level string) api.Text {
	switch level {
	case "git_root":
		return clicky.Text(key, "font-bold")
	case "folder":
		return clicky.Text(key+"/", "text-gray-600")
	case "file":
		return clicky.Text(key, "text-gray-700")
	case "package":
		return clicky.Text("package ", "text-blue-500").Append(key, "text-green-600 font-bold")
	case "language":
		return clicky.Text(key, "text-purple-600 font-bold")
	default:
		return clicky.Text(key)
	}
}

// groupNode is a generic tree node for grouping levels.
type groupNode struct {
	label    api.Text
	children []api.TreeNode
}

func (n *groupNode) Pretty() api.Text            { return n.label }
func (n *groupNode) GetChildren() []api.TreeNode { return n.children }

// Git root caching
var gitRootCache = make(map[string]string)

func findGitRootCached(dir string) string {
	if dir == "" {
		return ""
	}
	if cached, ok := gitRootCache[dir]; ok {
		return cached
	}
	root := findGitRoot(dir)
	gitRootCache[dir] = root
	return root
}

func findGitRoot(dir string) string {
	for d := dir; d != "/" && d != "."; d = filepath.Dir(d) {
		if info, err := filepath.Glob(filepath.Join(d, ".git")); err == nil && len(info) > 0 {
			return d
		}
	}
	return dir
}

// Tree renders a UIR as a TreeNode using the default grouping.
func Tree(u *uir.UIR) api.TreeNode {
	return BuildTree(u, ParseGroupBy(""))
}

// Within-file node types

type typeTreeNode struct{ typ uir.TypedNode }

func (n *typeTreeNode) Pretty() api.Text {
	return clicky.Text("class ", "text-blue-500").Append(n.typ.Type, "text-green-600 font-bold")
}

func (n *typeTreeNode) GetChildren() []api.TreeNode {
	var children []api.TreeNode
	for i := range n.typ.Variables {
		children = append(children, &fieldTreeNode{f: n.typ.Variables[i]})
	}
	for i := range n.typ.Methods {
		children = append(children, &methodTreeNode{m: n.typ.Methods[i]})
	}
	for i := range n.typ.Types {
		children = append(children, &typeTreeNode{typ: n.typ.Types[i]})
	}
	return children
}

type methodTreeNode struct{ m uir.MethodNode }

func (n *methodTreeNode) Pretty() api.Text {
	t := clicky.Text(n.m.Method, "text-green-600").Add(n.m.Params.Pretty())
	if len(n.m.Returns) > 0 {
		t = t.Append(" : ", "text-gray-600").Add(n.m.Returns.Pretty())
	}
	if n.m.Body != nil && len(n.m.Body.Children) > 0 {
		t = t.Add(clicky.Collapsed("body", n.m.Body.Pretty()))
	}
	return t
}

func (n *methodTreeNode) GetChildren() []api.TreeNode { return nil }

type endpointTreeNode struct{ ep uir.ASTEndpoint }

func (n *endpointTreeNode) Pretty() api.Text            { return n.ep.Pretty() }
func (n *endpointTreeNode) GetChildren() []api.TreeNode { return nil }

type recordTreeNode struct{ rec uir.ASTRecord }

func (n *recordTreeNode) Pretty() api.Text {
	return clicky.Text(string(n.rec.RecordType)+" ", "text-blue-500").Append(n.rec.Type, "text-green-600")
}

func (n *recordTreeNode) GetChildren() []api.TreeNode {
	var children []api.TreeNode
	for i := range n.rec.Fields {
		children = append(children, &fieldTreeNode{f: n.rec.Fields[i]})
	}
	return children
}

// tableTreeNode renders a table as its columns, then its indexes, then its
// foreign keys — the order someone reads a schema in.
type tableTreeNode struct{ tbl uir.RecordTable }

func (n *tableTreeNode) Pretty() api.Text {
	return clicky.Text(string(n.tbl.RecordType)+" ", "text-blue-500").
		Append(n.tbl.QualifiedName(), "text-green-600")
}

func (n *tableTreeNode) GetChildren() []api.TreeNode {
	var children []api.TreeNode
	for i := range n.tbl.Columns {
		children = append(children, &columnTreeNode{c: n.tbl.Columns[i]})
	}
	for i := range n.tbl.Indexes {
		children = append(children, &indexTreeNode{idx: n.tbl.Indexes[i]})
	}
	for i := range n.tbl.ForeignKeys {
		children = append(children, &foreignKeyTreeNode{fk: n.tbl.ForeignKeys[i]})
	}
	for i := range n.tbl.Extends {
		children = append(children, &recordTreeNode{rec: n.tbl.Extends[i]})
	}
	return children
}

type columnTreeNode struct{ c uir.RecordColumn }

func (n *columnTreeNode) Pretty() api.Text            { return n.c.Pretty() }
func (n *columnTreeNode) GetChildren() []api.TreeNode { return nil }

type indexTreeNode struct{ idx uir.RecordIndex }

func (n *indexTreeNode) Pretty() api.Text            { return n.idx.Pretty() }
func (n *indexTreeNode) GetChildren() []api.TreeNode { return nil }

type foreignKeyTreeNode struct{ fk uir.RecordForeignKey }

func (n *foreignKeyTreeNode) Pretty() api.Text            { return n.fk.Pretty() }
func (n *foreignKeyTreeNode) GetChildren() []api.TreeNode { return nil }

type fieldTreeNode struct{ f uir.RecordField }

func (n *fieldTreeNode) Pretty() api.Text            { return n.f.Pretty() }
func (n *fieldTreeNode) GetChildren() []api.TreeNode { return nil }
