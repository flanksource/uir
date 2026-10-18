package indexer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (indexer *Indexer) indexLocalCycleGraph(ctx context.Context, graph localDependencyGraph, requested int, options ModuleOptions) ([]ModuleResult, error) {
	if err := indexer.indexVersionedDependencies(ctx, graph.roots, options.IncludeTests); err != nil {
		return nil, err
	}
	ctx, memo := ensureRunMemo(ctx)
	check := newHeadCheck(indexer, memo, options.IncludeTests)
	addProgress(ctx, len(graph.roots), 0)
	results := make(map[int]ModuleResult, len(graph.roots))
	snapshots := make(map[int]storage.ModuleSnapshot, len(graph.roots))
	for _, group := range graph.groups {
		groupOptions := options
		groupOptions.Force = false
		for _, index := range group {
			groupOptions.Force = groupOptions.Force || options.Force && index < requested
		}
		if err := Phase(ctx, graph.groupName(group), func(ctx context.Context) error {
			return indexer.indexLocalGroup(ctx, check, &graph, group, groupOptions, results, snapshots)
		}); err != nil {
			return nil, err
		}
		addProgress(ctx, 0, len(group))
	}
	selected := make([]ModuleResult, requested)
	for index := range selected {
		selected[index] = results[index]
	}
	return selected, nil
}

// indexLocalGroup takes a group the run already indexed from memo, or else links it to the earlier
// groups' snapshots, publishes or reuses it, and remembers every member in memo.
func (indexer *Indexer) indexLocalGroup(ctx context.Context, check *headCheck, graph *localDependencyGraph, group []int, options ModuleOptions, results map[int]ModuleResult, snapshots map[int]storage.ModuleSnapshot) error {
	remembered, err := indexer.rememberedLocalGroup(ctx, check.memo, *graph, group, options, results, snapshots)
	if err != nil || remembered {
		return err
	}
	if err := indexer.resolveLocalGroup(ctx, graph, group, snapshots); err != nil {
		return err
	}
	if err := indexer.publishLocalGroup(ctx, check, *graph, group, options, results, snapshots); err != nil {
		return err
	}
	indexed := make([]ModuleResult, 0, len(group))
	for _, index := range group {
		indexed = append(indexed, results[index])
	}
	check.memo.rememberModules(indexed, options.IncludeTests)
	return nil
}

// rememberedLocalGroup takes every member's result and snapshot from memo when the run already indexed
// the whole group and it is not forced, and reports whether it did.
func (indexer *Indexer) rememberedLocalGroup(ctx context.Context, memo *runMemo, graph localDependencyGraph, group []int, options ModuleOptions, results map[int]ModuleResult, snapshots map[int]storage.ModuleSnapshot) (bool, error) {
	if options.Force {
		return false, nil
	}
	remembered := make(map[int]ModuleResult, len(group))
	for _, index := range group {
		result, found := memo.module(graph.roots[index].LocalPath, options.IncludeTests)
		if !found {
			return false, nil
		}
		remembered[index] = result
	}
	for index, result := range remembered {
		var snapshot storage.ModuleSnapshot
		if err := indexer.database.WithContext(ctx).Where("id = ?", uuid.MustParse(result.SnapshotID)).Take(&snapshot).Error; err != nil {
			return false, fmt.Errorf("load remembered snapshot %s of %q: %w", result.SnapshotID, graph.roots[index].RootKey, err)
		}
		results[index], snapshots[index] = result, snapshot
	}
	return true, nil
}

