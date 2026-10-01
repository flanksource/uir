package query

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/storage"
)

const (
	// unresolvedNodePrefix starts the id of a call target the index could not resolve. The id names
	// the caller too: two callers' unresolved calls of one name are not known to be one target.
	unresolvedNodePrefix = "unresolved:"
	// packageScopePrefix starts the id of the node standing for the calls a package makes outside
	// any declaration.
	packageScopePrefix = "package:"
)

// scopeSources finds, for a path of a selected snapshot, its scope and active document.
type scopeSources struct {
	index     *indexContext
	snapshots map[string]snapshotSources
}

type snapshotSources struct {
	position  int
	documents map[string]storage.ActiveDocument
}

func newScopeSources(index *indexContext) *scopeSources {
	return &scopeSources{index: index, snapshots: map[string]snapshotSources{}}
}

func (sources *scopeSources) located(snapshotID, path string) (int, storage.ActiveDocument, error) {
	snapshot, found := sources.snapshots[snapshotID]
	if !found {
		position := slices.IndexFunc(sources.index.scopes, func(scope indexScope) bool { return scope.snapshot.ID.String() == snapshotID })
		if position < 0 {
			return 0, storage.ActiveDocument{}, fmt.Errorf("snapshot %s is not among the selected snapshots", snapshotID)
		}
		active := sources.index.scopes[position].documents
		snapshot = snapshotSources{position: position, documents: make(map[string]storage.ActiveDocument, len(active))}
		for _, document := range active {
			snapshot.documents[document.Document.PathKey] = document
		}
		sources.snapshots[snapshotID] = snapshot
	}
	document, found := snapshot.documents[path]
	if !found {
		return 0, storage.ActiveDocument{}, fmt.Errorf("source %q is not active in snapshot %s", path, snapshotID)
	}
	return snapshot.position, document, nil
}

func (sources *scopeSources) document(snapshotID, path string) (moduleScope, storage.ActiveDocument, error) {
	position, document, err := sources.located(snapshotID, path)
	if err != nil {
		return moduleScope{}, storage.ActiveDocument{}, err
	}
	return sources.index.scopes[position].moduleScope, document, nil
}

// indexGraphSource serves the call graph of the selected snapshots from the symbol index. A node is
// a canonical symbol, an edge the call occurrences between two of them, and a dispatch edge a call
// of an interface method reaching an implementation. guards reads each site's call and guards from
// source; a site in an unreadable file keeps the callee text the index recorded and has no guards.
type indexGraphSource struct {
	index   *compactIndex
	sources *scopeSources
	guards  *guardReader
	symbols map[string]ModuleSymbol
	nodes   map[string]graph.Node
	stages  []ResolutionStage
	// declared are the package paths of the selected snapshots.
	declared map[string]bool
	// packages describe the group of every node the source has made, for exclusion patterns.
	packages map[string]graphPackageFacts
}

func newIndexGraphSource(index *compactIndex, sources *scopeSources, guards *guardReader) *indexGraphSource {
	return &indexGraphSource{
		index: index, sources: sources, guards: guards, symbols: map[string]ModuleSymbol{}, nodes: map[string]graph.Node{},
		declared: scopePackages(index.indexContext), packages: map[string]graphPackageFacts{},
	}
}

// notePackage describes a group the first time a node of it is made. A package outside the selected
// snapshots is external; an external test package, p_test, lives beside p and is not.
func (source *indexGraphSource) notePackage(path, module string, builtin bool) {
	if _, known := source.packages[path]; known {
		return
	}
	declared := source.declared[path] || source.declared[strings.TrimSuffix(path, "_test")]
	source.packages[path] = graphPackageFacts{path: path, module: module, builtin: builtin, external: !declared}
}

// packageFacts describes the group of a node the source made.
func (source *indexGraphSource) packageFacts(group string) graphPackageFacts {
	facts, known := source.packages[group]
	if !known {
		panic(fmt.Sprintf("query: graph group %q belongs to no node the graph source made", group))
	}
	return facts
}

