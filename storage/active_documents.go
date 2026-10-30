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
	// source revision, coverage, digest, and ordinal, so a reader can find the paths it needs before it
	// reads any content, then load those with DocumentContents.
	Content bool
}

const documentLookupBatch = 256

// effectiveDocumentsSQL returns the first document delta per path (newest link first), tombstones included.
var effectiveDocumentsSQL = baseChainSQL + fmt.Sprintf(`,
ranked AS (
  SELECT d.path_key, d.document_ordinal, d.operation,
         ROW_NUMBER() OVER (PARTITION BY d.path_key ORDER BY c.depth) AS path_rank
  FROM %[1]s d JOIN chain c ON c.ordinal = d.snapshot_ordinal
)
SELECT t.depth AS tail_depth, t.base_id AS tail_base_id, r.root_count,
       e.path_key, e.document_ordinal, e.operation
FROM tail t CROSS JOIN roots r LEFT JOIN ranked e ON e.path_rank = 1`, DocumentDelta{}.TableName())

type effectiveDocumentRow struct {
	TailDepth       int
	TailBaseID      *uuid.UUID
	RootCount       int
	PathKey         *string
	DocumentOrdinal *int64
	Operation       *SourceOperation
}

// ActiveDocuments resolves a snapshot's path_key → document membership: one recursive query takes the
// newest document delta per path along the base chain, tombstones drop their path, and the bound
// documents are loaded by ordinal in batches. Every path the effective sources serve must be bound to a
// document of that root and path describing its effective revision, and the snapshot's package coverage
// must account for every file; any gap is an error.
func ActiveDocuments(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID, options ActiveDocumentOptions) (map[string]ActiveDocument, error) {
	if database == nil {
		return nil, fmt.Errorf("load active documents for snapshot %s: database is required", snapshotID)
	}
	sources, err := EffectiveSources(ctx, database, EffectiveSourcesOptions{SnapshotID: snapshotID})
	if err != nil {
		return nil, err
	}
	var snapshot ModuleSnapshot
	if err := database.WithContext(ctx).Where("id = ?", snapshotID).First(&snapshot).Error; err != nil {
		return nil, fmt.Errorf("load snapshot %s: %w", snapshotID, err)
	}
	if err := checkPackageCoverage(ctx, database, snapshot, sources); err != nil {
		return nil, err
	}
	bindings, err := EffectiveDocumentOrdinals(ctx, database, snapshotID)
	if err != nil {
		return nil, err
	}
	for path := range sources {
		if _, bound := bindings[path]; !bound {
			return nil, fmt.Errorf("snapshot %s has no document for %q", snapshotID, path)
		}
	}
	return loadBoundDocuments(ctx, database, boundDocuments{snapshot: snapshot, sources: sources, bindings: bindings, content: options.Content})
}

// EffectiveDocumentOrdinals is a snapshot's path → document ordinal membership along its base chain,
// without loading any document. A base cycle, a dangling base, a chain spanning roots, and a missing
// snapshot are errors, as for EffectiveSources.
func EffectiveDocumentOrdinals(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID) (map[string]int64, error) {
	var rows []effectiveDocumentRow
	if err := database.WithContext(ctx).Raw(effectiveDocumentsSQL, snapshotID, maxBaseChainDepth).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load effective document deltas for snapshot %s: %w", snapshotID, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("snapshot %s does not exist", snapshotID)
	}
	if err := (baseChain{TailDepth: rows[0].TailDepth, TailBaseID: rows[0].TailBaseID, RootCount: rows[0].RootCount}).check(snapshotID); err != nil {
		return nil, err
	}
	bindings := make(map[string]int64, len(rows))
	for _, row := range rows {
		switch {
		case row.PathKey == nil:
		case row.Operation == nil:
			return nil, fmt.Errorf("document delta %q of snapshot %s chain has no operation", *row.PathKey, snapshotID)
		case *row.Operation == SourceSet && row.DocumentOrdinal != nil:
			bindings[*row.PathKey] = *row.DocumentOrdinal
		case *row.Operation != SourceDelete || row.DocumentOrdinal != nil:
			return nil, fmt.Errorf("document delta %q of snapshot %s chain has operation %q with document %v", *row.PathKey, snapshotID, *row.Operation, row.DocumentOrdinal)
		}
	}
	return bindings, nil
}

