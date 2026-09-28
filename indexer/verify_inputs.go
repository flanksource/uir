package indexer

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

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
				return fmt.Errorf("module %q changed while indexing; retry", root.RootKey)
			}
		}
		if !matched {
			return fmt.Errorf("module %q disappeared while indexing; retry", root.RootKey)
		}
		for _, dependency := range root.Dependencies {
			if dependency.LocalDir == "" || dependency.Edge.TargetSnapshotID == nil {
				continue
			}
			var snapshot storage.ModuleSnapshot
			if err := database.WithContext(ctx).Where("id = ?", *dependency.Edge.TargetSnapshotID).Take(&snapshot).Error; err != nil {
				return fmt.Errorf("recheck dependency %q snapshot: %w", dependency.Edge.ModulePath, err)
			}
			var head storage.ModuleLocationHead
			if err := database.WithContext(ctx).Where("location_id = ?", snapshot.LocationID).Take(&head).Error; err != nil {
				return fmt.Errorf("recheck dependency %q head: %w", dependency.Edge.ModulePath, err)
			}
			if head.SnapshotID != snapshot.ID {
				return fmt.Errorf("dependency %q head changed while indexing; retry", dependency.Edge.ModulePath)
			}
			fresh, err := discoverModules(ctx, dependency.LocalDir, includeTests)
			if err != nil {
				return fmt.Errorf("recheck dependency %q source: %w", dependency.Edge.ModulePath, err)
			}
			found := false
			for _, current := range fresh {
				if current.LocalPath == filepath.Clean(dependency.LocalDir) {
					found = true
					if current.ContentSetHash != snapshot.ContentSetHash || current.GitCommit != snapshot.GitCommit {
						return fmt.Errorf("dependency %q source changed while indexing; retry", dependency.Edge.ModulePath)
					}
				}
			}
			if !found {
				return fmt.Errorf("dependency %q source disappeared while indexing; retry", dependency.Edge.ModulePath)
			}
		}
	}
	return nil
}
