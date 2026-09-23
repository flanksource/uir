package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ModuleOptions struct {
	Path         string
	IncludeTests bool
	Force        bool
	ExistingOnly bool
}

type ModuleResult struct {
	RootKey     string `json:"root_key"`
	Location    string `json:"location"`
	SnapshotID  string `json:"snapshot_id"`
	HeadVersion int64  `json:"head_version"`
	Files       int    `json:"files"`
	ParsedFiles int    `json:"parsed_files"`
	ReusedFiles int    `json:"reused_files"`
	Unchanged   bool   `json:"unchanged"`
}

func (indexer *Indexer) IndexModules(ctx context.Context, options ModuleOptions) ([]ModuleResult, error) {
	if indexer == nil || indexer.database == nil {
		return nil, errors.New("UIR index database is required")
	}
	roots, err := discoverModules(ctx, options.Path, options.IncludeTests)
	if err != nil {
		return nil, err
	}
	configuration, err := json.Marshal(struct {
		ExtractorVersion string `json:"extractor_version"`
		IncludeTests     bool   `json:"include_tests"`
	}{ExtractorVersion: ExtractorVersion, IncludeTests: options.IncludeTests})
	if err != nil {
		return nil, fmt.Errorf("encode module index configuration: %w", err)
	}
	results := make([]ModuleResult, 0, len(roots))
	err = indexer.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		locations := make(map[string]storage.ModuleLocation, len(roots))
		for _, root := range roots {
			result, location, indexErr := indexModule(ctx, transaction, root, locations, hashBytes(configuration), options)
			if indexErr != nil {
				return fmt.Errorf("index module %q at %q: %w", root.RootKey, root.LocalPath, indexErr)
			}
			locations[root.LocalPath] = location
			results = append(results, result)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

func indexModule(ctx context.Context, database *gorm.DB, discovered discoveredRoot, locations map[string]storage.ModuleLocation, configurationHash string, options ModuleOptions) (ModuleResult, storage.ModuleLocation, error) {
	if options.ExistingOnly {
		var registered storage.ModuleLocation
		err := database.WithContext(ctx).Table("uir_module_locations AS location").
			Select("location.*").Joins("JOIN uir_module_roots AS root ON root.id = location.root_id").
			Where("root.root_key = ? AND location.canonical_path = ?", discovered.RootKey, discovered.LocalPath).
			First(&registered).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ModuleResult{}, storage.ModuleLocation{}, fmt.Errorf("module %q at %q is not registered", discovered.RootKey, discovered.LocalPath)
		}
		if err != nil {
			return ModuleResult{}, storage.ModuleLocation{}, fmt.Errorf("load registered module location: %w", err)
		}
	}
	root, location, err := ensureModuleLocation(ctx, database, discovered, locations)
	if err != nil {
		return ModuleResult{}, storage.ModuleLocation{}, err
	}
	result := ModuleResult{RootKey: root.RootKey, Location: location.CanonicalPath, Files: len(discovered.Files)}
	var head storage.ModuleLocationHead
	err = database.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("location_id = ?", location.ID).First(&head).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return ModuleResult{}, storage.ModuleLocation{}, fmt.Errorf("load location head: %w", err)
	}
	hasHead := err == nil
	baseID := uuid.Nil
	if hasHead {
		baseID = head.SnapshotID
	} else {
		var primary storage.ModulePrimary
		if err := database.WithContext(ctx).Where("root_id = ?", root.ID).First(&primary).Error; err != nil {
			return ModuleResult{}, storage.ModuleLocation{}, fmt.Errorf("load primary location: %w", err)
		}
		if primary.LocationID != location.ID {
			var primaryHead storage.ModuleLocationHead
			if err := database.WithContext(ctx).Where("location_id = ?", primary.LocationID).First(&primaryHead).Error; err != nil {
				return ModuleResult{}, storage.ModuleLocation{}, fmt.Errorf("load primary head: %w", err)
			}
			baseID = primaryHead.SnapshotID
		}
	}
	var base storage.ModuleSnapshot
	if baseID != uuid.Nil {
		if err := database.WithContext(ctx).Where("id = ?", baseID).First(&base).Error; err != nil {
			return ModuleResult{}, storage.ModuleLocation{}, fmt.Errorf("load base snapshot %s: %w", baseID, err)
		}
		if base.State != storage.SnapshotReady || base.RootID != root.ID {
			return ModuleResult{}, storage.ModuleLocation{}, fmt.Errorf("invalid base snapshot %s for root %s", baseID, root.ID)
		}
	}
	if hasHead && !options.Force && base.Revision == discovered.Revision && base.ContentSetHash == discovered.ContentSetHash && base.ConfigurationHash == configurationHash && base.ExtractorVersion == ExtractorVersion {
		result.SnapshotID, result.HeadVersion, result.ReusedFiles, result.Unchanged = base.ID.String(), head.Version, len(discovered.Files), true
		return result, location, nil
	}
	previous := map[string]storage.SourceRevision{}
	if baseID != uuid.Nil {
		previous, err = storage.EffectiveSources(ctx, database, baseID)
		if err != nil {
			return ModuleResult{}, storage.ModuleLocation{}, err
		}
	}
	current := make(map[string]storage.SourceRevision, len(discovered.Files))
	for _, file := range discovered.Files {
		revision, parsed, revisionErr := sourceRevision(ctx, database, root.ID, file, previous[file.PathKey], options.Force)
		if revisionErr != nil {
			return ModuleResult{}, storage.ModuleLocation{}, revisionErr
		}
		current[file.PathKey] = revision
		if parsed {
			result.ParsedFiles++
		} else {
			result.ReusedFiles++
		}
	}
	now := time.Now().UTC()
	snapshot := storage.ModuleSnapshot{
		ID: uuid.New(), RootID: root.ID, LocationID: location.ID, State: storage.SnapshotBuilding,
		Revision: discovered.Revision, ContentSetHash: discovered.ContentSetHash,
		ConfigurationHash: configurationHash, ExtractorVersion: ExtractorVersion, StartedAt: now,
	}
	if baseID != uuid.Nil {
		snapshot.BaseSnapshotID = &baseID
	}
	if err := database.WithContext(ctx).Create(&snapshot).Error; err != nil {
		return ModuleResult{}, storage.ModuleLocation{}, fmt.Errorf("create module snapshot: %w", err)
	}
	if err := createSourceDeltas(ctx, database, snapshot, previous, current); err != nil {
		return ModuleResult{}, storage.ModuleLocation{}, err
	}
	if err := database.WithContext(ctx).Model(&snapshot).Updates(map[string]any{"state": storage.SnapshotReady, "completed_at": now}).Error; err != nil {
		return ModuleResult{}, storage.ModuleLocation{}, fmt.Errorf("complete module snapshot: %w", err)
	}
	version := int64(1)
	if hasHead {
		version = head.Version + 1
	}
	if hasHead {
		updated := database.WithContext(ctx).Model(&storage.ModuleLocationHead{}).
			Where("location_id = ? AND version = ?", location.ID, head.Version).
			Updates(map[string]any{"snapshot_id": snapshot.ID, "version": version})
		if updated.Error != nil {
			return ModuleResult{}, storage.ModuleLocation{}, fmt.Errorf("publish module location head: %w", updated.Error)
		}
		if updated.RowsAffected != 1 {
			return ModuleResult{}, storage.ModuleLocation{}, fmt.Errorf("location %s head changed during indexing", location.ID)
		}
	} else {
		head = storage.ModuleLocationHead{RootID: root.ID, LocationID: location.ID, SnapshotID: snapshot.ID, Version: version}
		if err := database.WithContext(ctx).Create(&head).Error; err != nil {
			return ModuleResult{}, storage.ModuleLocation{}, fmt.Errorf("publish first module location head: %w", err)
		}
	}
	result.SnapshotID, result.HeadVersion = snapshot.ID.String(), version
	return result, location, nil
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