// boundDocuments is a snapshot's effective sources and document bindings, which must name the same paths.
type boundDocuments struct {
	snapshot ModuleSnapshot
	sources  map[string]SourceRevision
	bindings map[string]int64
	content  bool
}

// loadBoundDocuments loads the bound documents by ordinal, with or without content, and checks each
// against its path's binding and effective revision.
func loadBoundDocuments(ctx context.Context, database *gorm.DB, bound boundDocuments) (map[string]ActiveDocument, error) {
	if len(bound.bindings) != len(bound.sources) {
		return nil, fmt.Errorf("snapshot %s binds %d documents, its effective sources serve %d paths", bound.snapshot.ID, len(bound.bindings), len(bound.sources))
	}
	ordinals := make([]int64, 0, len(bound.bindings))
	paths := make(map[int64]string, len(bound.bindings))
	for path, ordinal := range bound.bindings {
		ordinals, paths[ordinal] = append(ordinals, ordinal), path
	}
	sort.Slice(ordinals, func(i, j int) bool { return ordinals[i] < ordinals[j] })
	result := make(map[string]ActiveDocument, len(ordinals))
	for start := 0; start < len(ordinals); start += documentLookupBatch {
		query := database.WithContext(ctx)
		if !bound.content {
			query = query.Omit("content")
		}
		var documents []Document
		if err := query.Where("ordinal IN ?", ordinals[start:min(start+documentLookupBatch, len(ordinals))]).Find(&documents).Error; err != nil {
			return nil, fmt.Errorf("load documents for snapshot %s: %w", bound.snapshot.ID, err)
		}
		for _, document := range documents {
			path := paths[document.Ordinal]
			source := bound.sources[path]
			if document.RootID != bound.snapshot.RootID || document.PathKey != path || document.SourceRevisionID != source.ID {
				return nil, fmt.Errorf("document %s bound to %q of snapshot %s describes %q at revision %s, the snapshot has revision %s",
					document.ID, path, bound.snapshot.ID, document.PathKey, document.SourceRevisionID, source.ID)
			}
			result[path] = ActiveDocument{Source: source, Document: document}
		}
	}
	if len(result) != len(ordinals) {
		return nil, fmt.Errorf("snapshot %s binds %d documents, %d exist", bound.snapshot.ID, len(ordinals), len(result))
	}
	return result, nil
}

// checkPackageCoverage checks that the snapshot's package coverage rows, the analysis record of each
// package, account for exactly its effective packages and their files.
func checkPackageCoverage(ctx context.Context, database *gorm.DB, snapshot ModuleSnapshot, sources map[string]SourceRevision) error {
	var rows []PackageCoverage
	if err := database.WithContext(ctx).Select("root_id", "package_path", "file_count").Where("snapshot_id = ?", snapshot.ID).Find(&rows).Error; err != nil {
		return fmt.Errorf("load package coverage for snapshot %s: %w", snapshot.ID, err)
	}
	files := map[string]int{}
	for _, source := range sources {
		files[source.PackagePath]++
	}
	if len(rows) != len(files) || len(rows) != snapshot.PackageCount {
		return fmt.Errorf("snapshot %s has %d package coverage rows, %d effective packages, and package_count %d", snapshot.ID, len(rows), len(files), snapshot.PackageCount)
	}
	for _, row := range rows {
		if row.RootID != snapshot.RootID || files[row.PackagePath] != row.FileCount {
			return fmt.Errorf("package coverage for %q in snapshot %s records %d files, effective sources have %d", row.PackagePath, snapshot.ID, row.FileCount, files[row.PackagePath])
		}
	}
	return nil
}
