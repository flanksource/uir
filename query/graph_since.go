package query

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
)

// GraphSince turns a call graph into a change query: has anything the root reaches changed between
// the snapshot SnapshotID and the scope's snapshot of the same root? Revision is what the caller
// selected the snapshot by, echoed in the report.
//
// The answer is only as complete as the call edges the index records. Known false negatives: an
// implementation outside the import closure of the interface it satisfies, dispatch through generic
// or type-set interfaces, and calls through method values or func-typed fields and variables produce
// no edge, so a change behind them is not reached. A change beyond Depth or Limit, behind an
// unresolved call, or in another root of the scope is not reported either; a changed module-cache
// dependency is reported at the node where the walk enters it.
type GraphSince struct {
	Revision   string
	SnapshotID uuid.UUID
}

// GraphChangeReason is why a node of a change query changed.
type GraphChangeReason string

const (
	GraphChangeAdded     GraphChangeReason = "added"
	GraphChangeRemoved   GraphChangeReason = "removed"
	GraphChangeSignature GraphChangeReason = "signature"
	GraphChangeStructure GraphChangeReason = "structure"
	GraphChangeBody      GraphChangeReason = "body"
	// GraphChangeDependency marks a node of a module-cache dependency whose selected version changed;
	// such dependencies have no per-symbol facts to compare.
	GraphChangeDependency GraphChangeReason = "dependency-version"
)

// GraphChanges is the answer to a change query.
type GraphChanges struct {
	Revision       string `json:"revision"`
	FromSnapshotID string `json:"from_snapshot_id"`
	ToSnapshotID   string `json:"to_snapshot_id"`
	// Changed is true when either walk reaches a changed symbol, a call edge is added or removed,
	// or either walk reaches a changed dependency that the exclusions left out.
	Changed bool `json:"changed"`
	// Before is the bounded graph of the same root in the earlier snapshot, absent for a newly added root.
	Before *graph.Graph `json:"before,omitempty"`
	// Nodes are changed symbols reached in either snapshot, current nodes first.
	Nodes []GraphChangedNode `json:"nodes"`
	// Edges are call or access edges added or removed between the bounded walks.
	Edges []GraphChangedEdge `json:"edges"`
	// Symbols counts the root's changed symbols by reason, reached or not.
	Symbols map[GraphChangeReason]int `json:"symbols"`
	// Dependencies are the dependencies whose selection changed, by module path.
	Dependencies []GraphDependencyChange `json:"dependencies"`
}

type GraphChangedNode struct {
	ID     string            `json:"id"`
	Reason GraphChangeReason `json:"reason"`
	// SnapshotID identifies the graph whose node and path this entry names.
	SnapshotID string `json:"snapshot_id"`
	// Module is the dependency of a GraphChangeDependency node.
	Module string `json:"module,omitempty"`
	// Path is the node ids of a shortest walk from the root to the node, root first.
	Path []string `json:"path"`
}

type GraphChangedEdge struct {
	ID     string            `json:"id"`
	Reason GraphChangeReason `json:"reason"`
	// SnapshotID identifies the graph whose edge and path this entry names.
	SnapshotID string   `json:"snapshot_id"`
	Path       []string `json:"path"`
}

