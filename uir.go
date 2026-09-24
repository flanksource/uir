package uir

import (
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky/api"
)

// UIR is the root structure containing all top-level modules, packages, types, records, and functions
// Nodes in the UIR are placed at their lowest level, i.e a MethodNode will be inside a TypedNode,
// which is inside a PackageNode, which is inside a ModuleNode.
// It serves as the entry point for representing code structure in a language-agnostic way.
type UIR struct {
	nilNode
	Modules   []ModuleNode    `json:"modules,omitempty"`
	Packages  []PackageNode   `json:"packages,omitempty"`
	Types     []TypedNode     `json:"types,omitempty"`
	Records   []ASTRecord     `json:"records,omitempty"`
	Tables    []RecordTable   `json:"tables,omitempty"`
	Endpoints []ASTEndpoint   `json:"endpoints,omitempty"`
	Functions []MethodNode    `json:"functions,omitempty"`
	Hierarchy *HierarchyGraph `json:"hierarchy,omitempty"`
	RawFiles  []RawFile       `json:"rawFiles,omitempty"`
	// Warnings lists the nodes Add could not place. It is in-process only:
	// an UnmarshalJSON caller reads it to learn the decoded document lost nodes.
	Warnings []Warning `json:"-"`
}

// RawFile is a verbatim file emitted into the output directory alongside
// generated code (e.g. package.json, tsconfig.json, eslint.config.mjs).
type RawFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (uir UIR) GetChildren() []Node {
	children := []Node{}
	for i := range uir.Modules {
		children = append(children, uir.Modules[i])
	}
	for i := range uir.Packages {
		children = append(children, uir.Packages[i])
	}
	for i := range uir.Types {
		children = append(children, uir.Types[i])
	}
	for i := range uir.Records {
		children = append(children, uir.Records[i])
	}
	for i := range uir.Tables {
		children = append(children, uir.Tables[i])
	}
	for i := range uir.Endpoints {
		children = append(children, uir.Endpoints[i])
	}
	for i := range uir.Functions {
		children = append(children, uir.Functions[i])
	}
	return children
}

func (uir *UIR) Add(node Node) *UIR {
	// Pointer forms are not a convenience: the polymorphic node registry builds
	// every node with reflect.New, so UnmarshalJSON always hands Add a pointer.
	// Without these arms a decoded UIR came back empty. PackageNode.Add has
	// carried both forms for the same reason.
	switch n := node.(type) {
	case ModuleNode:
		uir.Modules = append(uir.Modules, n)
	case *ModuleNode:
		uir.Modules = append(uir.Modules, *n)
	case PackageNode:
		uir.Packages = append(uir.Packages, n)
	case *PackageNode:
		uir.Packages = append(uir.Packages, *n)
	case TypedNode:
		uir.Types = append(uir.Types, n)
	case *TypedNode:
		uir.Types = append(uir.Types, *n)
	case ASTRecord:
		uir.Records = append(uir.Records, n)
	case *ASTRecord:
		uir.Records = append(uir.Records, *n)
	case RecordTable:
		uir.Tables = append(uir.Tables, n)
	case *RecordTable:
		uir.Tables = append(uir.Tables, *n)
	case ASTEndpoint:
		uir.Endpoints = append(uir.Endpoints, n)
	case *ASTEndpoint:
		uir.Endpoints = append(uir.Endpoints, *n)
	case MethodNode:
		uir.Functions = append(uir.Functions, n)
	case *MethodNode:
		uir.Functions = append(uir.Functions, *n)
	case UIRNode:
		return uir.AddNode(&n)
	case *UIRNode:
		return uir.AddNode(n)
	default:
		// A node type with no arm here is dropped, and UnmarshalJSON routes every
		// decoded node through Add — so a missing case silently empties a
		// round-tripped UIR. Say so rather than losing it quietly.
		uir.Warnings = append(uir.Warnings, Warning{Message: "uir: Add has no case for this node type; the node was dropped", Node: node})
	}
	return uir
}

// AddNode unwraps a UIRNode wrapper into the appropriate slice on UIR. The
// wrapper carries one of Module/Package/Type/Record/Endpoint/Function as a
// non-nil pointer; AddNode dereferences it and routes to Add. Library
// callers that hold []*UIRNode (the common case for plugin extractors)
// should prefer this over Add, since Add only matches concrete node types
// and silently drops UIRNode wrappers.
func (uir *UIR) AddNode(node *UIRNode) *UIR {
	if uir == nil || node == nil {
		return uir
	}
	switch {
	case node.Module != nil:
		return uir.Add(*node.Module)
	case node.Package != nil:
		return uir.Add(*node.Package)
	case node.Type != nil:
		return uir.Add(*node.Type)
	case node.Record != nil:
		return uir.Add(*node.Record)
	case node.Table != nil:
		return uir.Add(*node.Table)
	case node.Endpoint != nil:
		return uir.Add(*node.Endpoint)
	case node.Function != nil:
		return uir.Add(*node.Function)
	}
	return uir
}

