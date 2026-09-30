package indexer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type versionedGraphNode struct {
	key      string
	prepared preparedRevision
}

type versionedDependencyGraph struct {
	nodes []versionedGraphNode
	byKey map[string]int
}

func versionedKey(modulePath, version string) string { return modulePath + "@" + version }

func (indexer *Indexer) discoverVersionedGraph(ctx context.Context, modulePath, version string, includeTests bool) (versionedDependencyGraph, error) {
	graph := versionedDependencyGraph{byKey: map[string]int{}}
	if err := indexer.addVersionedNode(ctx, &graph, modulePath, version, includeTests); err != nil {
		return versionedDependencyGraph{}, errors.Join(err, graph.close())
	}
	for index := 0; index < len(graph.nodes); index++ {
		for dependencyIndex := range graph.nodes[index].prepared.root.Dependencies {
			dependency := &graph.nodes[index].prepared.root.Dependencies[dependencyIndex]
			if dependency.TargetModulePath == "" || dependency.TargetVersion == "" || dependency.LocalDir != "" {
				continue
			}
			if _, found := graph.byKey[versionedKey(dependency.TargetModulePath, dependency.TargetVersion)]; found {
				continue
			}
			if err := indexer.addVersionedNode(ctx, &graph, dependency.TargetModulePath, dependency.TargetVersion, includeTests); err != nil {
				if !errors.Is(err, errVersionUnavailable) {
					return versionedDependencyGraph{}, errors.Join(err, graph.close())
				}
				dependency.Edge.UnresolvedReason = err.Error()
			}
		}
	}
	return graph, nil
}

func (indexer *Indexer) addVersionedNode(ctx context.Context, graph *versionedDependencyGraph, modulePath, version string, includeTests bool) error {
	key := versionedKey(modulePath, version)
	if _, found := graph.byKey[key]; found {
		return nil
	}
	var locations []storage.ModuleLocation
	if err := indexer.database.WithContext(ctx).Table("locations AS location").Select("location.*").
		Joins("JOIN modules AS root ON root.id = location.root_id").Where("root.root_key = ?", modulePath).
		Order("location.canonical_path").Find(&locations).Error; err != nil {
		return fmt.Errorf("find checkouts for %s: %w", key, err)
	}
	for _, location := range locations {
		commit, err := versionCommit(ctx, location.CanonicalPath, version)
		if err != nil {
			continue
		}
		prepared, err := indexer.prepareRevision(ctx, RevisionOptions{
			RootKey: modulePath, Checkout: location.CanonicalPath, Commit: commit, Version: version, IncludeTests: includeTests,
		})
		if err != nil {
			return fmt.Errorf("prepare version %s from %q: %w", key, location.CanonicalPath, err)
		}
		graph.byKey[key] = len(graph.nodes)
		graph.nodes = append(graph.nodes, versionedGraphNode{key: key, prepared: prepared})
		return nil
	}
	return fmt.Errorf("%w: no registered checkout contains %s", errVersionUnavailable, key)
}

func (graph versionedDependencyGraph) close() error {
	var err error
	for _, node := range graph.nodes {
		err = errors.Join(err, node.prepared.cleanup())
	}
	return err
}

func (graph versionedDependencyGraph) components() [][]int {
	return dependencyComponents(len(graph.nodes), func(node int) []int {
		var targets []int
		for _, dependency := range graph.nodes[node].prepared.root.Dependencies {
			if dependency.TargetVersion == "" {
				continue
			}
			if target, found := graph.byKey[versionedKey(dependency.TargetModulePath, dependency.TargetVersion)]; found {
				targets = append(targets, target)
			}
		}
		return targets
	})
}

func (graph versionedDependencyGraph) cyclic() bool {
	for _, group := range graph.components() {
		if len(group) > 1 {
			return true
		}
		for _, dependency := range graph.nodes[group[0]].prepared.root.Dependencies {
			if dependency.TargetVersion != "" && versionedKey(dependency.TargetModulePath, dependency.TargetVersion) == graph.nodes[group[0]].key {
				return true
			}
		}
	}
	return false
}

func (indexer *Indexer) publishVersionedComponents(ctx context.Context, graph versionedDependencyGraph, includeTests bool) (storage.ModuleSnapshot, error) {
	var rootSnapshot storage.ModuleSnapshot
	for _, group := range graph.components() {
		component := versionedDependencyGraph{byKey: map[string]int{}}
		for _, index := range group {
			if index == 0 {
				component.nodes = append(component.nodes, graph.nodes[index])
			}
		}
		for _, index := range group {
			if index != 0 {
				component.nodes = append(component.nodes, graph.nodes[index])
			}
		}
		for index, node := range component.nodes {
			component.byKey[node.key] = index
		}
		stored, same, err := indexer.reuseVersionedGraph(ctx, component, includeTests)
		if err != nil {
			return storage.ModuleSnapshot{}, err
		}
		if !same {
			stored, err = indexer.publishVersionedGraph(ctx, component, includeTests)
			if err != nil {
				return storage.ModuleSnapshot{}, err
			}
		}
		if _, containsRoot := component.byKey[graph.nodes[0].key]; containsRoot {
			rootSnapshot = stored
		}
	}
	return rootSnapshot, nil
}