// GraphDependencyChange is a dependency whose selection differs between the snapshots: From or To
// is empty for a dependency added or dropped.
type GraphDependencyChange struct {
	Module string `json:"module"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	// Excluded is true when either walk reached a package of the module that the exclusions left out,
	// so no drawn node carries the change.
	Excluded bool `json:"excluded,omitempty"`
}

// sinceSides are the two snapshots of one root a change query compares.
type sinceSides struct {
	request  GraphSince
	from, to storage.ModuleSnapshot
}

// sinceSides finds the scope's snapshot of the root that since.SnapshotID belongs to, and checks that
// their fingerprints are comparable.
func (pipeline *Pipeline) sinceSides(ctx context.Context, since GraphSince, scopes []moduleScope) (*sinceSides, error) {
	var from storage.ModuleSnapshot
	if err := pipeline.database.WithContext(ctx).Where("id = ?", since.SnapshotID).First(&from).Error; err != nil {
		return nil, lookupError(err, fmt.Sprintf("since snapshot %s", since.SnapshotID))
	}
	var matched []storage.ModuleSnapshot
	for _, scope := range scopes {
		if scope.snapshot.RootID == from.RootID {
			matched = append(matched, scope.snapshot)
		}
	}
	if len(matched) != 1 {
		return nil, invalidGraph("since snapshot %s needs exactly one snapshot of its root in the scope, the scope holds %d; select --root, --location or --snapshot", from.ID, len(matched))
	}
	if from.ConfigurationHash != matched[0].ConfigurationHash {
		return nil, fmt.Errorf("snapshots %s and %s were indexed under different configuration hashes (%s, %s), so their fingerprints are not comparable",
			from.ID, matched[0].ID, from.ConfigurationHash, matched[0].ConfigurationHash)
	}
	return &sinceSides{request: since, from: from, to: matched[0]}, nil
}

// graphChanges compares the bounded walks and the fingerprints of every symbol either one reaches.
func (pipeline *Pipeline) graphChanges(ctx context.Context, request graphBuild, built *graph.Graph, facts func(string) graphPackageFacts) (*GraphChanges, error) {
	sides := request.since
	previous, err := pipeline.previousGraph(ctx, request)
	if err != nil {
		return nil, err
	}
	symbols, err := pipeline.symbolChanges(ctx, sides)
	if err != nil {
		return nil, err
	}
	dependencies, err := pipeline.dependencyChanges(ctx, sides)
	if err != nil {
		return nil, err
	}
	var before *graph.Graph
	if previous != nil {
		before = previous.built
		markExcludedDependencies(dependencies, before.Omitted.Excluded, previous.source.packageFacts)
	}
	nodes := slices.Clone(built.Nodes)
	if before != nil {
		nodes = append(nodes, before.Nodes...)
	}
	handles, err := pipeline.symbolHandles(ctx, nodes)
	if err != nil {
		return nil, err
	}
	changes := &GraphChanges{
		Revision: sides.request.Revision, FromSnapshotID: sides.from.ID.String(), ToSnapshotID: sides.to.ID.String(),
		Before: before, Nodes: []GraphChangedNode{}, Edges: sides.edgeChanges(before, built),
		Symbols: map[GraphChangeReason]int{}, Dependencies: []GraphDependencyChange{},
	}
	for _, reason := range symbols {
		changes.Symbols[reason]++
	}
	for _, node := range built.Nodes {
		handle, indexed := handles[node.ID]
		if reason, changed := symbols[handle]; indexed && changed {
			changes.Nodes = append(changes.Nodes, GraphChangedNode{ID: node.ID, Reason: reason, SnapshotID: sides.to.ID.String()})
		} else if module := facts(node.Group).module; dependencies[module].Module != "" {
			changes.Nodes = append(changes.Nodes, GraphChangedNode{ID: node.ID, Reason: GraphChangeDependency, Module: module, SnapshotID: sides.to.ID.String()})
		}
	}
	changes.previousNodes(before, symbols, handles)
	changes.markPaths(built)
	markExcludedDependencies(dependencies, built.Omitted.Excluded, facts)
	for _, module := range slices.Sorted(maps.Keys(dependencies)) {
		changes.Dependencies = append(changes.Dependencies, dependencies[module])
	}
	changes.Changed = len(changes.Nodes)+len(changes.Edges) > 0 || slices.ContainsFunc(changes.Dependencies, func(change GraphDependencyChange) bool { return change.Excluded })
	return changes, nil
}

func (changes *GraphChanges) markPaths(built *graph.Graph) {
	ids := make([]string, 0, len(changes.Nodes))
	for _, node := range changes.Nodes {
		ids = append(ids, node.ID)
	}
	paths := built.Paths(ids)
	var beforePaths map[string][]string
	if changes.Before != nil {
		beforePaths = changes.Before.Paths(ids)
	}
	for index := range changes.Nodes {
		node := &changes.Nodes[index]
		if node.SnapshotID == changes.FromSnapshotID {
			node.Path = beforePaths[node.ID]
		} else {
			node.Path = paths[node.ID]
		}
	}
}

// markExcludedDependencies marks the changed dependencies reached through excluded packages.
func markExcludedDependencies(dependencies map[string]GraphDependencyChange, excluded map[string]int, facts func(string) graphPackageFacts) {
	for group := range excluded {
		if change, changed := dependencies[facts(group).module]; changed {
			change.Excluded = true
			dependencies[change.Module] = change
		}
	}
}

// symbolChanges classifies, by handle, every symbol of the root whose effective fingerprints differ
// between the two snapshots, naming the outermost layer that differs.
func (pipeline *Pipeline) symbolChanges(ctx context.Context, sides *sinceSides) (map[int64]GraphChangeReason, error) {
	before, err := storage.EffectiveSymbols(ctx, pipeline.database, sides.from.ID)
	if err != nil {
		return nil, err
	}
	after, err := storage.EffectiveSymbols(ctx, pipeline.database, sides.to.ID)
	if err != nil {
		return nil, err
	}
	old := make(map[int64]storage.EffectiveSymbol, len(before))
	for _, symbol := range before {
		old[symbol.Handle] = symbol
	}
	changes := map[int64]GraphChangeReason{}
	for _, symbol := range after {
		previous, found := old[symbol.Handle]
		delete(old, symbol.Handle)
		switch {
		case !found:
			changes[symbol.Handle] = GraphChangeAdded
		case *previous.PublicFP != *symbol.PublicFP:
			changes[symbol.Handle] = GraphChangeSignature
		case *previous.PrivateFP != *symbol.PrivateFP:
			changes[symbol.Handle] = GraphChangeStructure
		case *previous.ContentFP != *symbol.ContentFP:
			changes[symbol.Handle] = GraphChangeBody
		}
	}
	for handle := range old {
		changes[handle] = GraphChangeRemoved
	}
	return changes, nil
}

// symbolHandles maps the id of every indexed symbol among nodes to its handle.
func (pipeline *Pipeline) symbolHandles(ctx context.Context, nodes []graph.Node) (map[string]int64, error) {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if !node.Unresolved && !strings.HasPrefix(node.ID, packageScopePrefix) {
			ids = append(ids, node.ID)
		}
	}
	handles := make(map[string]int64, len(ids))
	for start := 0; start < len(ids); start += lookupBatch {
		var rows []storage.Symbol
		if err := pipeline.database.WithContext(ctx).Select("id", "handle").Where("id IN ?", ids[start:min(start+lookupBatch, len(ids))]).Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load the handles of graph symbols: %w", err)
		}
		for _, row := range rows {
			handles[row.ID] = row.Handle
		}
	}
	return handles, nil
}

func (pipeline *Pipeline) dependencyChanges(ctx context.Context, sides *sinceSides) (map[string]GraphDependencyChange, error) {
	before, err := storage.ModuleDependencies(ctx, pipeline.database, sides.from.ID)
	if err != nil {
		return nil, err
	}
	after, err := storage.ModuleDependencies(ctx, pipeline.database, sides.to.ID)
	if err != nil {
		return nil, err
	}
	return changedDependencies(before, after)
}

// changedDependencies are the dependencies whose selection differs between two snapshots, by module
// path. Two snapshots that captured no dependencies have none to compare; one that did and one that
// did not cannot be compared.
func changedDependencies(before, after storage.SnapshotDependenciesResult) (map[string]GraphDependencyChange, error) {
	if before.Captured != after.Captured {
		return nil, fmt.Errorf("one snapshot captured its dependencies (%t) and the other did not (%t), so dependency versions cannot be compared; reindex the older one", before.Captured, after.Captured)
	}
	selections := map[string]GraphDependencyChange{}
	for _, dependency := range before.Items {
		selections[dependency.ModulePath] = GraphDependencyChange{Module: dependency.ModulePath, From: dependencySelection(dependency)}
	}
	for _, dependency := range after.Items {
		change := selections[dependency.ModulePath]
		change.Module, change.To = dependency.ModulePath, dependencySelection(dependency)
		selections[dependency.ModulePath] = change
	}
	maps.DeleteFunc(selections, func(_ string, change GraphDependencyChange) bool { return change.From == change.To })
	return selections, nil
}

// dependencySelection is what a snapshot built a dependency from: its selected version (the declared
// one when none was recorded), its replacement, and the snapshot a local replacement resolved to.
func dependencySelection(dependency storage.SnapshotDependency) string {
	selection := dependency.SelectedVersion
	if selection == "" {
		selection = dependency.DeclaredVersion
	}
	if dependency.ReplacePath != "" {
		selection += " => " + strings.TrimSpace(dependency.ReplacePath+" "+dependency.ReplaceVersion)
	}
	if dependency.TargetSnapshotID != nil {
		selection += " @" + dependency.TargetSnapshotID.String()
	}
	return selection
}

// sinceStage summarises a change query.
func sinceStage(changes *GraphChanges, built *graph.Graph) ResolutionStage {
	symbols := 0
	for _, count := range changes.Symbols {
		symbols += count
	}
	value := fmt.Sprintf("%s (snapshot %s): %d changed symbols, %d changed dependencies, %d changed nodes reached, %d added or removed edges",
		changes.Revision, changes.FromSnapshotID, symbols, len(changes.Dependencies), len(changes.Nodes), len(changes.Edges))
	limited := built.Omitted.BeyondDepth > 0 || built.Omitted.NodeLimit
	if changes.Before != nil {
		limited = limited || changes.Before.Omitted.BeyondDepth > 0 || changes.Before.Omitted.NodeLimit
	}
	if limited {
		value += "; a walk stopped at its depth or node limit, so changes past it are not reported"
	}
	return ResolutionStage{Name: "since", Value: value}
}
