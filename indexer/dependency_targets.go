package indexer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var pseudoVersionCommit = regexp.MustCompile(`-[0-9]{14}-([0-9a-f]{12,})$`)
var errVersionUnavailable = errors.New("versioned dependency snapshot unavailable")

type activeModulePaths struct{}

func withActivePaths(ctx context.Context, roots []discoveredRoot) context.Context {
	active, _ := ctx.Value(activeModulePaths{}).(map[string]bool)
	copy := make(map[string]bool, len(active)+len(roots))
	for path := range active {
		copy[path] = true
	}
	for _, root := range roots {
		copy[root.LocalPath] = true
	}
	return context.WithValue(ctx, activeModulePaths{}, copy)
}

func (indexer *Indexer) indexLocalDependencies(ctx context.Context, roots []discoveredRoot, includeTests bool) error {
	active, _ := ctx.Value(activeModulePaths{}).(map[string]bool)
	for rootIndex := range roots {
		root := &roots[rootIndex]
		for dependencyIndex := range root.Dependencies {
			dependency := &root.Dependencies[dependencyIndex]
			if dependency.LocalDir == "" {
				continue
			}
			path, err := filepath.EvalSymlinks(dependency.LocalDir)
			if err != nil {
				dependency.Edge.UnresolvedReason = fmt.Sprintf("resolve local dependency %q: %v", dependency.LocalDir, err)
				continue
			}
			dependency.LocalDir = path
			if active[path] {
				return fmt.Errorf("local dependency cycle at %q escaped graph publication", path)
			}
			results, err := indexer.localDependencyResults(ctx, path, includeTests)
			if err != nil {
				return fmt.Errorf("index local dependency %q: %w", path, err)
			}
			if len(results) != 1 {
				dependency.Edge.UnresolvedReason = fmt.Sprintf("local dependency %q produced %d snapshots", path, len(results))
				continue
			}
			var snapshot storage.ModuleSnapshot
			if err := indexer.database.WithContext(ctx).Where("id = ?", uuid.MustParse(results[0].SnapshotID)).Take(&snapshot).Error; err != nil {
				return fmt.Errorf("load local dependency snapshot %q: %w", results[0].SnapshotID, err)
			}
			linkLocalSnapshot(root, dependency, snapshot)
		}
	}
	return nil
}

// linkLocalSnapshot points a local dependency's edge at snapshot; a committed root whose dependency's
// snapshot is not clean becomes dirty as of the later of their modification times.
func linkLocalSnapshot(root *discoveredRoot, dependency *dependencyObservation, snapshot storage.ModuleSnapshot) {
	dependency.Edge.TargetSnapshotID = &snapshot.ID
	dependency.Edge.UnresolvedReason = ""
	if root.GitCommit != "" && snapshot.WorktreeState != storage.WorktreeClean {
		root.WorktreeState = storage.WorktreeDirty
		if snapshot.LastModifiedAt != nil {
			root.LastModifiedAt = maxTime(root.LastModifiedAt, *snapshot.LastModifiedAt)
		}
		root.Revision = dirtyRevision(root.GitCommit, root.LastModifiedAt)
	}
}

// localDependencyResults is the outcome of indexing the local dependency at path: the run's
// remembered result for that location, or else an index of it as a step of the run.
func (indexer *Indexer) localDependencyResults(ctx context.Context, path string, includeTests bool) ([]ModuleResult, error) {
	ctx, memo := ensureRunMemo(ctx)
	if result, found := memo.module(path, includeTests); found {
		return []ModuleResult{result}, nil
	}
	return indexStep(ctx, "local dependency "+path, func(ctx context.Context) ([]ModuleResult, error) {
		return indexer.IndexModules(ctx, ModuleOptions{Path: path, ExactLocation: path, IncludeTests: includeTests, Reason: storage.ReasonLocalDependency})
	})
}

// indexVersionedDependencies links every root's versioned dependencies to their snapshots, resolving
// those the run has not resolved yet together, as one versioned dependency graph.
func (indexer *Indexer) indexVersionedDependencies(ctx context.Context, roots []discoveredRoot, includeTests bool) error {
	ctx, memo := ensureRunMemo(ctx)
	var keys []versionMemoKey
	for _, root := range roots {
		for _, dependency := range root.Dependencies {
			if key, versioned := versionedRequirement(dependency, includeTests); versioned {
				keys = append(keys, key)
			}
		}
	}
	if err := indexer.resolveVersions(ctx, memo, keys, includeTests); err != nil {
		return err
	}
	for rootIndex := range roots {
		for dependencyIndex := range roots[rootIndex].Dependencies {
			if err := linkVersionedDependency(memo, &roots[rootIndex].Dependencies[dependencyIndex], includeTests); err != nil {
				return err
			}
		}
	}
	return nil
}

