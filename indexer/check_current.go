package indexer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// CheckCurrent verifies an existing location head against the checkout without publishing a snapshot.
func (indexer *Indexer) CheckCurrent(ctx context.Context, path string, includeTests bool) (ModuleResult, error) {
	if indexer == nil || indexer.database == nil {
		return ModuleResult{}, errors.New("UIR index database is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ModuleResult{}, fmt.Errorf("resolve checkout %q: %w", path, err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return ModuleResult{}, fmt.Errorf("canonicalize checkout %q: %w", path, err)
	}
	roots, err := discoverModules(ctx, canonical, includeTests)
	if err != nil {
		return ModuleResult{}, err
	}
	for _, root := range roots {
		if root.LocalPath != canonical {
			continue
		}
		head, found, err := loadHead(ctx, indexer.database, root)
		if err != nil {
			return ModuleResult{}, err
		}
		if !found {
			return ModuleResult{}, fmt.Errorf("no indexed head for %q at %q; run uir reindex", root.RootKey, root.LocalPath)
		}
		if head.Revision != root.Revision || head.ContentSetHash != root.ContentSetHash || head.ConfigurationHash != root.ConfigurationHash {
			return ModuleResult{}, fmt.Errorf("stale UIR index for %q at %q; run uir reindex", root.RootKey, root.LocalPath)
		}
		if err := indexer.checkContext(ctx, root, head.ContextHash, includeTests); err != nil {
			return ModuleResult{}, err
		}
		if !head.Coverage.Complete() {
			return ModuleResult{}, fmt.Errorf("incomplete UIR index for %q at %q: coverage %s", root.RootKey, root.LocalPath, head.Coverage)
		}
		return ModuleResult{RootKey: root.RootKey, Location: root.LocalPath, SnapshotID: head.ID.String()}, nil
	}
	return ModuleResult{}, fmt.Errorf("%q is not a Go module root", canonical)
}

// HeadIncludesTests reports whether the head of the module checked out at path was indexed with its
// test files: the one test setting whose configuration hash, under the checkout's Go environment,
// is the head's.
func (indexer *Indexer) HeadIncludesTests(ctx context.Context, path string) (bool, error) {
	if indexer == nil || indexer.database == nil {
		return false, errors.New("UIR index database is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("resolve checkout %q: %w", path, err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return false, fmt.Errorf("canonicalize checkout %q: %w", path, err)
	}
	roots, err := locateModuleRoots(canonical)
	if err != nil {
		return false, err
	}
	for _, root := range roots {
		if root.LocalPath != canonical {
			continue
		}
		head, found, err := loadHead(ctx, indexer.database, root)
		if err != nil {
			return false, err
		}
		if !found {
			return false, fmt.Errorf("no indexed head for %q at %q; run uir reindex", root.RootKey, root.LocalPath)
		}
		environment, err := readGoEnvironment(ctx, root.LocalPath)
		if err != nil {
			return false, fmt.Errorf("root %q: %w", root.RootKey, err)
		}
		for _, includeTests := range []bool{true, false} {
			if configurationHash(includeTests, environment.Variant) == head.ConfigurationHash {
				return includeTests, nil
			}
		}
		return false, fmt.Errorf("head of %q at %q was indexed under another Go build configuration; run uir reindex", root.RootKey, root.LocalPath)
	}
	return false, fmt.Errorf("%q is not a Go module root", canonical)
}

// checkContext confirms the head's local dependency context when the root imports workspace siblings:
// through the sibling shapes and source digests the head recorded, when they hold, and otherwise by
// extracting the root and comparing context hashes, the authoritative check.
func (indexer *Indexer) checkContext(ctx context.Context, root discoveredRoot, indexed string, includeTests bool) error {
	siblings, _, err := workspaceSiblingImports(root)
	if err != nil {
		return err
	}
	if len(siblings) == 0 {
		return nil
	}
	_, reason, err := indexer.storedHeadCheck(includeTests).reusedRoot(ctx, root)
	if err != nil {
		return err
	}
	if reason == "" {
		return nil
	}
	if log, found := ctx.Value(phaseLogKey{}).(phaseLog); found {
		log.Infof("check-current: %s at %s: %s", root.RootKey, root.LocalPath, reason)
	}
	extracted, err := extractModule(ctx, indexer.loadPackages, root, includeTests)
	if err != nil {
		return err
	}
	if extracted.contextHash != indexed {
		return fmt.Errorf("stale UIR index for %q at %q: local dependency context changed; run uir reindex", root.RootKey, root.LocalPath)
	}
	return nil
}

// storedHeadCheck is a headCheck, with a memo of its own, that links versioned dependencies only to
// snapshots already stored, so confirming a head never checks out, indexes or publishes a version.
func (indexer *Indexer) storedHeadCheck(includeTests bool) *headCheck {
	memo := newRunMemo()
	check := newHeadCheck(indexer, memo, includeTests)
	check.resolveVersions = func(ctx context.Context, roots []discoveredRoot, includeTests bool) error {
		return indexer.linkStoredVersions(ctx, memo, roots, includeTests)
	}
	return check
}

// linkStoredVersions links roots' versioned dependencies as indexVersionedDependencies would when it
// needs to index none of them, and records any version it would index as unavailable instead.
func (indexer *Indexer) linkStoredVersions(ctx context.Context, memo *runMemo, roots []discoveredRoot, includeTests bool) error {
	for rootIndex := range roots {
		for dependencyIndex := range roots[rootIndex].Dependencies {
			dependency := &roots[rootIndex].Dependencies[dependencyIndex]
			key, versioned := versionedRequirement(*dependency, includeTests)
			if !versioned {
				continue
			}
			if err := indexer.rememberStoredVersion(ctx, memo, key); err != nil {
				return err
			}
			if err := linkVersionedDependency(memo, dependency, includeTests); err != nil {
				return err
			}
		}
	}
	return nil
}

// rememberStoredVersion records the outcome expandVersionedNode would record for key without indexing:
// the stored snapshot when it stands, or, when no registered checkout has the version, the stored
// snapshot or the same unavailable reason. A version it would index is recorded as unavailable for
// that reason, so the root's dependency set differs from its head's.
func (indexer *Indexer) rememberStoredVersion(ctx context.Context, memo *runMemo, key versionMemoKey) error {
	if _, found := memo.version(key); found {
		return nil
	}
	stored, found, err := indexer.storedVersionSnapshot(ctx, key.modulePath, key.version)
	if err != nil {
		return err
	}
	miss := "no stored snapshot"
	if found {
		if miss, err = indexer.storedVersionMiss(ctx, memo, stored); err != nil {
			return err
		}
	}
	tag := tagCommit{}
	if miss != "" {
		if tag, err = indexer.versionTag(ctx, memo, key.modulePath, key.version); err != nil {
			return err
		}
	}
	name := versionedKey(key.modulePath, key.version)
	switch {
	case miss == "" || (found && !tag.found):
		memo.rememberVersion(key, versionOutcome{snapshot: stored})
	case !tag.found:
		memo.rememberVersion(key, versionOutcome{unavailable: versionNotRegistered(name)})
	default:
		memo.rememberVersion(key, versionOutcome{unavailable: fmt.Errorf("%w: %s would be indexed again: %s", errVersionUnavailable, name, miss)})
	}
	return nil
}
