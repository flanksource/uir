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
				dependency.Edge.UnresolvedReason = "local dependency is in the active indexing cycle"
				continue
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

func (indexer *Indexer) versionedSnapshot(ctx context.Context, modulePath, version string, includeTests bool) (storage.ModuleSnapshot, error) {
	var snapshot storage.ModuleSnapshot
	err := indexer.database.WithContext(ctx).Table("snapshots AS snapshot").Select("snapshot.*").
		Joins("JOIN modules AS root ON root.id = snapshot.root_id").
		Where("root.root_key = ? AND snapshot.module_version = ? AND snapshot.worktree_state = ?", modulePath, version, storage.WorktreeClean).
		Order("snapshot.completed_at DESC").Take(&snapshot).Error
	if err == nil {
		return snapshot, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return storage.ModuleSnapshot{}, fmt.Errorf("find indexed version %s@%s: %w", modulePath, version, err)
	}
	key := modulePath + "@" + version
	active, _ := ctx.Value(activeModuleVersions{}).(map[string]bool)
	if active[key] {
		return storage.ModuleSnapshot{}, fmt.Errorf("%w: cycle at %s", errVersionUnavailable, key)
	}
	next := make(map[string]bool, len(active)+1)
	for existing := range active {
		next[existing] = true
	}
	next[key] = true
	ctx = context.WithValue(ctx, activeModuleVersions{}, next)
	var locations []storage.ModuleLocation
	if err := indexer.database.WithContext(ctx).Table("locations AS location").Select("location.*").
		Joins("JOIN modules AS root ON root.id = location.root_id").Where("root.root_key = ?", modulePath).
		Order("location.canonical_path").Find(&locations).Error; err != nil {
		return storage.ModuleSnapshot{}, fmt.Errorf("find checkouts for %s@%s: %w", modulePath, version, err)
	}
	for _, location := range locations {
		commit, err := versionCommit(ctx, location.CanonicalPath, version)
		if err != nil {
			continue
		}
		result, err := indexer.IndexRevision(ctx, RevisionOptions{
			RootKey: modulePath, Checkout: location.CanonicalPath, Commit: commit, Version: version, IncludeTests: includeTests,
		})
		if err != nil {
			return storage.ModuleSnapshot{}, fmt.Errorf("index version %s@%s from %q: %w", modulePath, version, location.CanonicalPath, err)
		}
		if err := indexer.database.WithContext(ctx).Where("id = ?", result.SnapshotID).Take(&snapshot).Error; err != nil {
			return storage.ModuleSnapshot{}, fmt.Errorf("load version snapshot %s: %w", result.SnapshotID, err)
		}
		return snapshot, nil
	}
	return storage.ModuleSnapshot{}, fmt.Errorf("%w: no registered checkout contains %s@%s", errVersionUnavailable, modulePath, version)
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
