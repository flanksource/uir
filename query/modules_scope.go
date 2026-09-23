package query

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func rootSelector(predicates []Predicate, optionRoot string) (string, []Predicate, error) {
	rootKey := optionRoot
	nodePredicates := make([]Predicate, 0, len(predicates))
	for _, predicate := range predicates {
		if predicate.Field != "root" {
			nodePredicates = append(nodePredicates, predicate)
			continue
		}
		if predicate.Value == "" {
			return "", nil, errors.New("root predicate cannot be empty")
		}
		if rootKey != "" && rootKey != predicate.Value {
			return "", nil, fmt.Errorf("root %q conflicts with root %q from the query", rootKey, predicate.Value)
		}
		rootKey = predicate.Value
	}
	return rootKey, nodePredicates, nil
}

func lookupError(err error, subject string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%s was not found", subject)
	}
	return fmt.Errorf("load %s: %w", subject, err)
}

func (pipeline *Pipeline) moduleScopes(ctx context.Context, options ModuleScopeOptions, allHeads bool) ([]moduleScope, error) {
	if options.SnapshotID != "" {
		id, err := uuid.Parse(options.SnapshotID)
		if err != nil {
			return nil, fmt.Errorf("invalid snapshot ID %q: %w", options.SnapshotID, err)
		}
		var snapshot storage.ModuleSnapshot
		if err := pipeline.database.WithContext(ctx).Where("id = ?", id).First(&snapshot).Error; err != nil {
			return nil, lookupError(err, fmt.Sprintf("snapshot %q", options.SnapshotID))
		}
		scope, err := pipeline.moduleScopeForSnapshot(ctx, snapshot)
		if err != nil {
			return nil, err
		}
		if options.RootKey != "" && scope.root.RootKey != options.RootKey {
			return nil, fmt.Errorf("snapshot %s belongs to root %q, not %q", id, scope.root.RootKey, options.RootKey)
		}
		return []moduleScope{scope}, nil
	}
	if options.Location != "" {
		path, err := canonicalLocation(options.Location)
		if err != nil {
			return nil, err
		}
		query := pipeline.database.WithContext(ctx).Where("canonical_path = ?", path)
		if options.RootKey != "" {
			var root storage.ModuleRoot
			if err := pipeline.database.WithContext(ctx).Where("root_key = ?", options.RootKey).First(&root).Error; err != nil {
				return nil, lookupError(err, fmt.Sprintf("root %q", options.RootKey))
			}
			query = query.Where("root_id = ?", root.ID)
		}
		var locations []storage.ModuleLocation
		if err := query.Find(&locations).Error; err != nil {
			return nil, fmt.Errorf("load location %q: %w", path, err)
		}
		if len(locations) != 1 {
			return nil, fmt.Errorf("location %q matched %d module roots; select --root", path, len(locations))
		}
		var head storage.ModuleLocationHead
		if err := pipeline.database.WithContext(ctx).Where("location_id = ?", locations[0].ID).First(&head).Error; err != nil {
			return nil, lookupError(err, fmt.Sprintf("head for location %q", path))
		}
		return pipeline.scopesFromHeads(ctx, []storage.ModuleLocationHead{head})
	}
	query := pipeline.database.WithContext(ctx).Model(&storage.ModuleRoot{}).Order("root_key")
	if options.RootKey != "" {
		query = query.Where("root_key = ?", options.RootKey)
	}
	var roots []storage.ModuleRoot
	if err := query.Find(&roots).Error; err != nil {
		return nil, fmt.Errorf("load module roots: %w", err)
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("no indexed module roots match %q", options.RootKey)
	}
	var heads []storage.ModuleLocationHead
	for _, root := range roots {
		if allHeads {
			var rootHeads []storage.ModuleLocationHead
			if err := pipeline.database.WithContext(ctx).Where("root_id = ?", root.ID).Order("location_id").Find(&rootHeads).Error; err != nil {
				return nil, fmt.Errorf("load heads for root %q: %w", root.RootKey, err)
			}
			if len(rootHeads) == 0 {
				return nil, fmt.Errorf("root %q has no published heads", root.RootKey)
			}
			heads = append(heads, rootHeads...)
			continue
		}
		var primary storage.ModulePrimary
		if err := pipeline.database.WithContext(ctx).Where("root_id = ?", root.ID).First(&primary).Error; err != nil {
			return nil, lookupError(err, fmt.Sprintf("primary location for root %q", root.RootKey))
		}
		var head storage.ModuleLocationHead
		if err := pipeline.database.WithContext(ctx).Where("location_id = ?", primary.LocationID).First(&head).Error; err != nil {
			return nil, lookupError(err, fmt.Sprintf("primary head for root %q", root.RootKey))
		}
		heads = append(heads, head)
	}
	return pipeline.scopesFromHeads(ctx, heads)
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
	if snapshot.State != storage.SnapshotReady {
		return moduleScope{}, fmt.Errorf("snapshot %s is %q, expected ready", snapshot.ID, snapshot.State)
	}
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