func (source *indexGraphSource) Describe(ctx context.Context, id string) (graph.Node, error) {
	if _, err := source.symbol(ctx, id); err != nil {
		return graph.Node{}, err
	}
	return source.nodes[id], nil
}

// Out are the calls the symbol's declarations make: a call step to each resolved target, a dispatch
// step to each implementation of an interface method it calls, and a call step to an unresolved
// node for each call the index could not resolve.
func (source *indexGraphSource) Out(ctx context.Context, id string) ([]graph.Step, error) {
	if strings.HasPrefix(id, packageScopePrefix) {
		return nil, nil
	}
	symbol, err := source.symbol(ctx, id)
	if err != nil {
		return nil, err
	}
	calls, err := source.index.callees(ctx, symbol)
	if err != nil {
		return nil, err
	}
	edges, err := source.index.forwardEdges(ctx, calls)
	if err != nil {
		return nil, err
	}
	targets := make([]ModuleSymbol, 0, len(edges))
	for _, edge := range edges {
		targets = append(targets, edge.symbol)
	}
	if err := source.remember(ctx, targets); err != nil {
		return nil, err
	}
	steps := make([]graph.Step, 0, len(calls))
	for _, edge := range edges {
		if steps, err = source.step(ctx, steps, source.nodes[edge.symbol.ID], edge.call); err != nil {
			return nil, err
		}
	}
	for _, call := range calls {
		if call.SymbolID != "" {
			continue
		}
		if steps, err = source.step(ctx, steps, unresolvedNode(symbol, call), call); err != nil {
			return nil, err
		}
	}
	return steps, nil
}

// In are the calls of a function or method, as `symbol <` lists them: a call step from each
// enclosing declaration, and a dispatch step where the caller reaches the symbol through an
// interface method its receiver implements. Nothing calls a symbol of another kind.
func (source *indexGraphSource) In(ctx context.Context, id string) ([]graph.Step, error) {
	if strings.HasPrefix(id, packageScopePrefix) {
		return nil, nil
	}
	symbol, err := source.symbol(ctx, id)
	if err != nil || !callable(symbol) {
		return nil, err
	}
	noted := &ModuleQueryResult{}
	rows, err := source.index.relationRows(ctx, symbol, "<", noted)
	if err != nil {
		return nil, err
	}
	source.note(noted.Stages)
	enclosing := map[string]bool{}
	for _, row := range rows {
		if row.EnclosingID != "" {
			enclosing[row.EnclosingID] = true
		}
	}
	callers, err := source.index.symbolsByID(ctx, sortedKeys(enclosing))
	if err != nil {
		return nil, err
	}
	if err := source.remember(ctx, callers); err != nil {
		return nil, err
	}
	steps := make([]graph.Step, 0, len(rows))
	for _, row := range rows {
		caller, declared := source.nodes[row.EnclosingID]
		if !declared {
			caller = packageScopeNode(row)
			source.notePackage(row.PackagePath, row.RootKey, false)
		}
		if steps, err = source.step(ctx, steps, caller, row); err != nil {
			return nil, err
		}
	}
	return steps, nil
}

// step appends the step to a neighbour over one call occurrence row. The site's text is the whole call
// as written, or the callee as the index recorded it when the file is unreadable.
func (source *indexGraphSource) step(ctx context.Context, steps []graph.Step, neighbour graph.Node, call ModuleMatch) ([]graph.Step, error) {
	site := graph.Site{Path: call.Path, Text: call.text}
	if call.Line != nil && call.Column != nil {
		site.Line, site.Column = *call.Line, *call.Column
	}
	read, readable, err := source.guards.site(ctx, call)
	if err != nil {
		return nil, fmt.Errorf("call site of %s at %s:%d: %w", call.text, call.Path, site.Line, err)
	}
	if readable {
		site.Text, site.Guards = read.Call, guardTexts(read.Guards)
	}
	kind := uir.RelationshipTypeCall
	if call.Dispatch {
		kind = uir.RelationshipTypeDispatch
	}
	return append(steps, graph.Step{Node: neighbour, Edge: graph.Edge{Type: kind, Sites: []graph.Site{site}}}), nil
}

