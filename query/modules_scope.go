package query

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func lookupError(err error, subject string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%s was not found", subject)
	}
	return fmt.Errorf("load %s: %w", subject, err)
}

// headError reports a missing head of a registered root or location, which only the pre-handle cutover
// leaves behind, with the command that indexes it again.
func headError(err error, subject string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return unindexedError(subject)
	}
	return fmt.Errorf("load head of %s: %w", subject, err)
}

func unindexedError(subject string) error {
	return fmt.Errorf("%s is registered but not indexed; run `uir reindex` on its checkout", subject)
}

type MissingHeadWarning struct {
	RootKey  string `json:"root_key"`
	Location string `json:"location"`
	Message  string `json:"message"`
}

type ItemsWithWarnings[T any] struct {
	Items    []T                  `json:"items"`
	Warnings []MissingHeadWarning `json:"warnings"`
}

type moduleScopeSelection struct {
	scopes   []moduleScope
	warnings []MissingHeadWarning
}

func missingHeadWarning(root, location string) MissingHeadWarning {
	return MissingHeadWarning{RootKey: root, Location: location, Message: "registered checkout has no indexed head; run `uir reindex --all`"}
}

// moduleScopes selects the snapshots a query reads. A selection by root or snapshot is kept for the
// cache generation, since only a publication or registration changes it; a selection by location
// reads the filesystem to canonicalize the path, so it is made every time.
func (pipeline *Pipeline) moduleScopes(ctx context.Context, options ModuleScopeOptions, allHeads bool) (moduleScopeSelection, error) {
	generation, err := pipeline.generation(ctx)
	if err != nil {
		return moduleScopeSelection{}, err
	}
	if options.Location != "" {
		return pipeline.selectModuleScopes(ctx, options, allHeads)
	}
	key := selectionKey{rootKey: options.RootKey, snapshotID: options.SnapshotID, allHeads: allHeads}
	if selection, found := generation.selection(key); found {
		return selection, nil
	}
	selection, err := pipeline.selectModuleScopes(ctx, options, allHeads)
	if err != nil {
		return moduleScopeSelection{}, err
	}
	generation.keepSelection(key, selection)
	return selection, nil
}

func (pipeline *Pipeline) selectModuleScopes(ctx context.Context, options ModuleScopeOptions, allHeads bool) (moduleScopeSelection, error) {
	if options.SnapshotID != "" {
		id, err := uuid.Parse(options.SnapshotID)
		if err != nil {
			return moduleScopeSelection{}, fmt.Errorf("invalid snapshot ID %q: %w", options.SnapshotID, err)
		}
		var snapshot storage.ModuleSnapshot
		if err := pipeline.database.WithContext(ctx).Where("id = ?", id).First(&snapshot).Error; err != nil {
			return moduleScopeSelection{}, lookupError(err, fmt.Sprintf("snapshot %q", options.SnapshotID))
		}
		scope, err := pipeline.moduleScopeForSnapshot(ctx, snapshot)
		if err != nil {
			return moduleScopeSelection{}, err
		}
		if options.RootKey != "" && scope.root.RootKey != options.RootKey {
			return moduleScopeSelection{}, fmt.Errorf("snapshot %s belongs to root %q, not %q", id, scope.root.RootKey, options.RootKey)
		}
		return moduleScopeSelection{scopes: []moduleScope{scope}, warnings: []MissingHeadWarning{}}, nil
	}
	if options.Location != "" {
		path, err := canonicalLocation(options.Location)
		if err != nil {
			return moduleScopeSelection{}, err
		}
		query := pipeline.database.WithContext(ctx).Where("canonical_path = ?", path)
		if options.RootKey != "" {
			var root storage.ModuleRoot
			if err := pipeline.database.WithContext(ctx).Where("root_key = ?", options.RootKey).First(&root).Error; err != nil {
				return moduleScopeSelection{}, lookupError(err, fmt.Sprintf("root %q", options.RootKey))
			}
			query = query.Where("root_id = ?", root.ID)
		}
		var locations []storage.ModuleLocation
		if err := query.Find(&locations).Error; err != nil {
			return moduleScopeSelection{}, fmt.Errorf("load location %q: %w", path, err)
		}
		if len(locations) != 1 {
			return moduleScopeSelection{}, fmt.Errorf("location %q matched %d module roots; select --root", path, len(locations))
		}
		var head storage.ModuleLocationHead
		if err := pipeline.database.WithContext(ctx).Where("location_id = ?", locations[0].ID).First(&head).Error; err != nil {
			return moduleScopeSelection{}, headError(err, fmt.Sprintf("location %q", path))
		}
		scopes, err := pipeline.scopesFromHeads(ctx, []storage.ModuleLocationHead{head})
		return moduleScopeSelection{scopes: scopes, warnings: []MissingHeadWarning{}}, err
	}
	return pipeline.broadModuleScopes(ctx, options.RootKey, allHeads)
}