// versionedSnapshot is the snapshot of modulePath at version, resolved once per run: a later request
// in the same run gets the same snapshot, or the same errVersionUnavailable.
func (indexer *Indexer) versionedSnapshot(ctx context.Context, modulePath, version string, includeTests bool) (storage.ModuleSnapshot, error) {
	ctx, memo := ensureRunMemo(ctx)
	key := versionMemoKey{modulePath: modulePath, version: version, includeTests: includeTests}
	if err := indexer.resolveVersions(ctx, memo, []versionMemoKey{key}, includeTests); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	outcome, err := resolvedVersion(memo, key)
	if err != nil {
		return storage.ModuleSnapshot{}, err
	}
	return outcome.snapshot, outcome.unavailable
}

// resolveVersions resolves every versioned dependency of keys, and of their closures, that the run has
// not resolved yet, recording each outcome in memo. Each version is checked out at most once.
func (indexer *Indexer) resolveVersions(ctx context.Context, memo *runMemo, keys []versionMemoKey, includeTests bool) error {
	graph, err := indexer.discoverVersionedGraph(ctx, memo, keys, includeTests)
	if err != nil {
		return err
	}
	return errors.Join(indexer.resolveVersionedGraph(ctx, memo, graph), graph.close())
}

// versionedRequirement is the version a dependency selects, unless it is local or selects none.
func versionedRequirement(dependency dependencyObservation, includeTests bool) (versionMemoKey, bool) {
	if dependency.LocalDir != "" || dependency.TargetVersion == "" || dependency.TargetModulePath == "" {
		return versionMemoKey{}, false
	}
	return versionMemoKey{modulePath: dependency.TargetModulePath, version: dependency.TargetVersion, includeTests: includeTests}, true
}

// linkVersionedDependency points a versioned dependency's edge at the snapshot the run resolved its
// version to, or records why none is available.
func linkVersionedDependency(memo *runMemo, dependency *dependencyObservation, includeTests bool) error {
	key, versioned := versionedRequirement(*dependency, includeTests)
	if !versioned {
		return nil
	}
	outcome, err := resolvedVersion(memo, key)
	if err != nil {
		return err
	}
	if outcome.unavailable != nil {
		dependency.Edge.UnresolvedReason = outcome.unavailable.Error()
		return nil
	}
	id := outcome.snapshot.ID
	dependency.Edge.TargetSnapshotID = &id
	dependency.Edge.UnresolvedReason = ""
	return nil
}

func resolvedVersion(memo *runMemo, key versionMemoKey) (versionOutcome, error) {
	outcome, found := memo.version(key)
	if !found {
		return versionOutcome{}, fmt.Errorf("versioned dependency %s was not resolved before it was linked", versionedKey(key.modulePath, key.version))
	}
	return outcome, nil
}

// versionTagMatches reports whether version still names storedCommit in the first registered checkout
// of modulePath that has it, or no checkout has it.
func (indexer *Indexer) versionTagMatches(ctx context.Context, memo *runMemo, modulePath, version, storedCommit string) (bool, error) {
	tag, err := indexer.versionTag(ctx, memo, modulePath, version)
	if err != nil {
		return false, err
	}
	return !tag.found || tag.commit == storedCommit, nil
}

// versionTag is the commit version names in the first registered checkout of modulePath that has it,
// looked up once per run.
func (indexer *Indexer) versionTag(ctx context.Context, memo *runMemo, modulePath, version string) (tagCommit, error) {
	return memo.tagCommit(tagMemoKey{modulePath: modulePath, version: version}, func() (tagCommit, error) {
		var locations []storage.ModuleLocation
		if err := indexer.database.WithContext(ctx).Table("locations AS location").Select("location.*").
			Joins("JOIN modules AS root ON root.id = location.root_id").Where("root.root_key = ?", modulePath).
			Order("location.canonical_path").Find(&locations).Error; err != nil {
			return tagCommit{}, fmt.Errorf("find checkouts for %s@%s: %w", modulePath, version, err)
		}
		for _, location := range locations {
			if commit, err := memo.versionCommit(ctx, location.CanonicalPath, version); err == nil {
				return tagCommit{commit: commit, checkout: location.CanonicalPath, found: true}, nil
			}
		}
		return tagCommit{}, nil
	})
}

// storedVersionSnapshot is the newest clean snapshot of modulePath at version, a complete one before
// any degraded one. A caller that reuses it without extracting must check its coverage.
func (indexer *Indexer) storedVersionSnapshot(ctx context.Context, modulePath, version string) (storage.ModuleSnapshot, bool, error) {
	var snapshot storage.ModuleSnapshot
	err := indexer.database.WithContext(ctx).Table("snapshots AS snapshot").Select("snapshot.*").
		Joins("JOIN modules AS root ON root.id = snapshot.root_id").
		Where("root.root_key = ? AND snapshot.module_version = ? AND snapshot.worktree_state = ?", modulePath, version, storage.WorktreeClean).
		Order(clause.OrderBy{Expression: clause.Expr{SQL: "CASE WHEN snapshot.coverage IN ? THEN 0 ELSE 1 END, snapshot.completed_at DESC", Vars: []any{storage.CompleteCoverages}}}).
		Take(&snapshot).Error
	if err == nil {
		return snapshot, true, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return storage.ModuleSnapshot{}, false, fmt.Errorf("find indexed version %s@%s: %w", modulePath, version, err)
	}
	return storage.ModuleSnapshot{}, false, nil
}

