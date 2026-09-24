package query

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/flanksource/uir/storage"
)

type ModuleHeadFiles struct {
	RootKey    string             `json:"root_key"`
	Name       string             `json:"name"`
	Location   string             `json:"location"`
	SnapshotID string             `json:"snapshot_id"`
	Sources    []ModuleSourceView `json:"sources"`
}

func (pipeline *Pipeline) BrowseModuleHeads(ctx context.Context) ([]ModuleHeadFiles, error) {
	if pipeline == nil || pipeline.database == nil {
		return nil, errors.New("UIR query database is required")
	}
	var rootCount int64
	if err := pipeline.database.WithContext(ctx).Model(&storage.ModuleRoot{}).Count(&rootCount).Error; err != nil {
		return nil, fmt.Errorf("count module roots: %w", err)
	}
	if rootCount == 0 {
		return []ModuleHeadFiles{}, nil
	}
	scopes, err := pipeline.moduleScopes(ctx, ModuleScopeOptions{}, true)
	if err != nil {
		return nil, err
	}
	sort.Slice(scopes, func(i, j int) bool {
		if scopes[i].root.RootKey != scopes[j].root.RootKey {
			return scopes[i].root.RootKey < scopes[j].root.RootKey
		}
		return scopes[i].location.CanonicalPath < scopes[j].location.CanonicalPath
	})
	result := make([]ModuleHeadFiles, 0, len(scopes))
	for _, scope := range scopes {
		revisions, err := storage.EffectiveSources(ctx, pipeline.database, scope.snapshot.ID)
		if err != nil {
			return nil, fmt.Errorf("load head files for %s at %s: %w", scope.root.RootKey, scope.location.CanonicalPath, err)
		}
		paths := make([]string, 0, len(revisions))
		for path := range revisions {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		head := ModuleHeadFiles{RootKey: scope.root.RootKey, Name: scope.root.Name, Location: scope.location.CanonicalPath,
			SnapshotID: scope.snapshot.ID.String(), Sources: make([]ModuleSourceView, 0, len(paths))}
		for _, path := range paths {
			head.Sources = append(head.Sources, moduleSourceView(scope, path, revisions[path]))
		}
		result = append(result, head)
	}
	return result, nil
}
