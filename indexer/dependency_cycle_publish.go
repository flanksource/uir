package indexer

import (
	"context"
	"fmt"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (indexer *Indexer) indexLocalCycleGraph(ctx context.Context, graph localDependencyGraph, requested int, options ModuleOptions) ([]ModuleResult, error) {
	if err := indexer.indexVersionedDependencies(ctx, graph.roots, options.IncludeTests); err != nil {
		return nil, err
	}
	extractions := make(map[int]moduleExtraction, len(graph.roots))
	for index, root := range graph.roots {
		extraction, err := extractModule(ctx, indexer.loadPackages, root, options.IncludeTests)
		if err != nil {
			return nil, fmt.Errorf("extract module %q: %w", root.RootKey, err)
		}
		extractions[index] = extraction
	}
	results := make(map[int]ModuleResult, len(graph.roots))
	snapshots := make(map[int]storage.ModuleSnapshot, len(graph.roots))
	for _, group := range graph.groups {
		groupOptions := options
		groupOptions.Force = false
		for _, index := range group {
			groupOptions.Force = groupOptions.Force || options.Force && index < requested
		}
		if err := indexer.resolveLocalGroup(ctx, &graph, group, snapshots); err != nil {
			return nil, err
		}
		if err := indexer.publishLocalGroup(ctx, graph, group, extractions, groupOptions, results, snapshots); err != nil {
			return nil, err
		}
	}
	selected := make([]ModuleResult, requested)
	for index := range selected {
		selected[index] = results[index]
	}
	return selected, nil
}

func (indexer *Indexer) resolveLocalGroup(ctx context.Context, graph *localDependencyGraph, group []int, snapshots map[int]storage.ModuleSnapshot) error {
	members := make(map[int]bool, len(group))
	for _, index := range group {
		members[index] = true
	}
	for _, index := range group {
		root := &graph.roots[index]
		for dependencyIndex := range root.Dependencies {
			dependency := &root.Dependencies[dependencyIndex]
			targetIndex, found := graph.byPath[dependency.LocalDir]
			if !found || members[targetIndex] {
				continue
			}
			target, ready := snapshots[targetIndex]
			if !ready {
				return fmt.Errorf("local dependency %q was not published before %q", dependency.LocalDir, root.RootKey)
			}
			dependency.Edge.TargetSnapshotID = &target.ID
			dependency.Edge.UnresolvedReason = ""
			if root.GitCommit != "" && target.WorktreeState != storage.WorktreeClean {
				root.WorktreeState = storage.WorktreeDirty
				if target.LastModifiedAt != nil {
					root.LastModifiedAt = maxTime(root.LastModifiedAt, *target.LastModifiedAt)
				}
				root.Revision = dirtyRevision(root.GitCommit, root.LastModifiedAt)
			}
		}
	}
	var latest time.Time
	dirty := false
	for _, index := range group {
		root := graph.roots[index]
		dirty = dirty || root.WorktreeState == storage.WorktreeDirty
		latest = maxTime(latest, root.LastModifiedAt)
	}
	if dirty {
		for _, index := range group {
			root := &graph.roots[index]
			if root.GitCommit == "" {
				continue
			}
			root.WorktreeState = storage.WorktreeDirty
			root.LastModifiedAt = latest
			root.Revision = dirtyRevision(root.GitCommit, latest)
		}
	}
	return nil
}

func (indexer *Indexer) publishLocalGroup(ctx context.Context, graph localDependencyGraph, group []int, extractions map[int]moduleExtraction, options ModuleOptions, results map[int]ModuleResult, snapshots map[int]storage.ModuleSnapshot) error {
	heads := make(map[int]storage.ModuleSnapshot, len(group))
	reusable := !options.Force
	for _, index := range group {
		head, found, err := loadHead(ctx, indexer.database, graph.roots[index])
		if err != nil {
			return err
		}
		if !found {
			reusable = false
		}
		heads[index] = head
	}
	for _, index := range group {
		for dependencyIndex := range graph.roots[index].Dependencies {
			dependency := &graph.roots[index].Dependencies[dependencyIndex]
			if targetIndex, linked := graph.byPath[dependency.LocalDir]; linked && containsIndex(group, targetIndex) {
				if heads[targetIndex].ID != uuid.Nil {
					target := heads[targetIndex].ID
					dependency.Edge.TargetSnapshotID = &target
					dependency.Edge.UnresolvedReason = ""
				}
			}
		}
	}
	if reusable {
		for _, index := range group {
			root, head := graph.roots[index], heads[index]
			if head.Revision != root.Revision || head.WorktreeState != root.WorktreeState || head.ContentSetHash != root.ContentSetHash ||
				head.ConfigurationHash != root.ConfigurationHash || head.ContextHash != extractions[index].contextHash ||
				head.DependencySetHash == nil || *head.DependencySetHash != dependencyHash(root.Dependencies) {
				reusable = false
				break
			}
		}
	}
	if reusable {
		return indexer.reuseLocalGroup(ctx, graph, group, extractions, heads, options, results, snapshots)
	}
	return indexer.createLocalGroup(ctx, graph, group, extractions, options, results, snapshots)
}

func (indexer *Indexer) reuseLocalGroup(ctx context.Context, graph localDependencyGraph, group []int, extractions map[int]moduleExtraction, heads map[int]storage.ModuleSnapshot, options ModuleOptions, results map[int]ModuleResult, snapshots map[int]storage.ModuleSnapshot) error {
	err := storage.RetryAllocationConflicts(ctx, indexer.database, func(transaction *gorm.DB) error {
		if err := verifyIndexInputs(ctx, transaction, groupInputs(graph, group), options.IncludeTests); err != nil {
			return err
		}
		locations := map[string]storage.ModuleLocation{}
		for _, index := range group {
			extraction := extractions[index]
			extraction.root = graph.roots[index]
			extraction.reusedHead = heads[index].ID
			if _, _, err := ensureGraphLocation(ctx, transaction, graph, index, locations); err != nil {
				return err
			}
			result, location, err := indexModule(ctx, transaction, extraction, locations, ModuleOptions{Reason: storage.ReasonDependencyCycle})
			if err != nil {
				return err
			}
			locations[extraction.root.LocalPath] = location
			results[index] = result
		}
		return nil
	})
	if err == nil {
		for _, index := range group {
			snapshots[index] = heads[index]
		}
	}
	return err
}

func (indexer *Indexer) createLocalGroup(ctx context.Context, graph localDependencyGraph, group []int, extractions map[int]moduleExtraction, options ModuleOptions, results map[int]ModuleResult, snapshots map[int]storage.ModuleSnapshot) error {
	ids := make(map[int]uuid.UUID, len(group))
	for _, index := range group {
		ids[index] = uuid.New()
	}
	for _, index := range group {
		for dependencyIndex := range graph.roots[index].Dependencies {
			dependency := &graph.roots[index].Dependencies[dependencyIndex]
			if targetIndex, linked := graph.byPath[dependency.LocalDir]; linked && containsIndex(group, targetIndex) {
				target := ids[targetIndex]
				dependency.Edge.TargetSnapshotID = &target
				dependency.Edge.UnresolvedReason = ""
			}
		}
	}
	return storage.RetryAllocationConflicts(ctx, indexer.database, func(transaction *gorm.DB) error {
		if err := verifyIndexInputs(ctx, transaction, groupInputs(graph, group), options.IncludeTests); err != nil {
			return err
		}
		locations := map[string]storage.ModuleLocation{}
		pending := []storage.SnapshotDependency{}
		for _, index := range group {
			extraction := extractions[index]
			extraction.root = graph.roots[index]
			root, location, err := ensureGraphLocation(ctx, transaction, graph, index, locations)
			if err != nil {
				return err
			}
			locations[extraction.root.LocalPath] = location
			base, err := loadModuleBase(ctx, transaction, root, location)
			if err != nil {
				return err
			}
			result := ModuleResult{RootKey: root.RootKey, Location: location.CanonicalPath, Files: len(extraction.root.Files)}
			snapshot, err := publishSnapshot(ctx, transaction, snapshotPublication{
				root: root, location: location, base: base, extraction: extraction, force: options.Force,
				startedAt: time.Now().UTC(), reason: storage.ReasonDependencyCycle, snapshotID: ids[index], pendingEdges: &pending,
			}, &result)
			if err != nil {
				return err
			}
			result.SnapshotID = snapshot.ID.String()
			results[index], snapshots[index] = result, snapshot
		}
		for _, edge := range pending {
			if err := transaction.WithContext(ctx).Create(&edge).Error; err != nil {
				return fmt.Errorf("publish dependency %q of snapshot %s: %w", edge.ModulePath, edge.SnapshotID, err)
			}
		}
		return nil
	})
}

func ensureGraphLocation(ctx context.Context, database *gorm.DB, graph localDependencyGraph, index int, locations map[string]storage.ModuleLocation) (storage.ModuleRoot, storage.ModuleLocation, error) {
	root := graph.roots[index]
	if root.ParentRootKey != "" {
		if _, found := locations[root.ParentRootKey]; !found {
			parent, exists := graph.byPath[root.ParentRootKey]
			if !exists {
				return storage.ModuleRoot{}, storage.ModuleLocation{}, fmt.Errorf("parent module location %q is missing from dependency graph", root.ParentRootKey)
			}
			if _, _, err := ensureGraphLocation(ctx, database, graph, parent, locations); err != nil {
				return storage.ModuleRoot{}, storage.ModuleLocation{}, err
			}
		}
	}
	stored, location, err := ensureModuleLocation(ctx, database, root, locations)
	if err == nil {
		locations[root.LocalPath] = location
	}
	return stored, location, err
}

func containsIndex(group []int, target int) bool {
	for _, index := range group {
		if index == target {
			return true
		}
	}
	return false
}

// groupInputs is what verifyIndexInputs checks for a cycle group: its roots, with the edges between
// members unlinked, since those name the snapshots the group's own publication creates.
func groupInputs(graph localDependencyGraph, group []int) []discoveredRoot {
	selected := make([]discoveredRoot, 0, len(group))
	for _, index := range group {
		root := graph.roots[index]
		root.Dependencies = append([]dependencyObservation(nil), root.Dependencies...)
		for dependencyIndex := range root.Dependencies {
			if targetIndex, linked := graph.byPath[root.Dependencies[dependencyIndex].LocalDir]; linked && containsIndex(group, targetIndex) {
				root.Dependencies[dependencyIndex].Edge.TargetSnapshotID = nil
			}
		}
		selected = append(selected, root)
	}
	return selected
}