// AddNodes is a vararg convenience wrapper for AddNode.
func (uir *UIR) AddNodes(nodes ...*UIRNode) *UIR {
	for _, n := range nodes {
		uir.AddNode(n)
	}
	return uir
}

func (uir UIR) IsEmpty() bool {
	return len(uir.Modules) == 0 &&
		len(uir.Packages) == 0 &&
		len(uir.Types) == 0 &&
		len(uir.Records) == 0 &&
		len(uir.Tables) == 0 &&
		len(uir.Endpoints) == 0 &&
		len(uir.Functions) == 0 &&
		len(uir.RawFiles) == 0 &&
		(uir.Hierarchy == nil || len(uir.Hierarchy.Nodes) == 0)
}

func (uir UIR) Merge(other UIR) UIR {
	uir.Modules = append(uir.Modules, other.Modules...)
	uir.Packages = append(uir.Packages, other.Packages...)
	uir.Types = append(uir.Types, other.Types...)
	uir.Records = append(uir.Records, other.Records...)
	uir.Tables = append(uir.Tables, other.Tables...)
	uir.Endpoints = append(uir.Endpoints, other.Endpoints...)
	uir.Functions = append(uir.Functions, other.Functions...)
	uir.Hierarchy = mergeHierarchyGraphs(uir.Hierarchy, other.Hierarchy)
	uir.Warnings = append(uir.Warnings, other.Warnings...)
	return uir
}

// Coalesce groups TypedNodes by (Module, Package, Type) and merges duplicates
// into a single node. This handles the common case where a logical class is
// emitted from multiple source files (e.g. an PolicyAdmin transaction whose verb
// methods live in separate XML files).
//
// For each group:
//   - Methods are concatenated (each contributing file adds its verb method)
//   - Variables are unioned by name
//   - Implements is unioned by Name (so the merged class implements every
//     contributing contract)
//   - SourceCode keeps the entry whose path looks most like a "root" (heuristic:
//     fewest path segments after `tr/<txn>/`); ties are broken by lexicographic order
//
// Coalesce is idempotent.
func (uir UIR) Coalesce() UIR {
	if len(uir.Types) <= 1 {
		return uir
	}
	groups := make(map[string]int, len(uir.Types))
	merged := make([]TypedNode, 0, len(uir.Types))
	for _, t := range uir.Types {
		key := t.Module + "|" + t.Package + "|" + t.Type
		if t.Package == "" && t.Type == "" {
			merged = append(merged, t)
			continue
		}
		if idx, ok := groups[key]; ok {
			merged[idx] = mergeTypedNode(merged[idx], t)
			continue
		}
		groups[key] = len(merged)
		merged = append(merged, t)
	}
	uir.Types = merged
	return uir
}

// mergeTypedNode merges b into a, returning the unioned result.
func mergeTypedNode(a, b TypedNode) TypedNode {
	a.Methods = append(a.Methods, b.Methods...)
	a.Variables = unionParams(a.Variables, b.Variables)
	a.Implements = unionTypeReferences(a.Implements, b.Implements)
	a.Types = append(a.Types, b.Types...)
	a.Metadata = mergeMetadata(a.Metadata, b.Metadata)
	if a.Constructor == nil && b.Constructor != nil {
		a.Constructor = b.Constructor
	}
	if a.Destructor == nil && b.Destructor != nil {
		a.Destructor = b.Destructor
	}
	if !a.IsAbstract && b.IsAbstract {
		a.IsAbstract = true
	}
	if preferTypedNodeSource(b.Location, a.Location) {
		a.SourceCode = b.SourceCode
	}
	return a
}

// ExternalImportProperty names the node property carrying extractor-supplied
// import hints, one `Symbol@@module` per line.
const ExternalImportProperty = "externalImport"

func mergeMetadata(a, b Metadata) Metadata {
	if len(b.Annotations) > 0 {
		a.Annotations = append(a.Annotations, b.Annotations...)
	}
	if len(b.Comments) > 0 {
		a.Comments = append(a.Comments, b.Comments...)
	}
	if len(b.Properties) > 0 {
		if a.Properties == nil {
			a.Properties = make(map[string]any, len(b.Properties))
		}
		for key, value := range b.Properties {
			// externalImport is a set of import hints, not a single answer:
			// mergeTypedNode keeps BOTH nodes' methods, so first-writer-wins
			// here would keep the second node's calls while dropping the
			// imports they need. Every other property is a scalar (an
			// outputPathHint has one right value), so those keep first-wins.
			if key == ExternalImportProperty {
				a.Properties[key] = unionImportHints(a.Properties[key], value)
				continue
			}
			if _, exists := a.Properties[key]; !exists {
				a.Properties[key] = value
			}
		}
	}
	return a
}

