package indexer

import (
	"context"
	"fmt"
	"sort"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// sourceRevisions returns the content-addressed revision of every discovered file, reusing the base
// snapshot's revision when the bytes and package are unchanged and inserting the rest idempotently.
func sourceRevisions(ctx context.Context, database *gorm.DB, rootID uuid.UUID, files []discoveredFile, previous map[string]storage.SourceRevision) (map[string]storage.SourceRevision, error) {
	current := make(map[string]storage.SourceRevision, len(files))
	for _, file := range files {
		prior := previous[file.PathKey]
		if prior.ID != uuid.Nil && prior.ContentHash == file.ContentHash && prior.PackagePath == file.PackagePath {
			current[file.PathKey] = prior
			continue
		}
		revision := storage.SourceRevision{
			ID: uuid.New(), RootID: rootID, PathKey: file.PathKey, ContentHash: file.ContentHash,
			PackagePath: file.PackagePath, SizeBytes: file.SizeBytes,
		}
		if err := database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&revision).Error; err != nil {
			return nil, fmt.Errorf("create source revision %q: %w", file.PathKey, err)
		}
		revision = storage.SourceRevision{}
		if err := database.WithContext(ctx).Where(
			"root_id = ? AND path_key = ? AND content_hash = ? AND package_path = ?",
			rootID, file.PathKey, file.ContentHash, file.PackagePath,
		).First(&revision).Error; err != nil {
			return nil, fmt.Errorf("load published source revision %q: %w", file.PathKey, err)
		}
		current[file.PathKey] = revision
	}
	return current, nil
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
