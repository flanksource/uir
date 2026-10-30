package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/flanksource/commons/logger"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// inputHashDocumentKey is the unique (root_id, path_key, input_hash) key documents had before document
// membership, when a snapshot selected its documents by package input hash.
const inputHashDocumentKey = "documents_root_id_path_key_input_hash_key"

const (
	// digestBatch bounds the documents, content included, one digest backfill transaction reads.
	digestBatch = 64
	// membershipMemo bounds the legacy memberships the backfill keeps for descendants to diff against.
	membershipMemo = 32
)

// ErrDocumentMembershipCutover marks a database whose document membership cutover has not completed.
var ErrDocumentMembershipCutover = errors.New("database predates document membership; open it read-write once to complete the cutover")

// dropInputHashDocumentKey drops the input hash key from a SQLite database before migration, which
// refuses to drop an index there; PostgreSQL's migration drops it itself.
func dropInputHashDocumentKey(ctx context.Context, database *gorm.DB) error {
	if database.Name() != "sqlite" {
		return nil
	}
	if err := database.WithContext(ctx).Exec(`DROP INDEX IF EXISTS "` + inputHashDocumentKey + `"`).Error; err != nil {
		return fmt.Errorf("drop %s: %w", inputHashDocumentKey, err)
	}
	return nil
}

// cutOverDocumentMembership completes the document membership cutover after migration: an empty
// database is marked complete; otherwise every document gets its content digest and every snapshot the
// document deltas its package input hashes used to select, each in bounded, resumable transactions, and
// the marker is set complete last. An interrupted cutover resumes from the marker on the next open.
func cutOverDocumentMembership(ctx context.Context, database *gorm.DB) error {
	var markers []DocumentMembership
	if err := database.WithContext(ctx).Where("id = 1").Find(&markers).Error; err != nil {
		return fmt.Errorf("read the document membership cutover: %w", err)
	}
	if len(markers) == 1 && markers[0].Complete {
		return nil
	}
	marker := DocumentMembership{ID: 1}
	if len(markers) == 1 {
		marker = markers[0]
	} else {
		empty, err := emptyIndex(ctx, database)
		if err != nil {
			return err
		}
		marker.Complete = empty
		if err := database.WithContext(ctx).Create(&marker).Error; err != nil {
			return fmt.Errorf("start the document membership cutover: %w", err)
		}
		if empty {
			return nil
		}
	}
	logger.Infof("backfilling document digests and snapshot document membership")
	if err := backfillDocumentDigests(ctx, database, &marker); err != nil {
		return fmt.Errorf("document membership cutover: %w", err)
	}
	if err := backfillDocumentMembership(ctx, database, &marker); err != nil {
		return fmt.Errorf("document membership cutover: %w", err)
	}
	if err := database.WithContext(ctx).Model(&marker).Update("complete", true).Error; err != nil {
		return fmt.Errorf("complete the document membership cutover: %w", err)
	}
	return nil
}

// requireDocumentMembership refuses a database whose document membership cutover is missing or incomplete.
func requireDocumentMembership(ctx context.Context, database *gorm.DB) error {
	if !database.Migrator().HasTable(&DocumentMembership{}) {
		return ErrDocumentMembershipCutover
	}
	var complete []bool
	if err := database.WithContext(ctx).Model(&DocumentMembership{}).Where("id = 1").Pluck("complete", &complete).Error; err != nil {
		return fmt.Errorf("read the document membership cutover: %w", err)
	}
	if len(complete) != 1 || !complete[0] {
		return ErrDocumentMembershipCutover
	}
	return nil
}

func emptyIndex(ctx context.Context, database *gorm.DB) (bool, error) {
	for _, model := range []any{&Document{}, &ModuleSnapshot{}} {
		var count int64
		if err := database.WithContext(ctx).Model(model).Limit(1).Count(&count).Error; err != nil {
			return false, fmt.Errorf("count %T rows: %w", model, err)
		}
		if count > 0 {
			return false, nil
		}
	}
	return true, nil
}

// digestKey is a document key with its root, unique across a database.
type digestKey struct {
	root uuid.UUID
	key  DocumentKey
}

// backfillDocumentDigests digests every document past marker.DigestedThrough in ordinal order. Before
// the cutover a file whose package inputs changed got a new document even when its rendering did not,
// so a path can hold several documents with one digest: the earliest keeps it, the rest keep a null
// digest, their ordinal still bound by every snapshot that bound them.
func backfillDocumentDigests(ctx context.Context, database *gorm.DB, marker *DocumentMembership) error {
	seen, err := storedDigests(ctx, database)
	if err != nil {
		return err
	}
	for {
		var documents []Document
		if err := database.WithContext(ctx).Where("ordinal > ?", marker.DigestedThrough).Order("ordinal").Limit(digestBatch).Find(&documents).Error; err != nil {
			return fmt.Errorf("load documents to digest: %w", err)
		}
		if len(documents) == 0 {
			return nil
		}
		if err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, document := range documents {
				if err := digestStoredDocument(ctx, tx, document, seen); err != nil {
					return err
				}
			}
			marker.DigestedThrough = documents[len(documents)-1].Ordinal
			return tx.Model(marker).Update("digested_through", marker.DigestedThrough).Error
		}); err != nil {
			return fmt.Errorf("digest documents through ordinal %d: %w", documents[len(documents)-1].Ordinal, err)
		}
	}
}

func storedDigests(ctx context.Context, database *gorm.DB) (map[digestKey]bool, error) {
	var stored []Document
	if err := database.WithContext(ctx).Select("root_id", "path_key", "content_digest").Where("content_digest IS NOT NULL").Find(&stored).Error; err != nil {
		return nil, fmt.Errorf("load stored document digests: %w", err)
	}
	seen := make(map[digestKey]bool, len(stored))
	for _, document := range stored {
		seen[digestKey{root: document.RootID, key: DocumentKey{PathKey: document.PathKey, Digest: *document.ContentDigest}}] = true
	}
	return seen, nil
}