func (source *indexGraphSource) symbol(ctx context.Context, id string) (ModuleSymbol, error) {
	if symbol, known := source.symbols[id]; known {
		return symbol, nil
	}
	symbols, err := source.index.symbolsByID(ctx, []string{id})
	if err != nil {
		return ModuleSymbol{}, fmt.Errorf("load graph symbol %s: %w", id, err)
	}
	if err := source.remember(ctx, symbols); err != nil {
		return ModuleSymbol{}, err
	}
	return symbols[0], nil
}

// remember builds the node of each symbol it has not seen, reading their declarations together.
func (source *indexGraphSource) remember(ctx context.Context, symbols []ModuleSymbol) error {
	var ids []string
	for _, symbol := range symbols {
		if _, known := source.symbols[symbol.ID]; !known {
			source.symbols[symbol.ID] = symbol
			ids = append(ids, symbol.ID)
		}
	}
	declarations, err := source.index.declarations(ctx, ids, "definition")
	if err != nil {
		return err
	}
	sortMatches(declarations)
	declared := map[string][]ModuleMatch{}
	for _, declaration := range declarations {
		declared[declaration.SymbolID] = append(declared[declaration.SymbolID], declaration)
	}
	for _, id := range ids {
		node, err := source.node(source.symbols[id], declared[id])
		if err != nil {
			return err
		}
		source.nodes[id] = node
	}
	return nil
}

// node describes a symbol. Its group is its package path, or builtin for a predeclared function. One
// with no declaration in the selected snapshots, such as a standard library function, has no
// location. One with several, such as a function declared per build tag, is located at the first by
// path and carries their count in the declarations property.
func (source *indexGraphSource) node(symbol ModuleSymbol, declarations []ModuleMatch) (graph.Node, error) {
	node := graph.Node{
		ID: symbol.ID, Identifier: symbol.identifier(), Kind: symbol.Kind,
		Label: strings.TrimPrefix(symbol.QueryName, symbol.PackagePath+"."), Group: symbol.PackagePath,
	}
	builtin := symbol.Kind == "builtin"
	if builtin {
		node.Group = builtinGroup
	}
	source.notePackage(node.Group, symbol.ModuleKey, builtin)
	if len(declarations) == 0 {
		return node, nil
	}
	first := declarations[0]
	if first.Line == nil || first.Column == nil {
		return graph.Node{}, fmt.Errorf("declaration of %s in %s has no source position", symbol.QueryName, first.Path)
	}
	position, active, err := source.sources.located(first.SnapshotID, first.Path)
	if err != nil {
		return graph.Node{}, err
	}
	document, err := source.index.document(scopedPosting{scope: position, document: active.Document.ID})
	if err != nil {
		return graph.Node{}, err
	}
	node.Identifier = first.Identifier
	node.Location = &graph.Location{
		RootKey: first.RootKey, CheckoutPath: first.Location, SnapshotID: first.SnapshotID,
		SourceID: active.Source.ID.String(), Path: first.Path, Line: *first.Line, Column: *first.Column,
	}
	if entry := document.entries[symbol.ID]; entry.Explorable() {
		node.Location.IdentityKey = entry.Key
	}
	if len(declarations) > 1 {
		node.Properties = map[string]string{"declarations": strconv.Itoa(len(declarations))}
	}
	return node, nil
}

// note keeps each stage the index reported once, in the order first reported.
func (source *indexGraphSource) note(stages []ResolutionStage) {
	for _, stage := range stages {
		if !slices.Contains(source.stages, stage) {
			source.stages = append(source.stages, stage)
		}
	}
}

func unresolvedNode(caller ModuleSymbol, call ModuleMatch) graph.Node {
	return graph.Node{
		ID:         unresolvedNodePrefix + caller.ID + ":" + call.Identifier.IdentityKey(),
		Identifier: call.Identifier, Kind: "unresolved", Label: call.text, Group: caller.PackagePath, Unresolved: true,
	}
}

func packageScopeNode(call ModuleMatch) graph.Node {
	return graph.Node{
		ID: packageScopePrefix + call.PackagePath, Identifier: call.Identifier,
		Kind: "package", Label: call.PackagePath, Group: call.PackagePath,
	}
}
