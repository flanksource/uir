// Package diff computes structural differences between UIR nodes and trees.
//
// Phase 1 (DiffNode) compares two nodes, including method bodies and statements.
// Phase 2 (DiffTree) compares whole UIR documents, classifying renames and moves
// from flattened identity plus the selective semantic hashes in the uir package.
package diff

import (
	"context"
	"fmt"
	"sort"

	"github.com/pkg/diff/edit"
	"github.com/pkg/diff/myers"

	"github.com/flanksource/uir"
)

type ChangeKind string

const (
	ChangeAdded      ChangeKind = "added"
	ChangeDeleted    ChangeKind = "deleted"
	ChangeModified   ChangeKind = "modified"
	ChangeReplaced   ChangeKind = "replaced"
	ChangeRenamed    ChangeKind = "renamed"
	ChangeMoved      ChangeKind = "moved"
	ChangeRenameMove ChangeKind = "rename_move"
)

type NodeDiffOptions struct {
	Statements bool `json:"statements,omitempty"`
}

type TreeDiffOptions struct {
	NodeDiffOptions `json:",inline"`
	Moves           bool `json:"moves,omitempty"`
	Renames         bool `json:"renames,omitempty"`
}

func DefaultNodeDiffOptions() NodeDiffOptions {
	return NodeDiffOptions{Statements: true}
}

func DefaultTreeDiffOptions() TreeDiffOptions {
	return TreeDiffOptions{
		NodeDiffOptions: DefaultNodeDiffOptions(),
		Moves:           true,
		Renames:         true,
	}
}

type DiffSummary struct {
	Added      int `json:"added,omitempty"`
	Deleted    int `json:"deleted,omitempty"`
	Modified   int `json:"modified,omitempty"`
	Replaced   int `json:"replaced,omitempty"`
	Renamed    int `json:"renamed,omitempty"`
	Moved      int `json:"moved,omitempty"`
	RenameMove int `json:"renameMove,omitempty"`
}

type DiffResult struct {
	Summary DiffSummary  `json:"summary"`
	Changes []DiffChange `json:"changes,omitempty"`
}

type DiffChange struct {
	Kind    ChangeKind   `json:"kind"`
	Path    string       `json:"path,omitempty"`
	Detail  string       `json:"detail,omitempty"`
	Before  *DiffNodeRef `json:"before,omitempty"`
	After   *DiffNodeRef `json:"after,omitempty"`
	Changes []DiffChange `json:"changes,omitempty"`
}

type DiffNodeRef struct {
	ID       string       `json:"id,omitempty"`
	Symbol   string       `json:"symbol,omitempty"`
	NodeType uir.NodeType `json:"nodeType,omitempty"`
	Name     string       `json:"name,omitempty"`
	Parent   string       `json:"parent,omitempty"`
	File     string       `json:"file,omitempty"`
	Hash     string       `json:"hash,omitempty"`
}

func (r *DiffResult) Add(change DiffChange) {
	if change.Kind == "" {
		return
	}
	r.Changes = append(r.Changes, change)
	r.Summary.add(change.Kind)
	for _, child := range change.Changes {
		r.Summary.add(child.Kind)
	}
}

func (s *DiffSummary) add(kind ChangeKind) {
	switch kind {
	case ChangeAdded:
		s.Added++
	case ChangeDeleted:
		s.Deleted++
	case ChangeModified:
		s.Modified++
	case ChangeReplaced:
		s.Replaced++
	case ChangeRenamed:
		s.Renamed++
	case ChangeMoved:
		s.Moved++
	case ChangeRenameMove:
		s.RenameMove++
	}
}

func (r *DiffResult) merge(other DiffResult) {
	for _, change := range other.Changes {
		r.Add(change)
	}
}

func DiffNode(before, after uir.Node, opts NodeDiffOptions) DiffResult {
	return diffNode(before, after, opts, true)
}