func digestStoredDocument(ctx context.Context, tx *gorm.DB, document Document, seen map[digestKey]bool) error {
	if document.ContentDigest != nil {
		return nil
	}
	digest, err := DocumentDigest(document)
	if err != nil {
		return err
	}
	key := digestKey{root: document.RootID, key: DocumentKey{PathKey: document.PathKey, Digest: digest}}
	if seen[key] {
		return nil
	}
	seen[key] = true
	if err := tx.WithContext(ctx).Model(&Document{}).Where("ordinal = ?", document.Ordinal).Update("content_digest", digest).Error; err != nil {
		return fmt.Errorf("record the digest of document %s: %w", document.ID, err)
	}
	return nil
}

// backfillDocumentMembership writes, for every snapshot past marker.MembersThrough in ordinal order, the
// document deltas of the membership its package input hashes selected: a full set without a base, and
// only the differences from its base's otherwise. Each snapshot is one transaction with the marker.
func backfillDocumentMembership(ctx context.Context, database *gorm.DB, marker *DocumentMembership) error {
	var snapshots []ModuleSnapshot
	if err := database.WithContext(ctx).Where("ordinal > ?", marker.MembersThrough).Order("ordinal").Find(&snapshots).Error; err != nil {
		return fmt.Errorf("load snapshots to backfill: %w", err)
	}
	memberships := map[uuid.UUID]map[string]int64{}
	for _, snapshot := range snapshots {
		current, err := legacyMembership(ctx, database, snapshot, memberships)
		if err != nil {
			return err
		}
		base := map[string]int64{}
		if snapshot.BaseSnapshotID != nil {
			var baseSnapshot ModuleSnapshot
			if err := database.WithContext(ctx).Where("id = ?", *snapshot.BaseSnapshotID).Take(&baseSnapshot).Error; err != nil {
				return fmt.Errorf("load base of snapshot %s: %w", snapshot.ID, err)
			}
			if base, err = legacyMembership(ctx, database, baseSnapshot, memberships); err != nil {
				return err
			}
		}
		if len(memberships) > membershipMemo {
			memberships = map[uuid.UUID]map[string]int64{snapshot.ID: current}
		}
		if err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if _, err := CreateDocumentDeltas(ctx, tx, DocumentDeltaOptions{SnapshotOrdinal: snapshot.Ordinal, RootID: snapshot.RootID, Base: base, Current: current}); err != nil {
				return err
			}
			marker.MembersThrough = snapshot.Ordinal
			return tx.Model(marker).Update("members_through", marker.MembersThrough).Error
		}); err != nil {
			return fmt.Errorf("backfill the document membership of snapshot %s: %w", snapshot.ID, err)
		}
	}
	return nil
}

// legacyMembership is a snapshot's path → document ordinal membership by the rule documents were
// selected with before document deltas: effective sources give each path's revision, the snapshot's
// package coverage each package's input hash, and the document is the one keyed (root, path, input
// hash). memberships memoizes the result per snapshot, since descendants read their base's.
func legacyMembership(ctx context.Context, database *gorm.DB, snapshot ModuleSnapshot, memberships map[uuid.UUID]map[string]int64) (map[string]int64, error) {
	if known, found := memberships[snapshot.ID]; found {
		return known, nil
	}
	sources, err := EffectiveSources(ctx, database, EffectiveSourcesOptions{SnapshotID: snapshot.ID})
	if err != nil {
		return nil, err
	}
	hashes, err := legacyInputHashes(ctx, database, snapshot, sources)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(sources))
	for path := range sources {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	membership := make(map[string]int64, len(paths))
	for start := 0; start < len(paths); start += documentLookupBatch {
		var documents []Document
		if err := database.WithContext(ctx).Select("id", "path_key", "input_hash", "source_revision_id", "ordinal").
			Where("root_id = ? AND path_key IN ?", snapshot.RootID, paths[start:min(start+documentLookupBatch, len(paths))]).Find(&documents).Error; err != nil {
			return nil, fmt.Errorf("load documents of snapshot %s: %w", snapshot.ID, err)
		}
		for _, document := range documents {
			source := sources[document.PathKey]
			if document.InputHash != hashes[source.PackagePath] {
				continue
			}
			if document.SourceRevisionID != source.ID {
				return nil, fmt.Errorf("document %s for %q describes revision %s, snapshot %s has %s", document.ID, document.PathKey, document.SourceRevisionID, snapshot.ID, source.ID)
			}
			membership[document.PathKey] = document.Ordinal
		}
	}
	for _, path := range paths {
		if _, found := membership[path]; !found {
			return nil, fmt.Errorf("snapshot %s has no document for %q under input hash %s", snapshot.ID, path, hashes[sources[path].PackagePath])
		}
	}
	memberships[snapshot.ID] = membership
	return membership, nil
}

func legacyInputHashes(ctx context.Context, database *gorm.DB, snapshot ModuleSnapshot, sources map[string]SourceRevision) (map[string]string, error) {
	if err := checkPackageCoverage(ctx, database, snapshot, sources); err != nil {
		return nil, err
	}
	var rows []PackageCoverage
	if err := database.WithContext(ctx).Select("package_path", "input_hash").Where("snapshot_id = ?", snapshot.ID).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load package input hashes of snapshot %s: %w", snapshot.ID, err)
	}
	hashes := make(map[string]string, len(rows))
	for _, row := range rows {
		hashes[row.PackagePath] = row.InputHash
	}
	return hashes, nil
}