func (indexer *Indexer) publishVersionedGraph(ctx context.Context, graph versionedDependencyGraph, includeTests bool) (storage.ModuleSnapshot, error) {
	ids := make([]uuid.UUID, len(graph.nodes))
	extractions := make([]moduleExtraction, len(graph.nodes))
	for index := range graph.nodes {
		ids[index] = uuid.New()
	}
	if err := indexer.setVersionedTargets(ctx, &graph, ids); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	for index := range graph.nodes {
		root := &graph.nodes[index].prepared.root
		extraction, err := extractModule(ctx, indexer.loadPackages, *root, includeTests)
		if err != nil {
			return storage.ModuleSnapshot{}, fmt.Errorf("extract version %s: %w", graph.nodes[index].key, err)
		}
		extractions[index] = extraction
	}
	var first storage.ModuleSnapshot
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
			snapshot, err := publishSnapshot(ctx, transaction, snapshotPublication{
				root: stored, location: node.prepared.location, base: base, extraction: extractions[index],
				startedAt: time.Now().UTC(), preserveHead: true, snapshotID: ids[index], pendingEdges: &pending,
			}, &result)
			if err != nil {
				return err
			}
			if index == 0 {
				first = snapshot
			}
		}
		for _, edge := range pending {
			if err := transaction.WithContext(ctx).Create(&edge).Error; err != nil {
				return fmt.Errorf("publish dependency %q of snapshot %s: %w", edge.ModulePath, edge.SnapshotID, err)
			}
		}
		return nil
	})
	return first, err
}

func (indexer *Indexer) setVersionedTargets(ctx context.Context, graph *versionedDependencyGraph, ids []uuid.UUID) error {
	for index := range graph.nodes {
		for dependencyIndex := range graph.nodes[index].prepared.root.Dependencies {
			dependency := &graph.nodes[index].prepared.root.Dependencies[dependencyIndex]
			if dependency.TargetVersion == "" || dependency.TargetModulePath == "" {
				continue
			}
			if target, found := graph.byKey[versionedKey(dependency.TargetModulePath, dependency.TargetVersion)]; found {
				id := ids[target]
				dependency.Edge.TargetSnapshotID = &id
				dependency.Edge.UnresolvedReason = ""
				continue
			}
			snapshot, found, err := indexer.storedVersionSnapshot(ctx, dependency.TargetModulePath, dependency.TargetVersion)
			if err != nil {
				return err
			}
			if found {
				dependency.Edge.TargetSnapshotID = &snapshot.ID
				dependency.Edge.UnresolvedReason = ""
			}
		}
	}
	return nil
}

func (indexer *Indexer) reuseVersionedGraph(ctx context.Context, graph versionedDependencyGraph, includeTests bool) (storage.ModuleSnapshot, bool, error) {
	ids := make([]uuid.UUID, len(graph.nodes))
	existing := make([]storage.ModuleSnapshot, len(graph.nodes))
	for index, node := range graph.nodes {
		snapshot, found, err := indexer.storedVersionSnapshot(ctx, node.prepared.root.RootKey, node.prepared.root.ModuleVersion)
		if err != nil || !found {
			return storage.ModuleSnapshot{}, false, err
		}
		ids[index], existing[index] = snapshot.ID, snapshot
	}
	if err := indexer.setVersionedTargets(ctx, &graph, ids); err != nil {
		return storage.ModuleSnapshot{}, false, err
	}
	for index, node := range graph.nodes {
		root, stored := node.prepared.root, existing[index]
		if stored.GitCommit != root.GitCommit || stored.ContentSetHash != root.ContentSetHash || stored.ConfigurationHash != root.ConfigurationHash || stored.DependencySetHash == nil || *stored.DependencySetHash != dependencyHash(root.Dependencies) {
			return storage.ModuleSnapshot{}, false, nil
		}
		extraction, err := extractModule(ctx, indexer.loadPackages, root, includeTests)
		if err != nil {
			return storage.ModuleSnapshot{}, false, fmt.Errorf("extract version %s: %w", node.key, err)
		}
		if stored.ContextHash != extraction.contextHash {
			return storage.ModuleSnapshot{}, false, nil
		}
	}
	return existing[0], true, nil
}
