package storage

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// createBatch bounds the rows of one insert of a rebased snapshot's deltas.
const createBatch = 500

// apply rebases, then deletes in the order the foreign keys allow: snapshots (their deltas, coverage,
// and dependency edges cascade), documents with their postings, source revisions, source blobs, and
// symbols. candidates collects the handles of every symbol a deleted row named, the only symbols that
// can have become orphans.
func (plan prunePlan) apply(ctx context.Context, tx *gorm.DB, result *PruneResult) error {
	candidates := map[int64]bool{}
	for _, rebased := range plan.rebase {
		if err := rebaseSnapshot(ctx, tx, plan.root, rebased, candidates); err != nil {
			return err
		}
	}
	result.Rebased = len(plan.rebase)
	if err := plan.deleteSnapshots(ctx, tx, candidates, result); err != nil {
		return err
	}
	if err := plan.deleteDocuments(ctx, tx, candidates, result); err != nil {
		return err
	}
	hashes, err := deleteUnusedRevisions(ctx, tx, plan.root, result)
	if err != nil {
		return err
	}
	if err := deleteUnusedBlobs(ctx, tx, hashes, result); err != nil {
		return err
	}
	if err := deleteOrphanSymbols(ctx, tx, sortedSet(candidates), result); err != nil {
		return err
	}
	return advancePruneEpoch(ctx, tx)
}

// rebaseSnapshot makes a kept snapshot whose base is deleted stand alone: one set delta per path,
// document binding, and symbol it serves replaces its own deltas, it loses its base, and its stats are recorded again against
// no base. The handles its old symbol deltas named become orphan candidates.
func rebaseSnapshot(ctx context.Context, tx *gorm.DB, root ModuleRoot, rebased rebasedSnapshot, candidates map[int64]bool) error {
	snapshot := rebased.snapshot
	for _, source := range snapshotHandles {
		if err := collectHandles(ctx, tx, source, "snapshot_ordinal", []int64{snapshot.Ordinal}, candidates); err != nil {
			return err
		}
	}
	if err := tx.WithContext(ctx).Where("snapshot_id = ?", snapshot.ID).Delete(&SourceDelta{}).Error; err != nil {
		return fmt.Errorf("delete source deltas of snapshot %s: %w", snapshot.ID, err)
	}
	for _, model := range []any{&SymbolDelta{}, &ImplementsDelta{}, &DocumentDelta{}} {
		if err := tx.WithContext(ctx).Where("snapshot_ordinal = ?", snapshot.Ordinal).Delete(model).Error; err != nil {
			return fmt.Errorf("delete the %T rows of snapshot %s: %w", model, snapshot.ID, err)
		}
	}
	if err := createRebasedDeltas(ctx, tx, root, rebased); err != nil {
		return err
	}
	if _, err := CreateDocumentDeltas(ctx, tx, DocumentDeltaOptions{
		SnapshotOrdinal: snapshot.Ordinal, RootID: root.ID, Base: map[string]int64{}, Current: rebased.documents,
	}); err != nil {
		return fmt.Errorf("write the document deltas of rebased snapshot %s: %w", snapshot.ID, err)
	}
	updated := tx.WithContext(ctx).Model(&ModuleSnapshot{}).Where("id = ?", snapshot.ID).Update("base_snapshot_id", nil)
	if updated.Error != nil {
		return fmt.Errorf("clear the base of snapshot %s: %w", snapshot.ID, updated.Error)
	}
	if updated.RowsAffected != 1 {
		return fmt.Errorf("clear the base of snapshot %s: updated %d rows", snapshot.ID, updated.RowsAffected)
	}
	return recordDerivedSnapshotStats(ctx, tx, snapshot.ID)
}

