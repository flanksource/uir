package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// pruneBatch bounds every IN list a prune sends.
const pruneBatch = 256

// PruneResult is what PruneHistory did to one root: the snapshots it kept, how many of those it rebased
// because their base was deleted, and the rows it deleted from each table.
type PruneResult struct {
	RootKey         string `json:"root_key"`
	Kept            int    `json:"kept"`
	Rebased         int    `json:"rebased"`
	Snapshots       int64  `json:"snapshots"`
	Documents       int64  `json:"documents"`
	Postings        int64  `json:"postings"`
	SourceRevisions int64  `json:"source_revisions"`
	SourceBlobs     int64  `json:"source_blobs"`
	Symbols         int64  `json:"symbols"`
}

// HistoryPrune is the one row counting the prunes that deleted rows; see 11_history_prunes.hcl.
type HistoryPrune struct {
	ID    int32 `gorm:"column:id;primaryKey;autoIncrement:false"`
	Epoch int64 `gorm:"column:epoch"`
}

func (HistoryPrune) TableName() string { return "history_prunes" }

// PruneHistory deletes, in one transaction, a root's snapshots older than its newest keep and every row
// only they used. It keeps every location head, and every snapshot a surviving snapshot's dependency
// targets, whatever its age. A kept snapshot whose base it deletes is rebased: it gets a source and
// symbol delta for every path and symbol it serves, and no base, so it serves exactly what it served
// before. Then the root's documents no kept snapshot activates go with their postings, the root's source
// revisions no remaining delta or document names, the source blobs no remaining revision of any root
// has, and the symbols no remaining posting or symbol delta names and no remaining symbol is owned by.
// A prune that deletes anything advances the history_prunes epoch.
func PruneHistory(ctx context.Context, database *gorm.DB, rootKey string, keep int) (PruneResult, error) {
	if database == nil {
		return PruneResult{}, fmt.Errorf("prune history of %q: database is required", rootKey)
	}
	if keep < 1 {
		return PruneResult{}, fmt.Errorf("prune history of %q: keep %d: at least the newest snapshot must be kept", rootKey, keep)
	}
	result := PruneResult{RootKey: rootKey}
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		plan, err := planPrune(ctx, tx, rootKey, keep)
		if err != nil {
			return err
		}
		result.Kept = len(plan.kept)
		if len(plan.deleted) == 0 {
			return nil
		}
		return plan.apply(ctx, tx, &result)
	})
	if err != nil {
		return PruneResult{}, fmt.Errorf("prune history of %q: %w", rootKey, err)
	}
	return result, nil
}

// prunePlan is what a prune keeps and deletes, read before it writes anything.
type prunePlan struct {
	root ModuleRoot
	// kept and deleted are the root's snapshots, newest first.
	kept, deleted []ModuleSnapshot
	// rebase are the kept snapshots whose base is deleted, with what they serve.
	rebase []rebasedSnapshot
	// live are the documents a kept snapshot activates.
	live map[uuid.UUID]bool
}

type rebasedSnapshot struct {
	snapshot ModuleSnapshot
	sources  map[string]SourceRevision
	symbols  []EffectiveSymbol
}

func planPrune(ctx context.Context, tx *gorm.DB, rootKey string, keep int) (prunePlan, error) {
	var root ModuleRoot
	err := tx.WithContext(ctx).Where("root_key = ?", rootKey).Take(&root).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return prunePlan{}, fmt.Errorf("module root %q is not registered", rootKey)
	}
	if err != nil {
		return prunePlan{}, fmt.Errorf("load module root: %w", err)
	}
	var snapshots []ModuleSnapshot
	if err := tx.WithContext(ctx).Select("id", "root_id", "location_id", "base_snapshot_id", "ordinal").
		Where("root_id = ?", root.ID).Order("ordinal DESC").Find(&snapshots).Error; err != nil {
		return prunePlan{}, fmt.Errorf("load snapshots: %w", err)
	}
	kept, err := keptSnapshots(ctx, tx, root, snapshots, keep)
	if err != nil {
		return prunePlan{}, err
	}
	plan := prunePlan{root: root, live: map[uuid.UUID]bool{}}
	for _, snapshot := range snapshots {
		if kept[snapshot.ID] {
			plan.kept = append(plan.kept, snapshot)
		} else {
			plan.deleted = append(plan.deleted, snapshot)
		}
	}
	if len(plan.deleted) == 0 {
		return plan, nil
	}
	return plan, plan.readKept(ctx, tx)
}

