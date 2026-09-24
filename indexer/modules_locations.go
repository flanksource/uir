package indexer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func requireRegisteredLocation(ctx context.Context, database *gorm.DB, discovered discoveredRoot) error {
	var registered storage.ModuleLocation
	err := database.WithContext(ctx).Table("locations AS location").
		Select("location.*").Joins("JOIN modules AS root ON root.id = location.root_id").
		Where("root.root_key = ? AND location.canonical_path = ?", discovered.RootKey, discovered.LocalPath).
		First(&registered).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("module %q at %q is not registered", discovered.RootKey, discovered.LocalPath)
	}
	if err != nil {
		return fmt.Errorf("load registered module location: %w", err)
	}
	return nil
}

// moduleBase is the location's locked head, when it has one, and the snapshot a new snapshot bases on:
// the location's own head, else the primary location's head, else none.
type moduleBase struct {
	head     storage.ModuleLocationHead
	hasHead  bool
	snapshot storage.ModuleSnapshot
}

func loadModuleBase(ctx context.Context, database *gorm.DB, root storage.ModuleRoot, location storage.ModuleLocation) (moduleBase, error) {
	var base moduleBase
	err := database.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("location_id = ?", location.ID).First(&base.head).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return moduleBase{}, fmt.Errorf("load location head: %w", err)
	}
	base.hasHead = err == nil
	baseID := base.head.SnapshotID
	if !base.hasHead {
		var primary storage.ModulePrimary
		if err := database.WithContext(ctx).Where("root_id = ?", root.ID).First(&primary).Error; err != nil {
			return moduleBase{}, fmt.Errorf("load primary location: %w", err)
		}
		if primary.LocationID != location.ID {
			var primaryHead storage.ModuleLocationHead
			if err := database.WithContext(ctx).Where("location_id = ?", primary.LocationID).First(&primaryHead).Error; err != nil {
				return moduleBase{}, fmt.Errorf("load primary head: %w", err)
			}
			baseID = primaryHead.SnapshotID
		}
	}
	if baseID == uuid.Nil {
		return base, nil
	}
	if err := database.WithContext(ctx).Where("id = ?", baseID).First(&base.snapshot).Error; err != nil {
		return moduleBase{}, fmt.Errorf("load base snapshot %s: %w", baseID, err)
	}
	if base.snapshot.RootID != root.ID {
		return moduleBase{}, fmt.Errorf("base snapshot %s belongs to root %s, expected %s", baseID, base.snapshot.RootID, root.ID)
	}
	return base, nil
}

// advanceHead points the location's head at the snapshot with a compare-and-swap on its version.
func advanceHead(ctx context.Context, database *gorm.DB, base moduleBase, snapshot storage.ModuleSnapshot) (int64, error) {
	if !base.hasHead {
		head := storage.ModuleLocationHead{RootID: snapshot.RootID, LocationID: snapshot.LocationID, SnapshotID: snapshot.ID, Version: 1}
		if err := database.WithContext(ctx).Create(&head).Error; err != nil {
			return 0, fmt.Errorf("publish first module location head: %w", err)
		}
		return head.Version, nil
	}
	version := base.head.Version + 1
	updated := database.WithContext(ctx).Model(&storage.ModuleLocationHead{}).
		Where("location_id = ? AND version = ?", snapshot.LocationID, base.head.Version).
		Updates(map[string]any{"snapshot_id": snapshot.ID, "version": version})
	if updated.Error != nil {
		return 0, fmt.Errorf("publish module location head: %w", updated.Error)
	}
	if updated.RowsAffected != 1 {
		return 0, fmt.Errorf("location %s head changed during indexing", snapshot.LocationID)
	}
	return version, nil
}

func ensureModuleLocation(ctx context.Context, database *gorm.DB, discovered discoveredRoot, locations map[string]storage.ModuleLocation) (storage.ModuleRoot, storage.ModuleLocation, error) {
	now := time.Now().UTC()
	root := storage.ModuleRoot{ID: uuid.New(), RootKey: discovered.RootKey, Name: filepath.Base(discovered.RootKey), CreatedAt: now}
	if err := database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&root).Error; err != nil {
		return storage.ModuleRoot{}, storage.ModuleLocation{}, fmt.Errorf("create module root: %w", err)
	}
	root = storage.ModuleRoot{}
	if err := database.WithContext(ctx).Where("root_key = ?", discovered.RootKey).First(&root).Error; err != nil {
		return storage.ModuleRoot{}, storage.ModuleLocation{}, fmt.Errorf("load module root: %w", err)
	}
	location := storage.ModuleLocation{
		ID: uuid.New(), RootID: root.ID, CanonicalPath: discovered.LocalPath,
		MountPath: discovered.MountPath, Kind: discovered.Kind, CreatedAt: now,
	}
	if discovered.RepositoryURI != "" {
		location.RepositoryURI = &discovered.RepositoryURI
	}
	if discovered.ParentRootKey != "" {
		parent, exists := locations[discovered.ParentRootKey]
		if !exists {
			return storage.ModuleRoot{}, storage.ModuleLocation{}, fmt.Errorf("parent module location %q is missing", discovered.ParentRootKey)
		}
		location.ParentLocationID = &parent.ID
	}
	if err := database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&location).Error; err != nil {
		return storage.ModuleRoot{}, storage.ModuleLocation{}, fmt.Errorf("create module location: %w", err)
	}
	location = storage.ModuleLocation{}
	if err := database.WithContext(ctx).Where("root_id = ? AND canonical_path = ?", root.ID, discovered.LocalPath).First(&location).Error; err != nil {
		return storage.ModuleRoot{}, storage.ModuleLocation{}, fmt.Errorf("load module location: %w", err)
	}
	primary := storage.ModulePrimary{RootID: root.ID, LocationID: location.ID}
	if err := database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&primary).Error; err != nil {
		return storage.ModuleRoot{}, storage.ModuleLocation{}, fmt.Errorf("set primary module location: %w", err)
	}
	return root, location, nil
}
