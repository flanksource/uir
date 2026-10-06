package indexer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/flanksource/uir/storage"
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
		if head.Coverage != storage.CoverageIndexed {
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

func (indexer *Indexer) checkContext(ctx context.Context, root discoveredRoot, indexed string, includeTests bool) error {
	siblings, err := workspaceSiblingImports(root)
	if err != nil {
		return err
	}
	if len(siblings) == 0 {
		return nil
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