// keptSnapshots are the newest keep of snapshots (newest first), the root's location heads, and,
// until none is added, every snapshot a dependency of a surviving snapshot of any root targets.
func keptSnapshots(ctx context.Context, tx *gorm.DB, root ModuleRoot, snapshots []ModuleSnapshot, keep int) (map[uuid.UUID]bool, error) {
	kept := map[uuid.UUID]bool{}
	for _, snapshot := range snapshots[:min(keep, len(snapshots))] {
		kept[snapshot.ID] = true
	}
	var heads []uuid.UUID
	if err := tx.WithContext(ctx).Model(&ModuleLocationHead{}).Where("root_id = ?", root.ID).Pluck("snapshot_id", &heads).Error; err != nil {
		return nil, fmt.Errorf("load location heads: %w", err)
	}
	for _, head := range heads {
		kept[head] = true
	}
	for {
		var doomed []uuid.UUID
		for _, snapshot := range snapshots {
			if !kept[snapshot.ID] {
				doomed = append(doomed, snapshot.ID)
			}
		}
		if len(doomed) == 0 {
			return kept, nil
		}
		var targets []uuid.UUID
		if err := tx.WithContext(ctx).Model(&SnapshotDependency{}).Distinct("target_snapshot_id").
			Where("target_snapshot_id IN ? AND snapshot_id NOT IN ?", doomed, doomed).Pluck("target_snapshot_id", &targets).Error; err != nil {
			return nil, fmt.Errorf("load dependency targets: %w", err)
		}
		if len(targets) == 0 {
			return kept, nil
		}
		for _, target := range targets {
			kept[target] = true
		}
	}
}

// readKept records the documents each kept snapshot activates and what each one to rebase serves.
func (plan *prunePlan) readKept(ctx context.Context, tx *gorm.DB) error {
	deleted := map[uuid.UUID]bool{}
	for _, snapshot := range plan.deleted {
		deleted[snapshot.ID] = true
	}
	for _, snapshot := range plan.kept {
		active, err := ActiveDocuments(ctx, tx, snapshot.ID, ActiveDocumentOptions{})
		if err != nil {
			return err
		}
		for _, document := range active {
			plan.live[document.Document.ID] = true
		}
		if snapshot.BaseSnapshotID == nil || !deleted[*snapshot.BaseSnapshotID] {
			continue
		}
		sources, err := EffectiveSources(ctx, tx, snapshot.ID)
		if err != nil {
			return err
		}
		symbols, err := EffectiveSymbols(ctx, tx, snapshot.ID)
		if err != nil {
			return err
		}
		plan.rebase = append(plan.rebase, rebasedSnapshot{snapshot: snapshot, sources: sources, symbols: symbols})
	}
	return nil
}

// advancePruneEpoch counts one more prune that deleted rows.
func advancePruneEpoch(ctx context.Context, tx *gorm.DB) error {
	if err := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.Assignments(map[string]any{"epoch": gorm.Expr("history_prunes.epoch + 1")}),
	}).Create(&HistoryPrune{ID: 1, Epoch: 1}).Error; err != nil {
		return fmt.Errorf("advance the prune epoch: %w", err)
	}
	return nil
}

// inBatches calls apply with values in slices of at most pruneBatch.
func inBatches[T any](values []T, apply func([]T) error) error {
	for start := 0; start < len(values); start += pruneBatch {
		if err := apply(values[start:min(start+pruneBatch, len(values))]); err != nil {
			return err
		}
	}
	return nil
}