func diffNode(before, after uir.Node, opts NodeDiffOptions, reportIdentity bool) DiffResult {
	var result DiffResult
	switch {
	case before == nil && after == nil:
		return result
	case before == nil:
		result.Add(DiffChange{Kind: ChangeAdded, Path: nodePath(after), After: newDiffNodeRef(after, "")})
		return result
	case after == nil:
		result.Add(DiffChange{Kind: ChangeDeleted, Path: nodePath(before), Before: newDiffNodeRef(before, "")})
		return result
	}

	if before.GetType() != after.GetType() {
		result.Add(DiffChange{
			Kind:   ChangeReplaced,
			Path:   nodePath(after),
			Before: newDiffNodeRef(before, ""),
			After:  newDiffNodeRef(after, ""),
			Detail: fmt.Sprintf("%s -> %s", before.GetType(), after.GetType()),
		})
		return result
	}

	if before.Hash() == after.Hash() && diffIdentity(before) == diffIdentity(after) {
		return result
	}
	if reportIdentity {
		result.addIdentityChange(before, after)
	}

	switch b := before.(type) {
	case uir.MethodNode:
		if a, ok := after.(uir.MethodNode); ok {
			result.merge(diffMethodNode(b, a, opts))
			return result
		}
	case *uir.MethodNode:
		if a, ok := after.(*uir.MethodNode); ok {
			result.merge(diffMethodNode(*b, *a, opts))
			return result
		}
	case uir.ASTRecord:
		if a, ok := after.(uir.ASTRecord); ok {
			result.merge(diffNodeSlices(nodePath(after)+".fields", uir.NodesOf(b.Fields), uir.NodesOf(a.Fields), opts, reportIdentity))
			result.addModifiedIfDifferent(before, after, "record attributes")
			return result
		}
	case uir.RecordTable:
		if a, ok := after.(uir.RecordTable); ok {
			path := nodePath(after)
			result.merge(diffNodeSlices(path+".columns", uir.NodesOf(b.Columns), uir.NodesOf(a.Columns), opts, reportIdentity))
			result.merge(diffNodeSlices(path+".indexes", uir.NodesOf(b.Indexes), uir.NodesOf(a.Indexes), opts, reportIdentity))
			result.merge(diffNodeSlices(path+".foreignKeys", uir.NodesOf(b.ForeignKeys), uir.NodesOf(a.ForeignKeys), opts, reportIdentity))
			result.addModifiedIfDifferent(before, after, "table attributes")
			return result
		}
	case uir.TypedNode:
		if a, ok := after.(uir.TypedNode); ok {
			result.merge(diffNodeSlices(nodePath(after)+".variables", uir.NodesOf(b.Variables), uir.NodesOf(a.Variables), opts, reportIdentity))
			result.merge(diffNodeSlices(nodePath(after)+".methods", uir.NodesOf(b.Methods), uir.NodesOf(a.Methods), opts, reportIdentity))
			result.merge(diffNodeSlices(nodePath(after)+".types", uir.NodesOf(b.Types), uir.NodesOf(a.Types), opts, reportIdentity))
			result.addModifiedIfDifferent(before, after, "type attributes")
			return result
		}
	case uir.PackageNode:
		if a, ok := after.(uir.PackageNode); ok {
			result.merge(diffNodeSlices(nodePath(after)+".types", uir.NodesOf(b.Types), uir.NodesOf(a.Types), opts, reportIdentity))
			result.merge(diffNodeSlices(nodePath(after)+".records", uir.NodesOf(b.Records), uir.NodesOf(a.Records), opts, reportIdentity))
			result.merge(diffNodeSlices(nodePath(after)+".tables", uir.NodesOf(b.Tables), uir.NodesOf(a.Tables), opts, reportIdentity))
			result.merge(diffNodeSlices(nodePath(after)+".endpoints", uir.NodesOf(b.Endpoints), uir.NodesOf(a.Endpoints), opts, reportIdentity))
			result.merge(diffNodeSlices(nodePath(after)+".functions", uir.NodesOf(b.Functions), uir.NodesOf(a.Functions), opts, reportIdentity))
			result.merge(diffNodeSlices(nodePath(after)+".initFunctions", uir.NodesOf(b.InitFunctions), uir.NodesOf(a.InitFunctions), opts, reportIdentity))
			result.addModifiedIfDifferent(before, after, "package attributes")
			return result
		}
	case uir.ModuleNode:
		if a, ok := after.(uir.ModuleNode); ok {
			result.merge(diffNodeSlices(nodePath(after)+".packages", uir.NodesOf(b.Packages), uir.NodesOf(a.Packages), opts, reportIdentity))
			return result
		}
	case uir.UIR:
		if a, ok := after.(uir.UIR); ok {
			result.merge(diffNodeSlices("modules", uir.NodesOf(b.Modules), uir.NodesOf(a.Modules), opts, reportIdentity))
			result.merge(diffNodeSlices("packages", uir.NodesOf(b.Packages), uir.NodesOf(a.Packages), opts, reportIdentity))
			result.merge(diffNodeSlices("types", uir.NodesOf(b.Types), uir.NodesOf(a.Types), opts, reportIdentity))
			result.merge(diffNodeSlices("records", uir.NodesOf(b.Records), uir.NodesOf(a.Records), opts, reportIdentity))
			result.merge(diffNodeSlices("tables", uir.NodesOf(b.Tables), uir.NodesOf(a.Tables), opts, reportIdentity))
			result.merge(diffNodeSlices("endpoints", uir.NodesOf(b.Endpoints), uir.NodesOf(a.Endpoints), opts, reportIdentity))
			result.merge(diffNodeSlices("functions", uir.NodesOf(b.Functions), uir.NodesOf(a.Functions), opts, reportIdentity))
			result.addModifiedIfDifferent(before, after, "uir attributes")
			return result
		}
	case uir.NodeList:
		if a, ok := after.(uir.NodeList); ok {
			result.merge(diffNodeSlices("nodes", []uir.Node(b), []uir.Node(a), opts, reportIdentity))
			return result
		}
	}

	result.merge(diffNodeSlices(nodePath(after)+".children", diffChildren(before), diffChildren(after), opts, reportIdentity))
	result.addModifiedIfDifferent(before, after, "attributes")
	return result
}

