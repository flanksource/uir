package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func sourceRevision(ctx context.Context, database *gorm.DB, rootID uuid.UUID, file discoveredFile, previous storage.SourceRevision, force bool) (storage.SourceRevision, bool, error) {
	if !force && previous.ID != uuid.Nil && previous.ContentHash == file.ContentHash && previous.PackagePath == file.PackagePath && previous.ExtractorVersion == ExtractorVersion {
		return previous, false, nil
	}
	query := func() *gorm.DB {
		return database.WithContext(ctx).Where(
			"root_id = ? AND path_key = ? AND content_hash = ? AND package_path = ? AND extractor_version = ?",
			rootID, file.PathKey, file.ContentHash, file.PackagePath, ExtractorVersion,
		)
	}
	var revision storage.SourceRevision
	err := query().First(&revision).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return storage.SourceRevision{}, false, fmt.Errorf("load source revision %q: %w", file.PathKey, err)
	}
	if err == nil && !force {
		return revision, false, nil
	}
	projection, err := extractGoFile(file.PathKey, file.PackagePath, file.Content)
	if err != nil {
		return storage.SourceRevision{}, false, err
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return storage.SourceRevision{}, false, fmt.Errorf("encode source projection %q: %w", file.PathKey, err)
	}
	if revision.ID != uuid.Nil {
		var cached, fresh any
		if err := json.Unmarshal(revision.Projection, &cached); err != nil {
			return storage.SourceRevision{}, false, fmt.Errorf("decode existing source projection %q: %w", file.PathKey, err)
		}
		if err := json.Unmarshal(encoded, &fresh); err != nil {
			return storage.SourceRevision{}, false, fmt.Errorf("decode extracted source projection %q: %w", file.PathKey, err)
		}
		if !reflect.DeepEqual(cached, fresh) {
			return storage.SourceRevision{}, false, fmt.Errorf("source projection for %q changed without an extractor version change", file.PathKey)
		}
		return revision, true, nil
	}
	revision = storage.SourceRevision{
		ID: uuid.New(), RootID: rootID, PathKey: file.PathKey, ContentHash: file.ContentHash,
		PackagePath: file.PackagePath, ExtractorVersion: ExtractorVersion,
		SizeBytes: file.SizeBytes, Projection: storage.JSON(encoded),
	}
	if err := database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&revision).Error; err != nil {
		return storage.SourceRevision{}, false, fmt.Errorf("create source revision %q: %w", file.PathKey, err)
	}
	revision = storage.SourceRevision{}
	if err := query().First(&revision).Error; err != nil {
		return storage.SourceRevision{}, false, fmt.Errorf("load published source revision %q: %w", file.PathKey, err)
	}
	return revision, true, nil
}

func createSourceDeltas(ctx context.Context, database *gorm.DB, snapshot storage.ModuleSnapshot, previous, current map[string]storage.SourceRevision) error {
	deltas := make([]storage.SourceDelta, 0, len(previous)+len(current))
	for path, revision := range current {
		if prior, exists := previous[path]; exists && prior.ID == revision.ID {
			continue
		}
		id := revision.ID
		deltas = append(deltas, storage.SourceDelta{
			SnapshotID: snapshot.ID, RootID: snapshot.RootID, PathKey: path,
			RevisionID: &id, Operation: storage.SourceSet,
		})
	}
	for path := range previous {
		if _, exists := current[path]; exists {
			continue
		}
		deltas = append(deltas, storage.SourceDelta{
			SnapshotID: snapshot.ID, RootID: snapshot.RootID, PathKey: path, Operation: storage.SourceDelete,
		})
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].PathKey < deltas[j].PathKey })
	if len(deltas) == 0 {
		return nil
	}
	if err := database.WithContext(ctx).CreateInBatches(deltas, 128).Error; err != nil {
		return fmt.Errorf("create %d source deltas for snapshot %s: %w", len(deltas), snapshot.ID, err)
	}
	return nil
}