func createRebasedDeltas(ctx context.Context, tx *gorm.DB, root ModuleRoot, rebased rebasedSnapshot) error {
	snapshot := rebased.snapshot
	paths := make([]string, 0, len(rebased.sources))
	for path := range rebased.sources {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	sources := make([]SourceDelta, 0, len(paths))
	for _, path := range paths {
		revision := rebased.sources[path].ID
		sources = append(sources, SourceDelta{SnapshotID: snapshot.ID, RootID: root.ID, PathKey: path, RevisionID: &revision, Operation: SourceSet})
	}
	symbols := make([]SymbolDelta, 0, len(rebased.symbols))
	for _, symbol := range rebased.symbols {
		symbols = append(symbols, SymbolDelta{
			SnapshotOrdinal: snapshot.Ordinal, RootOrdinal: root.Ordinal, SymbolHandle: symbol.Handle,
			Operation: SourceSet, PublicFP: symbol.PublicFP, PrivateFP: symbol.PrivateFP, ContentFP: symbol.ContentFP,
		})
	}
	if len(sources) > 0 {
		if err := tx.WithContext(ctx).CreateInBatches(&sources, createBatch).Error; err != nil {
			return fmt.Errorf("write the source deltas of rebased snapshot %s: %w", snapshot.ID, err)
		}
	}
	if len(symbols) > 0 {
		if err := tx.WithContext(ctx).CreateInBatches(&symbols, createBatch).Error; err != nil {
			return fmt.Errorf("write the symbol deltas of rebased snapshot %s: %w", snapshot.ID, err)
		}
	}
	implements := make([]ImplementsDelta, len(rebased.implements))
	for i, pair := range rebased.implements {
		implements[i] = ImplementsDelta{SnapshotOrdinal: snapshot.Ordinal, RootOrdinal: root.Ordinal, TypeHandle: pair.Type, InterfaceHandle: pair.Interface, Operation: SourceSet}
	}
	if len(implements) > 0 {
		if err := tx.WithContext(ctx).CreateInBatches(&implements, createBatch).Error; err != nil {
			return fmt.Errorf("write the implements deltas of rebased snapshot %s: %w", snapshot.ID, err)
		}
	}
	return nil
}

// deleteSnapshots deletes the plan's deleted snapshots newest first, so a snapshot goes before the base
// it names; the handles of their symbol deltas become orphan candidates.
func (plan prunePlan) deleteSnapshots(ctx context.Context, tx *gorm.DB, candidates map[int64]bool, result *PruneResult) error {
	if err := inBatches(plan.deleted, func(batch []ModuleSnapshot) error {
		ids, ordinals := make([]uuid.UUID, 0, len(batch)), make([]int64, 0, len(batch))
		for _, snapshot := range batch {
			ids, ordinals = append(ids, snapshot.ID), append(ordinals, snapshot.Ordinal)
		}
		for _, source := range snapshotHandles {
			if err := collectHandles(ctx, tx, source, "snapshot_ordinal", ordinals, candidates); err != nil {
				return err
			}
		}
		deleted := tx.WithContext(ctx).Where("id IN ?", ids).Delete(&ModuleSnapshot{})
		if deleted.Error != nil {
			return fmt.Errorf("delete snapshots: %w", deleted.Error)
		}
		result.Snapshots += deleted.RowsAffected
		return nil
	}); err != nil {
		return err
	}
	if result.Snapshots != int64(len(plan.deleted)) {
		return fmt.Errorf("deleted %d of %d superseded snapshots", result.Snapshots, len(plan.deleted))
	}
	return nil
}

// deleteDocuments deletes the root's documents no kept snapshot activates, with their postings, whose
// symbols become orphan candidates.
func (plan prunePlan) deleteDocuments(ctx context.Context, tx *gorm.DB, candidates map[int64]bool, result *PruneResult) error {
	var documents []Document
	if err := tx.WithContext(ctx).Select("id", "ordinal").Where("root_id = ?", plan.root.ID).Order("ordinal").Find(&documents).Error; err != nil {
		return fmt.Errorf("load documents: %w", err)
	}
	var dead []int64
	for _, document := range documents {
		if !plan.live[document.ID] {
			dead = append(dead, document.Ordinal)
		}
	}
	return inBatches(dead, func(batch []int64) error {
		if err := collectHandles(ctx, tx, postingHandles, "document_ordinal", batch, candidates); err != nil {
			return err
		}
		postings := tx.WithContext(ctx).Where("document_ordinal IN ?", batch).Delete(&SymbolPosting{})
		if postings.Error != nil {
			return fmt.Errorf("delete postings: %w", postings.Error)
		}
		deleted := tx.WithContext(ctx).Where("ordinal IN ?", batch).Delete(&Document{})
		if deleted.Error != nil {
			return fmt.Errorf("delete documents: %w", deleted.Error)
		}
		result.Postings, result.Documents = result.Postings+postings.RowsAffected, result.Documents+deleted.RowsAffected
		return nil
	})
}

// handleColumn is a column that names a symbol handle, which keeps the symbol from being an orphan.
type handleColumn struct{ table, handle string }

var (
	postingHandles             = handleColumn{"symbol_postings", "symbol_handle"}
	symbolDeltaHandles         = handleColumn{"symbol_deltas", "symbol_handle"}
	implementsTypeHandles      = handleColumn{"implements_deltas", "type_handle"}
	implementsInterfaceHandles = handleColumn{"implements_deltas", "interface_handle"}
	// snapshotHandles are the handle columns of rows keyed by snapshot ordinal.
	snapshotHandles = []handleColumn{symbolDeltaHandles, implementsTypeHandles, implementsInterfaceHandles}
)

// collectHandles adds to candidates the symbol handles source names in the rows whose column is in values.
func collectHandles(ctx context.Context, tx *gorm.DB, source handleColumn, column string, values []int64, candidates map[int64]bool) error {
	var handles []int64
	if err := tx.WithContext(ctx).Table(source.table).Distinct(source.handle).Where(column+" IN ?", values).Pluck(source.handle, &handles).Error; err != nil {
		return fmt.Errorf("load the symbol handles of %s.%s: %w", source.table, source.handle, err)
	}
	for _, handle := range handles {
		candidates[handle] = true
	}
	return nil
}

// deleteUnusedRevisions deletes the root's source revisions that no source delta and no document names,
// and returns their content hashes.
func deleteUnusedRevisions(ctx context.Context, tx *gorm.DB, root ModuleRoot, result *PruneResult) ([]string, error) {
	var unused []SourceRevision
	if err := tx.WithContext(ctx).Select("id", "content_hash").Where("root_id = ?", root.ID).
		Where("NOT EXISTS (SELECT 1 FROM source_deltas d WHERE d.revision_id = source_revisions.id)").
		Where("NOT EXISTS (SELECT 1 FROM documents x WHERE x.source_revision_id = source_revisions.id)").
		Order("id").Find(&unused).Error; err != nil {
		return nil, fmt.Errorf("find unused source revisions: %w", err)
	}
	hashes := map[string]bool{}
	ids := make([]uuid.UUID, 0, len(unused))
	for _, revision := range unused {
		ids, hashes[revision.ContentHash] = append(ids, revision.ID), true
	}
	err := inBatches(ids, func(batch []uuid.UUID) error {
		deleted := tx.WithContext(ctx).Where("id IN ?", batch).Delete(&SourceRevision{})
		if deleted.Error != nil {
			return fmt.Errorf("delete source revisions: %w", deleted.Error)
		}
		result.SourceRevisions += deleted.RowsAffected
		return nil
	})
	return sortedSet(hashes), err
}

// deleteUnusedBlobs deletes the source blobs of hashes that no remaining source revision of any root has.
func deleteUnusedBlobs(ctx context.Context, tx *gorm.DB, hashes []string, result *PruneResult) error {
	return inBatches(hashes, func(batch []string) error {
		var used []string
		if err := tx.WithContext(ctx).Model(&SourceRevision{}).Distinct("content_hash").Where("content_hash IN ?", batch).Pluck("content_hash", &used).Error; err != nil {
			return fmt.Errorf("find used source blobs: %w", err)
		}
		unused := slices.DeleteFunc(slices.Clone(batch), func(hash string) bool { return slices.Contains(used, hash) })
		if len(unused) == 0 {
			return nil
		}
		deleted := tx.WithContext(ctx).Where("content_hash IN ?", unused).Delete(&SourceBlob{})
		if deleted.Error != nil {
			return fmt.Errorf("delete source blobs: %w", deleted.Error)
		}
		result.SourceBlobs += deleted.RowsAffected
		return nil
	})
}