func (r *DiffResult) addIdentityChange(before, after uir.Node) {
	kind := classifyNodeIdentityChange(before, after)
	if kind == "" {
		return
	}
	r.Add(DiffChange{
		Kind:   kind,
		Path:   nodePath(after),
		Before: newDiffNodeRef(before, ""),
		After:  newDiffNodeRef(after, ""),
	})
}

func (r *DiffResult) addModifiedIfDifferent(before, after uir.Node, detail string) {
	if before.Hash() == after.Hash() {
		return
	}
	r.Add(DiffChange{
		Kind:   ChangeModified,
		Path:   nodePath(after),
		Before: newDiffNodeRef(before, ""),
		After:  newDiffNodeRef(after, ""),
		Detail: detail,
	})
}

func diffMethodNode(before, after uir.MethodNode, opts NodeDiffOptions) DiffResult {
	var result DiffResult
	if before.Hash() == after.Hash() {
		return result
	}
	if opts.Statements {
		result.merge(diffBlocks(nodePath(after)+".body", before.Body, after.Body))
	}
	result.addModifiedIfDifferent(before, after, "method attributes")
	return result
}

func diffNodeSlices(path string, before, after []uir.Node, opts NodeDiffOptions, reportIdentity bool) DiffResult {
	var result DiffResult
	script := myers.Diff(context.Background(), nodePair{before: before, after: after})
	for _, r := range script.Ranges {
		switch r.Op() {
		case edit.Eq:
			for i := 0; i < r.Len(); i++ {
				result.merge(diffNode(before[r.LowA+i], after[r.LowB+i], opts, reportIdentity))
			}
		case edit.Del:
			for i := r.LowA; i < r.HighA; i++ {
				result.Add(DiffChange{Kind: ChangeDeleted, Path: path, Before: newDiffNodeRef(before[i], "")})
			}
		case edit.Ins:
			for i := r.LowB; i < r.HighB; i++ {
				result.Add(DiffChange{Kind: ChangeAdded, Path: path, After: newDiffNodeRef(after[i], "")})
			}
		}
	}
	return result
}

type nodePair struct {
	before []uir.Node
	after  []uir.Node
}

