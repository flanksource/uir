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
	Layer   uir.Layer    `json:"layer,omitempty"`
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

// DiffNode compares two nodes, pairing the members of each container by
// identity. It never pairs two different symbols: renames and moves are
// DiffTree's to classify.
func DiffNode(before, after uir.Node, opts NodeDiffOptions) DiffResult {
	return differ{opts: opts, walkMembers: true}.node(before, after)
}

// differ is one diff's configuration. walkMembers is set by DiffNode, which
// recurses into a container's members and reports identity changes itself;
// DiffTree clears it because it flattens every member into an entry of its own
// and classifies identity from its matching, so walking members there would
// report each member change twice.
type differ struct {
	opts        NodeDiffOptions
	walkMembers bool
}

func (d differ) node(before, after uir.Node) DiffResult {
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
	if d.walkMembers {
		result.addIdentityChange(before, after)
	}
	result.merge(d.contents(deref(before), deref(after)))
	return result
}

// contents diffs two nodes of the same NodeType: their members, their bodies
// and, last, the node itself.
func (d differ) contents(before, after uir.Node) DiffResult {
	path := nodePath(after) + "."
	var result DiffResult
	detail := "attributes"
	switch b := before.(type) {
	case uir.MethodNode:
		return d.method(b, sameType[uir.MethodNode](before, after))
	case uir.RecordTable:
		return d.table(b, sameType[uir.RecordTable](before, after))
	case uir.ASTRecord:
		a := sameType[uir.ASTRecord](before, after)
		result, detail = d.members(path+"fields", uir.NodesOf(b.Fields), uir.NodesOf(a.Fields)), "record attributes"
	case uir.TypedNode:
		result, detail = d.typed(path, b, sameType[uir.TypedNode](before, after)), "type attributes"
	case uir.PackageNode:
		result, detail = d.pkg(path, b, sameType[uir.PackageNode](before, after)), "package attributes"
	case uir.ModuleNode:
		a := sameType[uir.ModuleNode](before, after)
		result, detail = d.members(path+"packages", uir.NodesOf(b.Packages), uir.NodesOf(a.Packages)), "module attributes"
	case uir.UIR:
		result, detail = d.document(b, sameType[uir.UIR](before, after)), "uir attributes"
	case uir.NodeList:
		return d.members("nodes", b, sameType[uir.NodeList](before, after))
	default:
		result = d.members(path+"children", diffChildren(before), diffChildren(after))
	}
	result.addModifiedIfDifferent(before, after, detail)
	return result
}

func (d differ) typed(path string, b, a uir.TypedNode) DiffResult {
	result := d.members(path+"variables", uir.NodesOf(b.Variables), uir.NodesOf(a.Variables))
	result.merge(d.members(path+"methods", uir.NodesOf(b.Methods), uir.NodesOf(a.Methods)))
	result.merge(d.members(path+"types", uir.NodesOf(b.Types), uir.NodesOf(a.Types)))
	return result
}

func (d differ) pkg(path string, b, a uir.PackageNode) DiffResult {
	result := d.members(path+"types", uir.NodesOf(b.Types), uir.NodesOf(a.Types))
	result.merge(d.members(path+"records", uir.NodesOf(b.Records), uir.NodesOf(a.Records)))
	result.merge(d.members(path+"tables", uir.NodesOf(b.Tables), uir.NodesOf(a.Tables)))
	result.merge(d.members(path+"endpoints", uir.NodesOf(b.Endpoints), uir.NodesOf(a.Endpoints)))
	result.merge(d.members(path+"functions", uir.NodesOf(b.Functions), uir.NodesOf(a.Functions)))
	result.merge(d.members(path+"initFunctions", uir.NodesOf(b.InitFunctions), uir.NodesOf(a.InitFunctions)))
	return result
}

