package indexer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type versionedGraphNode struct {
	key      string
	options  RevisionOptions
	prepared preparedRevision
}

func (node versionedGraphNode) memoKey() versionMemoKey {
	return versionMemoKey{modulePath: node.options.RootKey, version: node.options.Version, includeTests: node.options.IncludeTests}
}

// versionedDependencyGraph is every version a run still has to index, each checked out and discovered
// once, with the dependency edges between them.
type versionedDependencyGraph struct {
	nodes []versionedGraphNode
	byKey map[string]int
}

func versionedKey(modulePath, version string) string { return modulePath + "@" + version }

// discoverVersionedGraph prepares every version of keys and of their closures that the run has not
// resolved, skipping, and remembering, a version whose stored snapshot's closure is still complete or
// that no registered checkout has.
func (indexer *Indexer) discoverVersionedGraph(ctx context.Context, memo *runMemo, keys []versionMemoKey, includeTests bool) (versionedDependencyGraph, error) {
	graph := versionedDependencyGraph{byKey: map[string]int{}}
	for _, key := range keys {
		if err := indexer.expandVersionedNode(ctx, memo, &graph, key); err != nil {
			return versionedDependencyGraph{}, errors.Join(err, graph.close())
		}
	}
	for index := 0; index < len(graph.nodes); index++ {
		for _, dependency := range graph.nodes[index].prepared.root.Dependencies {
			key, versioned := versionedRequirement(dependency, includeTests)
			if !versioned {
				continue
			}
			if err := indexer.expandVersionedNode(ctx, memo, &graph, key); err != nil {
				return versionedDependencyGraph{}, errors.Join(err, graph.close())
			}
		}
	}
	return graph, nil
}

// expandVersionedNode adds the version key names to graph, unless the run already resolved it or its
// stored snapshot needs no index, in which case memo records that snapshot, or no registered checkout
// has it, in which case memo records the newest stored snapshot or that none is available.
func (indexer *Indexer) expandVersionedNode(ctx context.Context, memo *runMemo, graph *versionedDependencyGraph, key versionMemoKey) error {
	if _, found := graph.byKey[versionedKey(key.modulePath, key.version)]; found {
		return nil
	}
	if _, found := memo.version(key); found {
		return nil
	}
	stored, found, err := indexer.storedVersionSnapshot(ctx, key.modulePath, key.version)
	if err != nil {
		return err
	}
	if found {
		miss, err := indexer.storedVersionMiss(ctx, memo, stored)
		if err != nil {
			return err
		}
		if miss == "" {
			memo.rememberVersion(key, versionOutcome{snapshot: stored})
			return nil
		}
		logVersionedMiss(ctx, versionedKey(key.modulePath, key.version), miss)
	}
	err = indexer.addVersionedNode(ctx, memo, graph, key)
	switch {
	case !errors.Is(err, errVersionUnavailable):
		return err
	case found:
		memo.rememberVersion(key, versionOutcome{snapshot: stored})
	default:
		memo.rememberVersion(key, versionOutcome{unavailable: err})
	}
	return nil
}

// storedVersionMiss is why a version's stored snapshot cannot stand without indexing the version
// again, or "" when it can: its coverage is one extracting again cannot improve and its closure is
// complete.
func (indexer *Indexer) storedVersionMiss(ctx context.Context, memo *runMemo, stored storage.ModuleSnapshot) (string, error) {
	if !stored.Coverage.Complete() {
		return fmt.Sprintf("snapshot %s has %s coverage", stored.ID, stored.Coverage), nil
	}
	return indexer.versionedClosureMiss(ctx, memo, stored.ID)
}

// logVersionedMiss counts a stored version snapshot the run indexes again in the running phase and
// logs the version and why.
func logVersionedMiss(ctx context.Context, name, miss string) {
	PhaseCount(ctx, "versioned-stale", 1)
	if log, found := ctx.Value(phaseLogKey{}).(phaseLog); found {
		log.Infof("versioned-stale: %s: %s", name, miss)
	}
}

// versionNotRegistered is the unavailable outcome of a version no registered checkout has. Its text
// becomes the edge's unresolved reason, which the dependency set hash covers, so every resolver must
// record this exact error.
func versionNotRegistered(name string) error {
	return fmt.Errorf("%w: no registered checkout contains %s", errVersionUnavailable, name)
}