// groupName names a group by its members' root keys.
func (graph localDependencyGraph) groupName(group []int) string {
	keys := make([]string, 0, len(group))
	for _, index := range group {
		keys = append(keys, graph.roots[index].RootKey)
	}
	return strings.Join(keys, " + ")
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

// publishLocalGroup reuses the group's heads without type-checking when every member's head is
// reusable, and otherwise extracts every member and reuses the heads only when their contexts are
// unchanged too, else publishes the group together.
func (indexer *Indexer) publishLocalGroup(ctx context.Context, check *headCheck, graph localDependencyGraph, group []int, options ModuleOptions, results map[int]ModuleResult, snapshots map[int]storage.ModuleSnapshot) error {
	heads, found, err := indexer.linkGroupHeads(ctx, graph, group)
	if err != nil {
		return err
	}
	reusable := found && !options.Force
	if reusable {
		if reusable, err = groupHeadsReusable(ctx, check, graph, group); err != nil {
			return err
		}
		if reusable {
			return indexer.reuseLocalGroup(ctx, graph, group, heads, nil, options, results, snapshots)
		}
	}
	extractions := make(map[int]moduleExtraction, len(group))
	for _, index := range group {
		extraction, err := extractModule(ctx, extractOptions{loadPackages: indexer.loadPackages, root: graph.roots[index], includeTests: options.IncludeTests})
		if err != nil {
			return fmt.Errorf("extract module %q: %w", graph.roots[index].RootKey, err)
		}
		extractions[index] = extraction
	}
	if found && !options.Force && extractedHeadsMatch(graph, group, heads, extractions) {
		return indexer.reuseLocalGroup(ctx, graph, group, heads, extractions, options, results, snapshots)
	}
	return indexer.createLocalGroup(ctx, graph, group, extractions, options, results, snapshots)
}

// linkGroupHeads loads every member's head, whether all have one, and links the edges between members
// to those heads.
func (indexer *Indexer) linkGroupHeads(ctx context.Context, graph localDependencyGraph, group []int) (map[int]storage.ModuleSnapshot, bool, error) {
	heads := make(map[int]storage.ModuleSnapshot, len(group))
	all := true
	for _, index := range group {
		head, found, err := loadHead(ctx, indexer.database, graph.roots[index])
		if err != nil {
			return nil, false, err
		}
		all = all && found
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
	return heads, all, nil
}

// groupHeadsReusable reports whether every member's head passes reusableHead.
func groupHeadsReusable(ctx context.Context, check *headCheck, graph localDependencyGraph, group []int) (bool, error) {
	for _, index := range group {
		var reusable bool
		if err := Phase(ctx, "unchanged-check", func(ctx context.Context) (err error) {
			_, reusable, err = reusableHead(ctx, check, graph.roots[index], false)
			return err
		}); err != nil || !reusable {
			return false, err
		}
	}
	return true, nil
}

// extractedHeadsMatch reports whether every member's head equals its extraction, context included.
func extractedHeadsMatch(graph localDependencyGraph, group []int, heads map[int]storage.ModuleSnapshot, extractions map[int]moduleExtraction) bool {
	for _, index := range group {
		root, head := graph.roots[index], heads[index]
		if head.Revision != root.Revision || head.WorktreeState != root.WorktreeState || head.ContentSetHash != root.ContentSetHash ||
			head.ConfigurationHash != root.ConfigurationHash || head.ContextHash != extractions[index].contextHash ||
			head.DependencySetHash == nil || *head.DependencySetHash != dependencyHash(root.Dependencies) {
			return false
		}
	}
	return true
}

// reuseLocalGroup reports every member unchanged at its head. extractions, when the members were
// extracted, record the sibling shapes of heads written before they were recorded.
func (indexer *Indexer) reuseLocalGroup(ctx context.Context, graph localDependencyGraph, group []int, heads map[int]storage.ModuleSnapshot, extractions map[int]moduleExtraction, options ModuleOptions, results map[int]ModuleResult, snapshots map[int]storage.ModuleSnapshot) error {
	err := storage.RetryAllocationConflicts(ctx, indexer.database, func(transaction *gorm.DB) error {
		if err := verifyIndexInputs(ctx, transaction, groupInputs(graph, group), options.IncludeTests); err != nil {
			return err
		}
		locations := map[string]storage.ModuleLocation{}
		for _, index := range group {
			extraction := moduleExtraction{root: graph.roots[index], reusedHead: heads[index].ID}
			if _, _, err := ensureGraphLocation(ctx, transaction, graph, index, locations); err != nil {
				return err
			}
			result, location, err := indexModule(ctx, transaction, extraction, locations, ModuleOptions{Reason: storage.ReasonDependencyCycle})
			if err != nil {
				return err
			}
			if err := createSiblingShapes(ctx, transaction, heads[index], extractions[index].siblingShapes); err != nil {
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
	if _, found := locations[root.ParentRootKey]; root.ParentRootKey != "" && !found {
		if parent, member := graph.byPath[root.ParentRootKey]; member {
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