func (pipeline *Pipeline) broadModuleScopes(ctx context.Context, rootKey string, allHeads bool) (moduleScopeSelection, error) {
	query := pipeline.database.WithContext(ctx).Model(&storage.ModuleRoot{}).Order("root_key")
	if rootKey != "" {
		query = query.Where("root_key = ?", rootKey)
	}
	var roots []storage.ModuleRoot
	if err := query.Find(&roots).Error; err != nil {
		return moduleScopeSelection{}, fmt.Errorf("load module roots: %w", err)
	}
	if len(roots) == 0 {
		return moduleScopeSelection{}, fmt.Errorf("no indexed module roots match %q", rootKey)
	}
	var heads []storage.ModuleLocationHead
	selection := moduleScopeSelection{warnings: []MissingHeadWarning{}}
	for _, root := range roots {
		if allHeads {
			var locations []storage.ModuleLocation
			if err := pipeline.database.WithContext(ctx).Where("root_id = ?", root.ID).Order("canonical_path").Find(&locations).Error; err != nil {
				return moduleScopeSelection{}, fmt.Errorf("load locations for root %q: %w", root.RootKey, err)
			}
			if len(locations) == 0 {
				return moduleScopeSelection{}, fmt.Errorf("root %q has no registered locations", root.RootKey)
			}
			var rootHeads []storage.ModuleLocationHead
			if err := pipeline.database.WithContext(ctx).Where("root_id = ?", root.ID).Order("location_id").Find(&rootHeads).Error; err != nil {
				return moduleScopeSelection{}, fmt.Errorf("load heads for root %q: %w", root.RootKey, err)
			}
			byLocation := make(map[uuid.UUID]storage.ModuleLocationHead, len(rootHeads))
			for _, head := range rootHeads {
				byLocation[head.LocationID] = head
			}
			for _, location := range locations {
				if head, found := byLocation[location.ID]; found {
					heads = append(heads, head)
				} else {
					selection.warnings = append(selection.warnings, missingHeadWarning(root.RootKey, location.CanonicalPath))
				}
			}
			continue
		}
		var primary storage.ModulePrimary
		if err := pipeline.database.WithContext(ctx).Where("root_id = ?", root.ID).First(&primary).Error; err != nil {
			return moduleScopeSelection{}, lookupError(err, fmt.Sprintf("primary location for root %q", root.RootKey))
		}
		var head storage.ModuleLocationHead
		if err := pipeline.database.WithContext(ctx).Where("location_id = ?", primary.LocationID).First(&head).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) || rootKey != "" {
				return moduleScopeSelection{}, headError(err, fmt.Sprintf("root %q", root.RootKey))
			}
			var location storage.ModuleLocation
			if err := pipeline.database.WithContext(ctx).Where("id = ? AND root_id = ?", primary.LocationID, root.ID).First(&location).Error; err != nil {
				return moduleScopeSelection{}, lookupError(err, fmt.Sprintf("primary location for root %q", root.RootKey))
			}
			selection.warnings = append(selection.warnings, missingHeadWarning(root.RootKey, location.CanonicalPath))
			continue
		}
		heads = append(heads, head)
	}
	var err error
	selection.scopes, err = pipeline.scopesFromHeads(ctx, heads)
	return selection, err
}

func (pipeline *Pipeline) scopesFromHeads(ctx context.Context, heads []storage.ModuleLocationHead) ([]moduleScope, error) {
	scopes := make([]moduleScope, 0, len(heads))
	for _, head := range heads {
		var snapshot storage.ModuleSnapshot
		if err := pipeline.database.WithContext(ctx).Where("id = ?", head.SnapshotID).First(&snapshot).Error; err != nil {
			return nil, lookupError(err, fmt.Sprintf("head snapshot %s", head.SnapshotID))
		}
		if snapshot.LocationID != head.LocationID || snapshot.RootID != head.RootID {
			return nil, fmt.Errorf("head for location %s points to snapshot in another location or root", head.LocationID)
		}
		scope, err := pipeline.moduleScopeForSnapshot(ctx, snapshot)
		if err != nil {
			return nil, err
		}
		scopes = append(scopes, scope)
	}
	return scopes, nil
}

func (pipeline *Pipeline) moduleScopeForSnapshot(ctx context.Context, snapshot storage.ModuleSnapshot) (moduleScope, error) {
	var root storage.ModuleRoot
	if err := pipeline.database.WithContext(ctx).Where("id = ?", snapshot.RootID).First(&root).Error; err != nil {
		return moduleScope{}, lookupError(err, fmt.Sprintf("root for snapshot %s", snapshot.ID))
	}
	var location storage.ModuleLocation
	if err := pipeline.database.WithContext(ctx).Where("id = ? AND root_id = ?", snapshot.LocationID, root.ID).First(&location).Error; err != nil {
		return moduleScope{}, lookupError(err, fmt.Sprintf("location for snapshot %s", snapshot.ID))
	}
	return moduleScope{root: root, location: location, snapshot: snapshot}, nil
}