func (d differ) document(b, a uir.UIR) DiffResult {
	result := d.members("modules", uir.NodesOf(b.Modules), uir.NodesOf(a.Modules))
	result.merge(d.members("packages", uir.NodesOf(b.Packages), uir.NodesOf(a.Packages)))
	result.merge(d.members("types", uir.NodesOf(b.Types), uir.NodesOf(a.Types)))
	result.merge(d.members("records", uir.NodesOf(b.Records), uir.NodesOf(a.Records)))
	result.merge(d.members("tables", uir.NodesOf(b.Tables), uir.NodesOf(a.Tables)))
	result.merge(d.members("endpoints", uir.NodesOf(b.Endpoints), uir.NodesOf(a.Endpoints)))
	result.merge(d.members("functions", uir.NodesOf(b.Functions), uir.NodesOf(a.Functions)))
	return result
}

// table diffs members by key, column order as moves (order is a private fact of
// the table, not of a column), and the view definition as statements.
func (d differ) table(b, a uir.RecordTable) DiffResult {
	path := nodePath(a)
	result := d.members(path+".columns", uir.NodesOf(b.Columns), uir.NodesOf(a.Columns))
	result.merge(d.members(path+".indexes", uir.NodesOf(b.Indexes), uir.NodesOf(a.Indexes)))
	result.merge(d.members(path+".foreignKeys", uir.NodesOf(b.ForeignKeys), uir.NodesOf(a.ForeignKeys)))
	result.merge(d.members(path+".triggers", uir.NodesOf(b.Triggers), uir.NodesOf(a.Triggers)))
	result.merge(reorderedColumns(path+".columns", b.Columns, a.Columns))
	if d.opts.Statements {
		result.merge(diffBlocks(path+".definition", b.Definition, a.Definition))
	}
	result.addModifiedIfDifferent(b, a, "table attributes")
	return result
}

// deref unwraps the pointer form of a node contents switches on, so both forms
// diff the same way.
func deref(node uir.Node) uir.Node {
	switch n := node.(type) {
	case *uir.MethodNode:
		return *n
	case *uir.RecordTable:
		return *n
	case *uir.ASTRecord:
		return *n
	case *uir.TypedNode:
		return *n
	case *uir.PackageNode:
		return *n
	case *uir.ModuleNode:
		return *n
	case *uir.UIR:
		return *n
	}
	return node
}

// sameType asserts after is the Go type before is; contents only runs on nodes
// of equal NodeType, so a mismatch is two Go types claiming one NodeType.
func sameType[T uir.Node](before, after uir.Node) T {
	a, ok := after.(T)
	if !ok {
		panic(fmt.Sprintf("diff: %T and %T share node type %s", before, after, before.GetType()))
	}
	return a
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
		Layer:  changedLayer(before, after),
	})
}

// changedLayer is the outermost fingerprint layer that differs. A document and
// a statement have no layers; for any other node a differing Hash with equal
// layers is impossible, since Hash digests the layers.
func changedLayer(before, after uir.Node) uir.Layer {
	switch before.(type) {
	case uir.UIR, *uir.UIR, uir.Statement:
		return ""
	}
	b, a := uir.Fingerprint(before), uir.Fingerprint(after)
	switch {
	case b.Public != a.Public:
		return uir.LayerPublic
	case b.Private != a.Private:
		return uir.LayerPrivate
	case b.Content != a.Content:
		return uir.LayerContent
	}
	panic(fmt.Sprintf("diff: %s hashes differ but no fingerprint layer does", nodePath(after)))
}

func (d differ) method(before, after uir.MethodNode) DiffResult {
	var result DiffResult
	if before.Hash() == after.Hash() {
		return result
	}
	if d.opts.Statements {
		result.merge(diffBlocks(nodePath(after)+".body", before.Body, after.Body))
	}
	result.addModifiedIfDifferent(before, after, "method attributes")
	return result
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
		result.merge(differ{opts: opts.NodeDiffOptions}.node(b.node, a.node))
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