// unionImportHints concatenates two newline-separated import-hint lists,
// preserving order and dropping repeats.
func unionImportHints(a, b any) string {
	seen := make(map[string]struct{})
	var out []string
	for _, hints := range []any{a, b} {
		text, _ := hints.(string)
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if _, ok := seen[line]; ok {
				continue
			}
			seen[line] = struct{}{}
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func preferTypedNodeSource(candidate, current Location) bool {
	if current.Path == "" {
		return candidate.Path != ""
	}
	if candidate.Path == "" {
		return false
	}

	candidateScore := scoreCoalescedPath(candidate.Path)
	currentScore := scoreCoalescedPath(current.Path)
	if candidateScore.afterTransactionSegments == noTransactionPathScore && currentScore.afterTransactionSegments == noTransactionPathScore {
		return false
	}
	if candidateScore.afterTransactionSegments != currentScore.afterTransactionSegments {
		return candidateScore.afterTransactionSegments < currentScore.afterTransactionSegments
	}
	if candidateScore.fileRank != currentScore.fileRank {
		return candidateScore.fileRank < currentScore.fileRank
	}
	return candidateScore.path < currentScore.path
}

type coalescedPathScore struct {
	afterTransactionSegments int
	fileRank                 int
	path                     string
}

const noTransactionPathScore = 1 << 30

func scoreCoalescedPath(raw string) coalescedPathScore {
	clean := filepath.ToSlash(strings.TrimPrefix(raw, "file://"))
	score := coalescedPathScore{
		afterTransactionSegments: noTransactionPathScore,
		fileRank:                 2,
		path:                     clean,
	}

	parts := strings.Split(clean, "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "tr" && parts[i+1] != "" {
			score.afterTransactionSegments = len(parts[i+2:])
			break
		}
	}

	switch filepath.Base(clean) {
	case "XMLData.xml":
		score.fileRank = 0
	case "data.xml":
		score.fileRank = 1
	}

	return score
}

func unionParams(a, b ParamsDef) ParamsDef {
	if len(b) == 0 {
		return a
	}
	seen := make(map[string]struct{}, len(a)+len(b))
	for _, p := range a {
		seen[p.Field] = struct{}{}
	}
	for _, p := range b {
		if _, ok := seen[p.Field]; ok {
			continue
		}
		seen[p.Field] = struct{}{}
		a = append(a, p)
	}
	return a
}

func unionTypeReferences(a, b []TypeReference) []TypeReference {
	if len(b) == 0 {
		return a
	}
	seen := make(map[string]struct{}, len(a)+len(b))
	for _, t := range a {
		seen[t.Name] = struct{}{}
	}
	for _, t := range b {
		if _, ok := seen[t.Name]; ok {
			continue
		}
		seen[t.Name] = struct{}{}
		a = append(a, t)
	}
	return a
}

func (uir UIR) Count() int {
	count := 0
	count += len(uir.Modules)
	count += len(uir.Packages)
	count += len(uir.Types)
	count += len(uir.Records)
	count += len(uir.Tables)
	count += len(uir.Endpoints)
	count += len(uir.Functions)
	if uir.Hierarchy != nil {
		count += len(uir.Hierarchy.Nodes)
	}
	return count
}

func (uir UIR) AsTree() NodeTree {
	return NodeTree{Node: uir}
}

func (uir UIR) PrettyFull() api.Text {
	t := api.Text{Content: ""}
	for _, p := range uir.Packages {
		t = t.Add(p.Pretty()).NewLine()
	}
	for _, typ := range uir.Types {
		t = t.Add(typ.Pretty()).NewLine()
	}
	for _, r := range uir.Records {
		t = t.Add(r.Pretty()).NewLine()
	}
	for _, tbl := range uir.Tables {
		t = t.Add(tbl.Pretty()).NewLine()
	}
	for _, e := range uir.Endpoints {
		t = t.Add(e.Pretty()).NewLine()
	}
	for _, f := range uir.Functions {
		t = t.Add(f.Pretty()).NewLine()
	}
	return t
}

func (uir UIR) Pretty() api.Text {
	m := make(map[string]any)
	t := api.Text{Content: ""}
	if len(uir.Modules) > 0 {
		m["modules"] = len(uir.Modules)
	}
	if len(uir.Packages) > 0 {
		m["packages"] = len(uir.Packages)
	}
	if len(uir.Types) > 0 {
		m["types"] = len(uir.Types)
	}
	if len(uir.Records) > 0 {
		m["records"] = len(uir.Records)
	}
	if len(uir.Tables) > 0 {
		m["tables"] = len(uir.Tables)
	}
	if len(uir.Endpoints) > 0 {
		m["endpoints"] = len(uir.Endpoints)
	}
	if len(uir.Functions) > 0 {
		m["functions"] = len(uir.Functions)
	}
	files := uir.AsTree().GetFiles()
	if len(files) > 3 {
		m["files"] = len(files)
	} else if len(files) > 0 {
		m["files"] = files
	}
	return t.Add(api.Map(m))
}
