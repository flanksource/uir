package storage

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ActiveDocument is the document a snapshot activates for one path, with the source revision it describes.
type ActiveDocument struct {
	Source   SourceRevision
	Document Document
}

// ActiveDocumentOptions selects what ActiveDocuments loads with each document row.
type ActiveDocumentOptions struct {
	// Content loads each document's content. Without it the documents carry only their membership,
	// source revision, coverage, and ordinal, so a reader can find the paths it needs before it reads
	// any content, then load those with DocumentContents.
	Content bool
}

const documentLookupBatch = 256

// ActiveDocuments derives a snapshot's path_key → document membership: effective sources give each
// path's revision, the snapshot's package_coverage rows give each package's input hash, and each
// document is looked up by (root_id, path_key, input_hash). Any gap between the three is an error.
func ActiveDocuments(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID, options ActiveDocumentOptions) (map[string]ActiveDocument, error) {
	if database == nil {
		return nil, fmt.Errorf("load active documents for snapshot %s: database is required", snapshotID)
	}
	sources, err := EffectiveSources(ctx, database, snapshotID)
	if err != nil {
		return nil, err
	}
	var snapshot ModuleSnapshot
	if err := database.WithContext(ctx).Where("id = ?", snapshotID).First(&snapshot).Error; err != nil {
		return nil, fmt.Errorf("load snapshot %s: %w", snapshotID, err)
	}
	inputHashes, err := packageInputHashes(ctx, database, snapshot, sources)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(sources))
	for path := range sources {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := make(map[string]ActiveDocument, len(paths))
	for start := 0; start < len(paths); start += documentLookupBatch {
		batch := paths[start:min(start+documentLookupBatch, len(paths))]
		if err := addActiveDocuments(ctx, database, activeBatch{snapshot: snapshot, paths: batch, sources: sources, inputHashes: inputHashes, content: options.Content}, result); err != nil {
			return nil, err
		}
	}
	for _, path := range paths {
		if _, found := result[path]; !found {
			return nil, fmt.Errorf("snapshot %s has no document for %q under input hash %s", snapshotID, path, inputHashes[sources[path].PackagePath])
		}
	}
	return result, nil
}

// activeBatch is one batch of a snapshot's paths with what their document lookup needs.
type activeBatch struct {
	snapshot    ModuleSnapshot
	paths       []string
	sources     map[string]SourceRevision
	inputHashes map[string]string
	content     bool
}

// addActiveDocuments looks up one batch of paths' documents by (root_id, path_key), with or without
// content, and adds each one that matches its path's package input hash to result. The input hash is
// matched here rather than in SQL: `input_hash IN ?` beside `path_key IN ?` makes SQLite probe the
// unique index once per path × hash pair, which dominated cold queries.
func addActiveDocuments(ctx context.Context, database *gorm.DB, batch activeBatch, result map[string]ActiveDocument) error {
	query := database.WithContext(ctx)
	if !batch.content {
		query = query.Omit("content")
	}
	var documents []Document
	if err := query.Where("root_id = ? AND path_key IN ?", batch.snapshot.RootID, batch.paths).Find(&documents).Error; err != nil {
		return fmt.Errorf("load documents for snapshot %s: %w", batch.snapshot.ID, err)
	}
	for _, document := range documents {
		source := batch.sources[document.PathKey]
		if document.InputHash != batch.inputHashes[source.PackagePath] {
			continue
		}
		if document.SourceRevisionID != source.ID {
			return fmt.Errorf("document %s for %q describes revision %s, snapshot %s has %s", document.ID, document.PathKey, document.SourceRevisionID, batch.snapshot.ID, source.ID)
		}
		result[document.PathKey] = ActiveDocument{Source: source, Document: document}
	}
	return nil
}

func packageInputHashes(ctx context.Context, database *gorm.DB, snapshot ModuleSnapshot, sources map[string]SourceRevision) (map[string]string, error) {
	var rows []PackageCoverage
	if err := database.WithContext(ctx).Where("snapshot_id = ?", snapshot.ID).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load package coverage for snapshot %s: %w", snapshot.ID, err)
	}
	files := map[string]int{}
	for _, source := range sources {
		files[source.PackagePath]++
	}
	if len(rows) != len(files) || len(rows) != snapshot.PackageCount {
		return nil, fmt.Errorf("snapshot %s has %d package coverage rows, %d effective packages, and package_count %d", snapshot.ID, len(rows), len(files), snapshot.PackageCount)
	}
	hashes := make(map[string]string, len(rows))
	for _, row := range rows {
		if row.RootID != snapshot.RootID || files[row.PackagePath] != row.FileCount {
			return nil, fmt.Errorf("package coverage for %q in snapshot %s records %d files, effective sources have %d", row.PackagePath, snapshot.ID, row.FileCount, files[row.PackagePath])
		}
		hashes[row.PackagePath] = row.InputHash
	}
	return hashes, nil
}