func (indexer *Indexer) addVersionedNode(ctx context.Context, memo *runMemo, graph *versionedDependencyGraph, key versionMemoKey) error {
	name := versionedKey(key.modulePath, key.version)
	tag, err := indexer.versionTag(ctx, memo, key.modulePath, key.version)
	if err != nil {
		return err
	}
	if !tag.found {
		return versionNotRegistered(name)
	}
	options := RevisionOptions{
		RootKey: key.modulePath, Checkout: tag.checkout, Commit: tag.commit, Version: key.version, IncludeTests: key.includeTests,
		Reason: storage.ReasonVersionedDependency,
	}
	prepared, err := indexer.prepareRevision(ctx, options)
	if err != nil {
		return fmt.Errorf("prepare version %s from %q: %w", name, tag.checkout, err)
	}
	graph.byKey[name] = len(graph.nodes)
	graph.nodes = append(graph.nodes, versionedGraphNode{key: name, options: options, prepared: prepared})
	addProgress(ctx, 1, 0)
	return nil
}

func (graph versionedDependencyGraph) close() error {
	var err error
	for _, node := range graph.nodes {
		err = errors.Join(err, node.prepared.cleanup())
	}
	return err
}

// targets are the nodes of graph that node depends on.
func (graph versionedDependencyGraph) targets(node int) []int {
	var targets []int
	for _, dependency := range graph.nodes[node].prepared.root.Dependencies {
		key, versioned := versionedRequirement(dependency, graph.nodes[node].options.IncludeTests)
		if !versioned {
			continue
		}
		if target, found := graph.byKey[versionedKey(key.modulePath, key.version)]; found {
			targets = append(targets, target)
		}
	}
	return targets
}

// components are graph's strongly connected components, every component after those it depends on.
func (graph versionedDependencyGraph) components() []versionedDependencyGraph {
	groups := dependencyComponents(len(graph.nodes), graph.targets)
	components := make([]versionedDependencyGraph, 0, len(groups))
	for _, group := range groups {
		component := versionedDependencyGraph{byKey: make(map[string]int, len(group))}
		for _, index := range group {
			component.byKey[graph.nodes[index].key] = len(component.nodes)
			component.nodes = append(component.nodes, graph.nodes[index])
		}
		components = append(components, component)
	}
	return components
}

// cyclic reports whether a component is a cycle: several versions, or one that depends on itself.
func (graph versionedDependencyGraph) cyclic() bool {
	return len(graph.nodes) > 1 || len(graph.targets(0)) > 0
}

func (graph versionedDependencyGraph) name() string {
	keys := make([]string, 0, len(graph.nodes))
	for _, node := range graph.nodes {
		keys = append(keys, node.key)
	}
	return strings.Join(keys, " + ")
}

// resolveVersionedGraph indexes graph one component at a time, dependencies first, each as a phase
// named by its versions, and records every version's snapshot in memo.
func (indexer *Indexer) resolveVersionedGraph(ctx context.Context, memo *runMemo, graph versionedDependencyGraph) error {
	for _, component := range graph.components() {
		if err := Phase(ctx, component.name(), func(ctx context.Context) error {
			snapshots, err := indexer.versionedComponentSnapshots(ctx, memo, component)
			if err != nil {
				return err
			}
			for index, node := range component.nodes {
				memo.rememberVersion(node.memoKey(), versionOutcome{snapshot: snapshots[index]})
			}
			return nil
		}); err != nil {
			return err
		}
		addProgress(ctx, 0, len(component.nodes))
	}
	return nil
}

// versionedComponentSnapshots indexes one component: a cycle is reused or published together, and a
// single version is indexed from the worktree the graph already prepared for it.
func (indexer *Indexer) versionedComponentSnapshots(ctx context.Context, memo *runMemo, component versionedDependencyGraph) ([]storage.ModuleSnapshot, error) {
	if component.cyclic() {
		stored, same, err := indexer.reuseVersionedGraph(ctx, memo, component)
		if err != nil || same {
			return stored, err
		}
		return indexer.publishVersionedGraph(ctx, memo, component)
	}
	node := component.nodes[0]
	result, err := indexer.indexPreparedRevision(ctx, node.prepared, node.options)
	if err != nil {
		return nil, fmt.Errorf("index version %s from %q: %w", node.key, node.options.Checkout, err)
	}
	var indexed storage.ModuleSnapshot
	if err := indexer.database.WithContext(ctx).Where("id = ?", result.SnapshotID).Take(&indexed).Error; err != nil {
		return nil, fmt.Errorf("load version snapshot %s: %w", result.SnapshotID, err)
	}
	return []storage.ModuleSnapshot{indexed}, nil
}