func (p nodePair) LenA() int { return len(p.before) }
func (p nodePair) LenB() int { return len(p.after) }
func (p nodePair) Equal(ai, bi int) bool {
	a := p.before[ai]
	b := p.after[bi]
	if a == nil || b == nil {
		return a == b
	}
	if a.GetIdentifier().SymbolKey() != "" && a.GetIdentifier().SymbolKey() == b.GetIdentifier().SymbolKey() {
		return true
	}
	return a.Hash() == b.Hash()
}

func diffIdentity(node uir.Node) string {
	if node == nil {
		return ""
	}
	h := uir.NewHasher("diff-identity")
	if stmt, ok := node.(uir.Statement); ok {
		h.AddString("node_type", string(uir.NodeTypeStatement))
		h.AddString("statement_type", string(stmt.GetStatementType()))
	} else {
		h.AddString("node_type", string(node.GetType()))
		id := node.GetIdentifier()
		h.AddString("symbol", id.SymbolKey())
	}
	children := diffChildren(node)
	h.AddInt("children.len", len(children))
	for i, child := range children {
		h.AddString(fmt.Sprintf("children.%d", i), diffIdentity(child))
	}
	return h.String()
}

func diffChildren(node uir.Node) []uir.Node {
	switch n := node.(type) {
	case nil:
		return nil
	case uir.NodeRef:
		return nil
	case *uir.NodeRef:
		return nil
	case uir.UIR:
		return n.GetChildren()
	case *uir.UIR:
		return n.GetChildren()
	case uir.ModuleNode:
		return n.GetChildren()
	case *uir.ModuleNode:
		return n.GetChildren()
	case uir.PackageNode:
		return n.GetChildren()
	case *uir.PackageNode:
		return n.GetChildren()
	case uir.TypedNode:
		return n.GetChildren()
	case *uir.TypedNode:
		return n.GetChildren()
	case uir.ASTRecord:
		return n.GetChildren()
	case *uir.ASTRecord:
		return n.GetChildren()
	case uir.MethodNode:
		return n.GetChildren()
	case *uir.MethodNode:
		return n.GetChildren()
	case uir.BlockStmt:
		return n.GetChildren()
	case *uir.BlockStmt:
		return n.GetChildren()
	case uir.ExprStmt:
		return n.GetChildren()
	case *uir.ExprStmt:
		return n.GetChildren()
	default:
		return node.GetChildren()
	}
}

func classifyNodeIdentityChange(before, after uir.Node) ChangeKind {
	beforeID := before.GetIdentifier()
	afterID := after.GetIdentifier()
	if beforeID.SymbolKey() == afterID.SymbolKey() {
		return ""
	}
	renamed := beforeID.GetName() != "" && afterID.GetName() != "" && beforeID.GetName() != afterID.GetName()
	moved := identifierParentKey(beforeID) != identifierParentKey(afterID)
	switch {
	case renamed && moved:
		return ChangeRenameMove
	case renamed:
		return ChangeRenamed
	case moved:
		return ChangeMoved
	default:
		return ""
	}
}

func identifierParentKey(id uir.Identifier) string {
	id.Signature = ""
	switch id.GetNodeType() {
	// A column, index and foreign key all hang off a table the same way a field
	// hangs off a record: the parent is the identifier with the member cleared.
	case uir.NodeTypeField, uir.NodeTypeColumn, uir.NodeTypeIndex, uir.NodeTypeForeignKey:
		id.Field = ""
	case uir.NodeTypeMethod, uir.NodeTypeFunction, uir.NodeTypeEndpoint, uir.NodeTypeConstructor:
		id.Method = ""
		id.Field = ""
	case uir.NodeTypeType, uir.NodeTypeRecord, uir.NodeTypeTable, uir.NodeTypeInterface:
		id.Type = ""
		id.Method = ""
		id.Field = ""
	case uir.NodeTypePackage:
		id.Package = ""
		id.Type = ""
		id.Method = ""
		id.Field = ""
	case uir.NodeTypeModule:
		id.Module = ""
		id.Package = ""
		id.Type = ""
		id.Method = ""
		id.Field = ""
	default:
		id.Field = ""
		id.Method = ""
	}
	id.NodeType = uir.NodeTypeUnknown
	return id.SymbolKey()
}

