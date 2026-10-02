package indexer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

// ErrIndexInputsChanged marks an index that saw its inputs change under it: a source, manifest, or
// dependency head moved between discovery and publication, or a concurrent index published the head
// first. Nothing was published, and running the whole index again starts from the new inputs.
var ErrIndexInputsChanged = errors.New("index inputs changed while indexing")

// verifyIndexInputs rediscovers every root and its local dependencies inside the publication
// transaction and fails with ErrIndexInputsChanged when any of them no longer matches what was
// extracted, so the publication cannot commit facts about inputs that are already gone.
func verifyIndexInputs(ctx context.Context, database *gorm.DB, roots []discoveredRoot, includeTests bool) error {
	for _, root := range roots {
		fresh, err := discoverModules(ctx, root.LocalPath, includeTests)
		if err != nil {
			return fmt.Errorf("recheck module %q before publication: %w", root.RootKey, err)
		}
		matched := false
		for _, current := range fresh {
			if current.LocalPath != root.LocalPath {
				continue
			}
			matched = true
			if current.RootKey != root.RootKey || current.GitCommit != root.GitCommit || current.ContentSetHash != root.ContentSetHash || current.ConfigurationHash != root.ConfigurationHash {
				return fmt.Errorf("module %q changed: %w", root.RootKey, ErrIndexInputsChanged)
			}
		}
		if !matched {
			return fmt.Errorf("module %q disappeared: %w", root.RootKey, ErrIndexInputsChanged)
		}
		for _, dependency := range root.Dependencies {
			if dependency.LocalDir == "" || dependency.Edge.TargetSnapshotID == nil {
				continue
			}
			if err := verifyLocalDependency(ctx, database, dependency, includeTests); err != nil {
				return err
			}
		}
	}
	return nil
}

// verifyLocalDependency checks that a local dependency's head is still the snapshot the edge targets
// and that its sources still match that snapshot.
func verifyLocalDependency(ctx context.Context, database *gorm.DB, dependency dependencyObservation, includeTests bool) error {
	var snapshot storage.ModuleSnapshot
	if err := database.WithContext(ctx).Where("id = ?", *dependency.Edge.TargetSnapshotID).Take(&snapshot).Error; err != nil {
		return fmt.Errorf("recheck dependency %q snapshot: %w", dependency.Edge.ModulePath, err)
	}
	var head storage.ModuleLocationHead
	if err := database.WithContext(ctx).Where("location_id = ?", snapshot.LocationID).Take(&head).Error; err != nil {
		return fmt.Errorf("recheck dependency %q head: %w", dependency.Edge.ModulePath, err)
	}
	if head.SnapshotID != snapshot.ID {
		return fmt.Errorf("dependency %q head changed: %w", dependency.Edge.ModulePath, ErrIndexInputsChanged)
	}
	fresh, err := discoverModules(ctx, dependency.LocalDir, includeTests)
	if err != nil {
		return fmt.Errorf("recheck dependency %q source: %w", dependency.Edge.ModulePath, err)
	}
	for _, current := range fresh {
		if current.LocalPath == filepath.Clean(dependency.LocalDir) {
			if current.ContentSetHash != snapshot.ContentSetHash || current.GitCommit != snapshot.GitCommit {
				return fmt.Errorf("dependency %q source changed: %w", dependency.Edge.ModulePath, ErrIndexInputsChanged)
			}
			return nil
		}
	}
	return fmt.Errorf("dependency %q source disappeared: %w", dependency.Edge.ModulePath, ErrIndexInputsChanged)
}