func (indexer *Indexer) publishVersionedGraph(ctx context.Context, memo *runMemo, graph versionedDependencyGraph) ([]storage.ModuleSnapshot, error) {
	ids := make([]uuid.UUID, len(graph.nodes))
	extractions := make([]moduleExtraction, len(graph.nodes))
	for index := range graph.nodes {
		ids[index] = uuid.New()
	}
	if err := setVersionedTargets(memo, &graph, ids); err != nil {
		return nil, err
	}
	for index, node := range graph.nodes {
		extraction, err := extractModule(ctx, extractOptions{loadPackages: indexer.loadPackages, root: node.prepared.root, includeTests: node.options.IncludeTests})
		if err != nil {
			return nil, fmt.Errorf("extract version %s: %w", node.key, err)
		}
		extractions[index] = extraction
	}
	snapshots := make([]storage.ModuleSnapshot, len(graph.nodes))
	err := storage.RetryAllocationConflicts(ctx, indexer.database, func(transaction *gorm.DB) error {
		pending := []storage.SnapshotDependency{}
		for index, node := range graph.nodes {
			var stored storage.ModuleRoot
			if err := transaction.WithContext(ctx).Where("id = ?", node.prepared.location.RootID).Take(&stored).Error; err != nil {
				return fmt.Errorf("load root of %s: %w", node.key, err)
			}
			base, err := loadModuleBase(ctx, transaction, stored, node.prepared.location)
			if err != nil {
				return err
			}
			result := ModuleResult{RootKey: stored.RootKey, Location: node.prepared.location.CanonicalPath, Files: len(node.prepared.root.Files)}
			snapshots[index], err = publishSnapshot(ctx, transaction, snapshotPublication{
				root: stored, location: node.prepared.location, base: base, extraction: extractions[index],
				startedAt: time.Now().UTC(), reason: storage.ReasonVersionedDependency, preserveHead: true, snapshotID: ids[index], pendingEdges: &pending,
			}, &result)
			if err != nil {
				return err
			}
		}
		for _, edge := range pending {
			if err := transaction.WithContext(ctx).Create(&edge).Error; err != nil {
				return fmt.Errorf("publish dependency %q of snapshot %s: %w", edge.ModulePath, edge.SnapshotID, err)
			}
		}
		return nil
	})
	return snapshots, err
}

// setVersionedTargets points each edge of graph at ids for a version inside graph, and at the snapshot
// the run resolved for any other version.
func setVersionedTargets(memo *runMemo, graph *versionedDependencyGraph, ids []uuid.UUID) error {
	for index := range graph.nodes {
		includeTests := graph.nodes[index].options.IncludeTests
		for dependencyIndex := range graph.nodes[index].prepared.root.Dependencies {
			dependency := &graph.nodes[index].prepared.root.Dependencies[dependencyIndex]
			key, versioned := versionedRequirement(*dependency, includeTests)
			if !versioned {
				continue
			}
			if target, found := graph.byKey[versionedKey(key.modulePath, key.version)]; found {
				id := ids[target]
				dependency.Edge.TargetSnapshotID = &id
				dependency.Edge.UnresolvedReason = ""
				continue
			}
			if err := linkVersionedDependency(memo, dependency, includeTests); err != nil {
				return err
			}
		}
	}
	return nil
}

func (indexer *Indexer) reuseVersionedGraph(ctx context.Context, memo *runMemo, graph versionedDependencyGraph) ([]storage.ModuleSnapshot, bool, error) {
	ids := make([]uuid.UUID, len(graph.nodes))
	existing := make([]storage.ModuleSnapshot, len(graph.nodes))
	for index, node := range graph.nodes {
		snapshot, found, err := indexer.storedVersionSnapshot(ctx, node.prepared.root.RootKey, node.prepared.root.ModuleVersion)
		if err != nil || !found {
			return nil, false, err
		}
		ids[index], existing[index] = snapshot.ID, snapshot
	}
	if err := setVersionedTargets(memo, &graph, ids); err != nil {
		return nil, false, err
	}
	for index, node := range graph.nodes {
		root, stored := node.prepared.root, existing[index]
		if stored.GitCommit != root.GitCommit || stored.ContentSetHash != root.ContentSetHash || stored.ConfigurationHash != root.ConfigurationHash || stored.DependencySetHash == nil || *stored.DependencySetHash != dependencyHash(root.Dependencies) {
			return nil, false, nil
		}
		extraction, err := extractModule(ctx, extractOptions{loadPackages: indexer.loadPackages, root: root, includeTests: node.options.IncludeTests})
		if err != nil {
			return nil, false, fmt.Errorf("extract version %s: %w", node.key, err)
		}
		if stored.ContextHash != extraction.contextHash || stored.Coverage != extraction.coverage {
			return nil, false, nil
		}
	}
	return existing, true, nil
}
