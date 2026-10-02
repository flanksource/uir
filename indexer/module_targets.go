package indexer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/flanksource/uir/storage"
)

// moduleTargets locates the modules a request indexes, one target each, without reading their
// sources. Each target is indexed on its own, as the module at its checkout below the canonical
// request path.
func (indexer *Indexer) moduleTargets(ctx context.Context, options ModuleOptions) ([]moduleTarget, error) {
	if indexer == nil || indexer.database == nil {
		return nil, errors.New("UIR index database is required")
	}
	path := options.Path
	if path == "" {
		path = "."
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve module path %q: %w", path, err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("canonicalize module path %q: %w", absolute, err)
	}
	roots, err := locateModuleRoots(canonical)
	if err != nil {
		return nil, err
	}
	targets := make([]moduleTarget, 0, len(roots))
	for _, root := range roots {
		if options.ExactLocation != "" && root.LocalPath != options.ExactLocation {
			continue
		}
		if options.ExistingOnly {
			if err := requireRegisteredLocation(ctx, indexer.database, root); err != nil {
				return nil, err
			}
		}
		target := options
		target.Path, target.ExactLocation = canonical, root.LocalPath
		targets = append(targets, moduleTarget{rootKey: root.RootKey, location: root.LocalPath, options: target})
	}
	if options.ExactLocation != "" && len(targets) != 1 {
		return nil, fmt.Errorf("registered checkout %q resolved to %d Go modules", options.ExactLocation, len(targets))
	}
	return targets, nil
}

// missingHeadTargets is every registered checkout without a head, shortest path first so an enclosing
// module is indexed before the modules nested in it.
func (indexer *Indexer) missingHeadTargets(ctx context.Context, includeTests bool) ([]moduleTarget, error) {
	if indexer == nil || indexer.database == nil {
		return nil, errors.New("UIR index database is required")
	}
	var locations []struct {
		RootKey       string
		CanonicalPath string
	}
	if err := indexer.database.WithContext(ctx).Table("locations AS location").Select("root.root_key, location.canonical_path").
		Joins("JOIN modules AS root ON root.id = location.root_id").
		Joins("LEFT JOIN location_heads AS head ON head.location_id = location.id").
		Where("head.location_id IS NULL").Scan(&locations).Error; err != nil {
		return nil, fmt.Errorf("list registered checkouts without indexed heads: %w", err)
	}
	sort.Slice(locations, func(i, j int) bool {
		if len(locations[i].CanonicalPath) != len(locations[j].CanonicalPath) {
			return len(locations[i].CanonicalPath) < len(locations[j].CanonicalPath)
		}
		return locations[i].CanonicalPath < locations[j].CanonicalPath
	})
	targets := make([]moduleTarget, 0, len(locations))
	for _, location := range locations {
		targets = append(targets, moduleTarget{rootKey: location.RootKey, location: location.CanonicalPath, options: ModuleOptions{
			Path: location.CanonicalPath, ExactLocation: location.CanonicalPath, IncludeTests: includeTests, ExistingOnly: true, Reason: storage.ReasonReindex,
		}})
	}
	return targets, nil
}