func diffBlocks(path string, before, after *uir.BlockStmt) DiffResult {
	var result DiffResult
	switch {
	case before == nil && after == nil:
		return result
	case before == nil:
		result.Add(DiffChange{Kind: ChangeAdded, Path: path, Detail: "body added"})
		return result
	case after == nil:
		result.Add(DiffChange{Kind: ChangeDeleted, Path: path, Detail: "body deleted"})
		return result
	case before.Hash() == after.Hash():
		return result
	}
	result.merge(diffStatements(path, before.Children, after.Children))
	return result
}

func diffStatements(path string, before, after []uir.Statement) DiffResult {
	var result DiffResult
	script := myers.Diff(context.Background(), statementPair{before: before, after: after})
	deletedByHash := map[string][]int{}
	insertedByHash := map[string][]int{}

	for _, r := range script.Ranges {
		switch r.Op() {
		case edit.Eq:
			continue
		case edit.Del:
			for i := r.LowA; i < r.HighA; i++ {
				deletedByHash[uir.HashStatement(before[i])] = append(deletedByHash[uir.HashStatement(before[i])], i)
			}
		case edit.Ins:
			for i := r.LowB; i < r.HighB; i++ {
				insertedByHash[uir.HashStatement(after[i])] = append(insertedByHash[uir.HashStatement(after[i])], i)
			}
		}
	}

	movedDeleted := map[int]bool{}
	movedInserted := map[int]bool{}
	for hash, dels := range deletedByHash {
		ins := insertedByHash[hash]
		if len(dels) == 1 && len(ins) == 1 {
			movedDeleted[dels[0]] = true
			movedInserted[ins[0]] = true
			result.Add(DiffChange{
				Kind:   ChangeMoved,
				Path:   path,
				Detail: fmt.Sprintf("statement %d -> %d", dels[0], ins[0]),
			})
		}
	}

	for _, r := range script.Ranges {
		switch r.Op() {
		case edit.Del:
			for i := r.LowA; i < r.HighA; i++ {
				if !movedDeleted[i] {
					result.Add(DiffChange{Kind: ChangeDeleted, Path: path, Detail: fmt.Sprintf("statement %d", i)})
				}
			}
		case edit.Ins:
			for i := r.LowB; i < r.HighB; i++ {
				if !movedInserted[i] {
					result.Add(DiffChange{Kind: ChangeAdded, Path: path, Detail: fmt.Sprintf("statement %d", i)})
				}
			}
		}
	}
	return result
}

type statementPair struct {
	before []uir.Statement
	after  []uir.Statement
}

func (p statementPair) LenA() int { return len(p.before) }
func (p statementPair) LenB() int { return len(p.after) }
func (p statementPair) Equal(ai, bi int) bool {
	return uir.HashStatement(p.before[ai]) == uir.HashStatement(p.after[bi])
}