// versionedClosureMiss is why the closure of snapshotID, every snapshot reachable from it through
// dependency edges, is not complete, or "" when it is. It reads each snapshot's node once per run.
func (indexer *Indexer) versionedClosureMiss(ctx context.Context, memo *runMemo, snapshotID uuid.UUID) (string, error) {
	visited := map[uuid.UUID]bool{}
	queue := []uuid.UUID{snapshotID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		node, err := memo.closureNode(id, func() (closureNode, error) { return indexer.loadClosureNode(ctx, memo, id) })
		if err != nil {
			return "", err
		}
		if node.miss != "" {
			return node.miss, nil
		}
		queue = append(queue, node.targets...)
	}
	return "", nil
}

// loadClosureNode reads whether snapshot id is itself complete: its dependencies were captured, its
// version, when it has one, still names its commit, and every dependency edge has a target or could
// not get one by indexing again. Snapshot rows and edges never change once committed, and the run looks
// each version tag up once, so the node holds for the rest of the run.
func (indexer *Indexer) loadClosureNode(ctx context.Context, memo *runMemo, id uuid.UUID) (closureNode, error) {
	var snapshot storage.ModuleSnapshot
	if err := indexer.database.WithContext(ctx).Where("id = ?", id).Take(&snapshot).Error; err != nil {
		return closureNode{}, fmt.Errorf("load version snapshot %s: %w", id, err)
	}
	var root storage.ModuleRoot
	if err := indexer.database.WithContext(ctx).Where("id = ?", snapshot.RootID).Take(&root).Error; err != nil {
		return closureNode{}, fmt.Errorf("load root of snapshot %s: %w", id, err)
	}
	name := versionedKey(root.RootKey, snapshot.ModuleVersion)
	if snapshot.DependencySetHash == nil {
		return closureNode{miss: fmt.Sprintf("%s (snapshot %s) recorded no dependency set", name, id)}, nil
	}
	if snapshot.ModuleVersion != "" {
		matching, err := indexer.versionTagMatches(ctx, memo, root.RootKey, snapshot.ModuleVersion, snapshot.GitCommit)
		if err != nil || !matching {
			return closureNode{miss: fmt.Sprintf("%s no longer names commit %s", name, snapshot.GitCommit)}, err
		}
	}
	var edges []storage.SnapshotDependency
	if err := indexer.database.WithContext(ctx).Where("snapshot_id = ?", id).Find(&edges).Error; err != nil {
		return closureNode{}, fmt.Errorf("load dependencies of version snapshot %s: %w", id, err)
	}
	targets := make([]uuid.UUID, 0, len(edges))
	for _, edge := range edges {
		if edge.TargetSnapshotID != nil {
			targets = append(targets, *edge.TargetSnapshotID)
			continue
		}
		if resolvable, err := indexer.unlinkedEdgeResolvable(ctx, memo, snapshot, edge); err != nil || resolvable {
			return closureNode{miss: fmt.Sprintf("%s: edge to %s %q has no target but could get one (%s)", name, edge.ModulePath, edge.SelectedVersion, edge.UnresolvedReason)}, err
		}
	}
	return closureNode{targets: targets}, nil
}

// unlinkedEdgeResolvable reports whether indexing owner again could give its edge without a target
// one: a registered checkout of the module it selects has the selected version, the rule
// addVersionedNode follows. A version's root is workspace-free and drops local replacements
// (historicalRoot), so module selection over its commit is all that decides an edge of it that selects
// no version, and indexing that commit again, which the caller verified the version still names when
// a checkout has it, selects none again. Any other edge that selects no version cannot be decided, so it counts as resolvable.
func (indexer *Indexer) unlinkedEdgeResolvable(ctx context.Context, memo *runMemo, owner storage.ModuleSnapshot, edge storage.SnapshotDependency) (bool, error) {
	modulePath, version := edge.ModulePath, edge.SelectedVersion
	if edge.ReplacePath != "" && edge.ReplaceVersion != "" {
		modulePath, version = edge.ReplacePath, edge.ReplaceVersion
	}
	if version == "" {
		return owner.ModuleVersion == "", nil
	}
	tag, err := indexer.versionTag(ctx, memo, modulePath, version)
	return tag.found, err
}

func versionCommit(ctx context.Context, checkout, version string) (string, error) {
	top, err := revisionGit(ctx, checkout, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	if match := pseudoVersionCommit.FindStringSubmatch(version); match != nil {
		return revisionGit(ctx, checkout, "rev-parse", "--verify", match[1]+"^{commit}")
	}
	relative, err := filepath.Rel(top, checkout)
	if err != nil {
		return "", err
	}
	tag := version
	if relative != "." {
		tag = strings.TrimPrefix(filepath.ToSlash(relative), "./") + "/" + version
	}
	return revisionGit(ctx, checkout, "rev-parse", "--verify", "refs/tags/"+tag+"^{commit}")
}

func maxTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}
