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
)

var pseudoVersionCommit = regexp.MustCompile(`-[0-9]{14}-([0-9a-f]{12,})$`)
var errVersionUnavailable = errors.New("versioned dependency snapshot unavailable")

type activeModulePaths struct{}
type activeModuleVersions struct{}

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
			results, err := indexer.IndexModules(ctx, ModuleOptions{Path: path, ExactLocation: path, IncludeTests: includeTests})
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
	}
	return nil
}

func (indexer *Indexer) indexVersionedDependencies(ctx context.Context, roots []discoveredRoot, includeTests bool) error {
	for rootIndex := range roots {
		for dependencyIndex := range roots[rootIndex].Dependencies {
			dependency := &roots[rootIndex].Dependencies[dependencyIndex]
			if dependency.LocalDir != "" || dependency.TargetVersion == "" || dependency.TargetModulePath == "" {
				continue
			}
			snapshot, err := indexer.versionedSnapshot(ctx, dependency.TargetModulePath, dependency.TargetVersion, includeTests)
			if err != nil {
				if !errors.Is(err, errVersionUnavailable) {
					return err
				}
				dependency.Edge.UnresolvedReason = err.Error()
				continue
			}
			dependency.Edge.TargetSnapshotID = &snapshot.ID
			dependency.Edge.UnresolvedReason = ""
		}
	}
	return nil
}

func (indexer *Indexer) versionedSnapshot(ctx context.Context, modulePath, version string, includeTests bool) (snapshot storage.ModuleSnapshot, err error) {
	snapshot, found, err := indexer.storedVersionSnapshot(ctx, modulePath, version)
	if err != nil {
		return storage.ModuleSnapshot{}, err
	}
	if found {
		complete, err := indexer.versionedClosureComplete(ctx, snapshot.ID)
		if err != nil {
			return storage.ModuleSnapshot{}, err
		}
		if complete {
			return snapshot, nil
		}
	}
	graph, err := indexer.discoverVersionedGraph(ctx, modulePath, version, includeTests)
	if err != nil {
		if found && errors.Is(err, errVersionUnavailable) {
			return snapshot, nil
		}
		return storage.ModuleSnapshot{}, err
	}
	defer func() { err = errors.Join(err, graph.close()) }()
	if graph.cyclic() {
		return indexer.publishVersionedComponents(ctx, graph, includeTests)
	}
	if found {
		return snapshot, nil
	}
	key := modulePath + "@" + version
	active, _ := ctx.Value(activeModuleVersions{}).(map[string]bool)
	if active[key] {
		return storage.ModuleSnapshot{}, fmt.Errorf("versioned dependency cycle at %s escaped graph publication", key)
	}
	next := make(map[string]bool, len(active)+1)
	for existing := range active {
		next[existing] = true
	}
	next[key] = true
	ctx = context.WithValue(ctx, activeModuleVersions{}, next)
	prepared := graph.nodes[0].prepared
	result, err := indexer.IndexRevision(ctx, RevisionOptions{
		RootKey: modulePath, Checkout: prepared.location.CanonicalPath, Commit: prepared.root.GitCommit, Version: version, IncludeTests: includeTests,
	})
	if err != nil {
		return storage.ModuleSnapshot{}, fmt.Errorf("index version %s from %q: %w", key, prepared.location.CanonicalPath, err)
	}
	if err := indexer.database.WithContext(ctx).Where("id = ?", result.SnapshotID).Take(&snapshot).Error; err != nil {
		return storage.ModuleSnapshot{}, fmt.Errorf("load version snapshot %s: %w", result.SnapshotID, err)
	}
	return snapshot, nil
}

func (indexer *Indexer) versionTagMatches(ctx context.Context, modulePath, version, storedCommit string) (bool, error) {
	var locations []storage.ModuleLocation
	if err := indexer.database.WithContext(ctx).Table("locations AS location").Select("location.*").
		Joins("JOIN modules AS root ON root.id = location.root_id").Where("root.root_key = ?", modulePath).
		Order("location.canonical_path").Find(&locations).Error; err != nil {
		return false, fmt.Errorf("find checkouts for %s@%s: %w", modulePath, version, err)
	}
	for _, location := range locations {
		commit, err := versionCommit(ctx, location.CanonicalPath, version)
		if err == nil {
			return commit == storedCommit, nil
		}
	}
	return true, nil
}

func (indexer *Indexer) storedVersionSnapshot(ctx context.Context, modulePath, version string) (storage.ModuleSnapshot, bool, error) {
	var snapshot storage.ModuleSnapshot
	err := indexer.database.WithContext(ctx).Table("snapshots AS snapshot").Select("snapshot.*").
		Joins("JOIN modules AS root ON root.id = snapshot.root_id").
		Where("root.root_key = ? AND snapshot.module_version = ? AND snapshot.worktree_state = ?", modulePath, version, storage.WorktreeClean).
		Order("snapshot.completed_at DESC").Take(&snapshot).Error
	if err == nil {
		return snapshot, true, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return storage.ModuleSnapshot{}, false, fmt.Errorf("find indexed version %s@%s: %w", modulePath, version, err)
	}
	return storage.ModuleSnapshot{}, false, nil
}

func (indexer *Indexer) versionedClosureComplete(ctx context.Context, snapshotID uuid.UUID) (bool, error) {
	visited := map[uuid.UUID]bool{}
	queue := []uuid.UUID{snapshotID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		var snapshot storage.ModuleSnapshot
		if err := indexer.database.WithContext(ctx).Where("id = ?", id).Take(&snapshot).Error; err != nil {
			return false, fmt.Errorf("load version snapshot %s: %w", id, err)
		}
		if snapshot.DependencySetHash == nil {
			return false, nil
		}
		if snapshot.ModuleVersion != "" {
			var root storage.ModuleRoot
			if err := indexer.database.WithContext(ctx).Where("id = ?", snapshot.RootID).Take(&root).Error; err != nil {
				return false, fmt.Errorf("load root of version snapshot %s: %w", id, err)
			}
			matching, err := indexer.versionTagMatches(ctx, root.RootKey, snapshot.ModuleVersion, snapshot.GitCommit)
			if err != nil {
				return false, err
			}
			if !matching {
				return false, nil
			}
		}
		var edges []storage.SnapshotDependency
		if err := indexer.database.WithContext(ctx).Where("snapshot_id = ?", id).Find(&edges).Error; err != nil {
			return false, fmt.Errorf("load dependencies of version snapshot %s: %w", id, err)
		}
		for _, edge := range edges {
			if edge.TargetSnapshotID == nil {
				return false, nil
			}
			queue = append(queue, *edge.TargetSnapshotID)
		}
	}
	return true, nil
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
