package storage

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DocumentDeltaOptions is the membership a snapshot publishes against its base's, each a path →
// document ordinal map; Base is empty for a snapshot without a base.
type DocumentDeltaOptions struct {
	SnapshotOrdinal int64
	RootID          uuid.UUID
	Base, Current   map[string]int64
}

// CreateDocumentDeltas writes a set row for every path whose document differs from the base's or that
// the base does not serve, and a delete row for every path only the base serves, in path order and
// bounded batches. It returns how many rows it wrote.
func CreateDocumentDeltas(ctx context.Context, database *gorm.DB, options DocumentDeltaOptions) (int, error) {
	if options.Base == nil || options.Current == nil {
		return 0, fmt.Errorf("document deltas of snapshot ordinal %d: base and current membership are required, empty when there is none", options.SnapshotOrdinal)
	}
	var rows []DocumentDelta
	for path, ordinal := range options.Current {
		if prior, found := options.Base[path]; !found || prior != ordinal {
			bound := ordinal
			rows = append(rows, DocumentDelta{SnapshotOrdinal: options.SnapshotOrdinal, RootID: options.RootID, PathKey: path, DocumentOrdinal: &bound, Operation: SourceSet})
		}
	}
	for path := range options.Base {
		if _, served := options.Current[path]; !served {
			rows = append(rows, DocumentDelta{SnapshotOrdinal: options.SnapshotOrdinal, RootID: options.RootID, PathKey: path, Operation: SourceDelete})
		}
	}
	if len(rows) == 0 {
		return 0, nil
	}
	slices.SortFunc(rows, func(left, right DocumentDelta) int { return cmp.Compare(left.PathKey, right.PathKey) })
	if err := database.WithContext(ctx).CreateInBatches(rows, createBatch).Error; err != nil {
		return 0, fmt.Errorf("create %d document deltas for snapshot ordinal %d: %w", len(rows), options.SnapshotOrdinal, err)
	}
	return len(rows), nil
}

// DocumentKey is a document's key within its root: its path and content digest.
type DocumentKey struct {
	PathKey, Digest string
}

// DocumentsByKey loads, without content unless asked, the root's documents stored under the given
// keys, keyed by them; a key without a document is absent. Each batch probes the
// (root_id, path_key, content_digest) unique index with paired keys.
func DocumentsByKey(ctx context.Context, database *gorm.DB, rootID uuid.UUID, keys []DocumentKey, content bool) (map[DocumentKey]Document, error) {
	result := make(map[DocumentKey]Document, len(keys))
	for start := 0; start < len(keys); start += documentLookupBatch {
		batch := keys[start:min(start+documentLookupBatch, len(keys))]
		pairs := make([][]any, len(batch))
		for i, key := range batch {
			pairs[i] = []any{key.PathKey, key.Digest}
		}
		query := database.WithContext(ctx)
		if !content {
			query = query.Omit("content")
		}
		var documents []Document
		if err := query.Where("root_id = ? AND (path_key, content_digest) IN ?", rootID, pairs).Find(&documents).Error; err != nil {
			return nil, fmt.Errorf("look up %d documents of root %s by key: %w", len(batch), rootID, err)
		}
		for _, document := range documents {
			result[DocumentKey{PathKey: document.PathKey, Digest: *document.ContentDigest}] = document
		}
	}
	return result, nil
}