func DiffTree(before, after *uir.UIR, opts TreeDiffOptions) DiffResult {
	beforeTree := uir.UIR{}
	afterTree := uir.UIR{}
	if before != nil {
		beforeTree = before.Coalesce()
	}
	if after != nil {
		afterTree = after.Coalesce()
	}

	beforeEntries := flattenDiffEntries(beforeTree)
	afterEntries := flattenDiffEntries(afterTree)
	matches := map[int]int{}
	matchedBefore := map[int]bool{}
	matchedAfter := map[int]bool{}

	matchUnique(beforeEntries, afterEntries, matches, matchedBefore, matchedAfter, func(e diffEntry) string {
		return e.uuid
	})
	matchUnique(beforeEntries, afterEntries, matches, matchedBefore, matchedAfter, func(e diffEntry) string {
		return e.symbol
	})

	var result DiffResult
	if opts.Moves || opts.Renames {
		matchUnique(beforeEntries, afterEntries, matches, matchedBefore, matchedAfter, func(e diffEntry) string {
			return e.hash
		})
	}

	for bi, ai := range matches {
		b := beforeEntries[bi]
		a := afterEntries[ai]
		if b.symbol != a.symbol {
			kind := classifyRelocation(b, a, opts)
			if kind != "" {
				result.Add(DiffChange{
					Kind:   kind,
					Path:   a.symbol,
					Before: newDiffNodeRef(b.node, b.parent),
					After:  newDiffNodeRef(a.node, a.parent),
				})
			}
		}
		result.merge(diffNode(b.node, a.node, opts.NodeDiffOptions, false))
	}

	for i, entry := range beforeEntries {
		if !matchedBefore[i] {
			result.Add(DiffChange{Kind: ChangeDeleted, Path: entry.symbol, Before: newDiffNodeRef(entry.node, entry.parent)})
		}
	}
	for i, entry := range afterEntries {
		if !matchedAfter[i] {
			result.Add(DiffChange{Kind: ChangeAdded, Path: entry.symbol, After: newDiffNodeRef(entry.node, entry.parent)})
		}
	}

	sort.SliceStable(result.Changes, func(i, j int) bool {
		a := result.Changes[i]
		b := result.Changes[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Detail < b.Detail
	})
	return result
}

type diffEntry struct {
	node   uir.Node
	parent string
	uuid   string
	symbol string
	name   string
	file   string
	hash   string
}

func flattenDiffEntries(uir uir.UIR) []diffEntry {
	var entries []diffEntry
	for _, child := range uir.GetChildren() {
		flattenNode(child, "", &entries)
	}
	return entries
}

func flattenNode(node uir.Node, parent string, entries *[]diffEntry) {
	if node == nil {
		return
	}
	id := node.GetIdentifier()
	symbol := id.SymbolKey()
	entry := diffEntry{
		node:   node,
		parent: parent,
		uuid:   id.GetUUID().String(),
		symbol: symbol,
		name:   id.GetName(),
		file:   node.GetLocation().Path,
		hash:   node.Hash(),
	}
	*entries = append(*entries, entry)
	for _, child := range node.GetChildren() {
		flattenNode(child, symbol, entries)
	}
}

func matchUnique(before, after []diffEntry, matches map[int]int, matchedBefore, matchedAfter map[int]bool, keyFn func(diffEntry) string) {
	beforeByKey := map[string][]int{}
	afterByKey := map[string][]int{}
	for i, entry := range before {
		if matchedBefore[i] {
			continue
		}
		key := keyFn(entry)
		if key == "" {
			continue
		}
		beforeByKey[key] = append(beforeByKey[key], i)
	}
	for i, entry := range after {
		if matchedAfter[i] {
			continue
		}
		key := keyFn(entry)
		if key == "" {
			continue
		}
		afterByKey[key] = append(afterByKey[key], i)
	}
	for key, bis := range beforeByKey {
		ais := afterByKey[key]
		if len(bis) != 1 || len(ais) != 1 {
			continue
		}
		matches[bis[0]] = ais[0]
		matchedBefore[bis[0]] = true
		matchedAfter[ais[0]] = true
	}
}

func classifyRelocation(before, after diffEntry, opts TreeDiffOptions) ChangeKind {
	renamed := before.name != after.name
	moved := before.parent != after.parent || before.file != after.file
	switch {
	case renamed && moved && opts.Renames && opts.Moves:
		return ChangeRenameMove
	case renamed && opts.Renames:
		return ChangeRenamed
	case moved && opts.Moves:
		return ChangeMoved
	default:
		return ""
	}
}

func newDiffNodeRef(node uir.Node, parent string) *DiffNodeRef {
	if node == nil {
		return nil
	}
	id := node.GetIdentifier()
	return &DiffNodeRef{
		ID:       id.GetUUID().String(),
		Symbol:   id.SymbolKey(),
		NodeType: node.GetType(),
		Name:     id.GetName(),
		Parent:   parent,
		File:     node.GetLocation().Path,
		Hash:     node.Hash(),
	}
}

func nodePath(node uir.Node) string {
	if node == nil {
		return ""
	}
	return node.GetIdentifier().SymbolKey()
}
