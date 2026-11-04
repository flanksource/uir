package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrSymbolGenerationCutover marks a database whose symbol generation cutover has not completed.
var ErrSymbolGenerationCutover = errors.New("database predates symbol content generations; open it read-write once to complete the cutover")

// SymbolGenerationBackfill is the one row recording the symbol generation cutover; see
// 23_symbol_generations.hcl.
type SymbolGenerationBackfill struct {
	ID             int32 `gorm:"column:id;primaryKey;autoIncrement:false"`
	ThroughOrdinal int64 `gorm:"column:through_ordinal"`
	Complete       bool  `gorm:"column:complete"`
}

func (SymbolGenerationBackfill) TableName() string { return "symbol_generation_backfill" }

// SymbolGeneration is the newest symbol delta of one handle along a snapshot's base chain, tombstones
// included: its content generation, and its content fingerprint, nil for a tombstone.
type SymbolGeneration struct {
	Generation int64
	ContentFP  *int64
}

// NextContentGeneration is the content generation of a delta written on top of prior, the newest delta
// of its handle along the base chain (found is false when the chain never named the handle). A set
// starts at 1 for a symbol the chain never named, keeps prior's generation when it repeats prior's
// content fingerprint, and otherwise, a changed implementation or a re-add after a tombstone, is one
// more. A delete carries the generation the symbol ended at, so a re-added symbol continues its count.
func NextContentGeneration(prior SymbolGeneration, found bool, operation SourceOperation, contentFP *int64) (int64, error) {
	switch {
	case operation == SourceDelete && !found:
		return 0, errors.New("a delete names a symbol its base chain never defined")
	case operation == SourceDelete:
		return prior.Generation, nil
	case contentFP == nil:
		return 0, errors.New("a set has no content fingerprint")
	case !found:
		return 1, nil
	case prior.ContentFP != nil && *prior.ContentFP == *contentFP:
		return prior.Generation, nil
	default:
		return prior.Generation + 1, nil
	}
}

// symbolGenerationsSQL is definedSymbolsSQL keeping tombstones and selecting the winning delta's
// content fingerprint and generation.
var symbolGenerationsSQL = baseChainSQL + fmt.Sprintf(`,
ranked AS (
  SELECT d.symbol_handle, d.content_fp, d.content_generation,
         ROW_NUMBER() OVER (PARTITION BY d.symbol_handle ORDER BY c.depth) AS handle_rank
  FROM %[1]s d JOIN chain c ON c.ordinal = d.snapshot_ordinal
  WHERE d.symbol_handle IN ?
)
SELECT t.depth AS tail_depth, t.base_id AS tail_base_id, r.root_count,
       e.symbol_handle, e.content_fp, e.content_generation
FROM tail t CROSS JOIN roots r LEFT JOIN ranked e ON e.handle_rank = 1
ORDER BY e.symbol_handle`, SymbolDelta{}.TableName())

// SymbolGenerations is, for each of handles the snapshot's base chain names, its newest delta's content
// generation and content fingerprint, tombstones included so a re-added symbol continues its count.
// Handles the chain never named are absent. Like DefinedSymbols it reads IN batches of 256 handles, so
// its cost follows the handles asked about. The chain errors are those of EffectiveSymbols; a delta
// without a generation is ErrSymbolGenerationCutover.
func SymbolGenerations(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID, handles []int64) (map[int64]SymbolGeneration, error) {
	if database == nil {
		return nil, fmt.Errorf("load symbol generations for snapshot %s: database is required", snapshotID)
	}
	asked := slices.Clone(handles)
	slices.Sort(asked)
	asked = slices.Compact(asked)
	generations := make(map[int64]SymbolGeneration, len(asked))
	for start := 0; start < len(asked); start += definedLookupBatch {
		var rows []effectiveSymbolRow
		if err := database.WithContext(ctx).Raw(symbolGenerationsSQL, snapshotID, maxBaseChainDepth, asked[start:min(start+definedLookupBatch, len(asked))]).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("load symbol generations for snapshot %s: %w", snapshotID, err)
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("snapshot %s does not exist", snapshotID)
		}
		if err := (baseChain{TailDepth: rows[0].TailDepth, TailBaseID: rows[0].TailBaseID, RootCount: rows[0].RootCount}).check(snapshotID); err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.SymbolHandle == nil {
				continue
			}
			if row.Generation == nil {
				return nil, fmt.Errorf("symbol delta for handle %d in snapshot %s chain has no content generation: %w", *row.SymbolHandle, snapshotID, ErrSymbolGenerationCutover)
			}
			generations[*row.SymbolHandle] = SymbolGeneration{Generation: *row.Generation, ContentFP: row.ContentFP}
		}
	}
	return generations, nil
}

// SymbolHistoryEntry is a snapshot along a base chain at which a symbol's content generation advanced.
type SymbolHistoryEntry struct {
	SnapshotID  uuid.UUID `json:"snapshot_id"`
	Ordinal     int64     `json:"ordinal"`
	Revision    string    `json:"revision,omitempty"`
	GitCommit   string    `json:"git_commit,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
	Generation  int64     `json:"generation"`
}

// symbolHistorySQL lists one handle's set deltas along the base chain, oldest link first, with each
// link's snapshot; the delta primary key (snapshot_ordinal, symbol_handle) serves each link's lookup.
var symbolHistorySQL = baseChainSQL + fmt.Sprintf(`
SELECT t.depth AS tail_depth, t.base_id AS tail_base_id, r.root_count,
       h.snapshot_id, h.ordinal, h.revision, h.git_commit, h.completed_at, h.generation
FROM tail t CROSS JOIN roots r LEFT JOIN (
  SELECT s.id AS snapshot_id, s.ordinal, s.revision, s.git_commit, s.completed_at, d.content_generation AS generation, c.depth
  FROM chain c
  JOIN %[1]s d ON d.snapshot_ordinal = c.ordinal AND d.symbol_handle = ? AND d.operation = 'set'
  JOIN %[2]s s ON s.id = c.id
) h ON 1 = 1
ORDER BY h.depth DESC`, SymbolDelta{}.TableName(), ModuleSnapshot{}.TableName())

type symbolHistoryRow struct {
	TailDepth   int
	TailBaseID  *uuid.UUID
	RootCount   int
	SnapshotID  *uuid.UUID
	Ordinal     *int64
	Revision    *string
	GitCommit   *string
	CompletedAt *time.Time
	Generation  *int64
}

// SymbolHistory lists, oldest first, the snapshots along snapshotID's base chain at which handle's
// content generation advanced: where the chain first defines it, and every later set with a higher
// generation. A set that kept the generation (a signature-only change) is not listed. The chain errors
// are those of EffectiveSymbols.
func SymbolHistory(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID, handle int64) ([]SymbolHistoryEntry, error) {
	var rows []symbolHistoryRow
	if err := database.WithContext(ctx).Raw(symbolHistorySQL, snapshotID, maxBaseChainDepth, handle).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load the history of symbol %d in snapshot %s: %w", handle, snapshotID, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("snapshot %s does not exist", snapshotID)
	}
	if err := (baseChain{TailDepth: rows[0].TailDepth, TailBaseID: rows[0].TailBaseID, RootCount: rows[0].RootCount}).check(snapshotID); err != nil {
		return nil, err
	}
	entries := []SymbolHistoryEntry{}
	var reached int64
	for _, row := range rows {
		if row.SnapshotID == nil {
			continue
		}
		if row.Generation == nil {
			return nil, fmt.Errorf("symbol delta for handle %d in snapshot %s has no content generation: %w", handle, *row.SnapshotID, ErrSymbolGenerationCutover)
		}
		if *row.Generation <= reached {
			continue
		}
		reached = *row.Generation
		entries = append(entries, SymbolHistoryEntry{
			SnapshotID: *row.SnapshotID, Ordinal: *row.Ordinal, Revision: *row.Revision, GitCommit: *row.GitCommit, CompletedAt: *row.CompletedAt, Generation: reached,
		})
	}
	return entries, nil
}

// cutOverSymbolGenerations completes the symbol generation cutover after migration: a database whose
// symbol deltas all carry a generation (an empty one, or one published after the column existed) is
// marked complete; otherwise every snapshot's deltas get their generation in ordinal order, one
// transaction per snapshot with the marker, and the marker is set complete last. An interrupted cutover
// resumes from the marker on the next open.
func cutOverSymbolGenerations(ctx context.Context, database *gorm.DB) error {
	var markers []SymbolGenerationBackfill
	if err := database.WithContext(ctx).Where("id = 1").Find(&markers).Error; err != nil {
		return fmt.Errorf("read the symbol generation cutover: %w", err)
	}
	if len(markers) == 1 && markers[0].Complete {
		return nil
	}
	marker := SymbolGenerationBackfill{ID: 1}
	if len(markers) == 1 {
		marker = markers[0]
	} else {
		var missing int64
		if err := database.WithContext(ctx).Model(&SymbolDelta{}).Where("content_generation IS NULL").Limit(1).Count(&missing).Error; err != nil {
			return fmt.Errorf("count symbol deltas without a generation: %w", err)
		}
		marker.Complete = missing == 0
		if err := database.WithContext(ctx).Create(&marker).Error; err != nil {
			return fmt.Errorf("start the symbol generation cutover: %w", err)
		}
		if marker.Complete {
			return nil
		}
	}
	logger.Infof("backfilling symbol content generations")
	if err := backfillSymbolGenerations(ctx, database, &marker); err != nil {
		return fmt.Errorf("symbol generation cutover: %w", err)
	}
	if err := database.WithContext(ctx).Model(&marker).Update("complete", true).Error; err != nil {
		return fmt.Errorf("complete the symbol generation cutover: %w", err)
	}
	return nil
}

// backfillSymbolGenerations gives the symbol deltas of every snapshot past marker.ThroughOrdinal their
// content generation, reading the base's generations through SymbolGenerations: a base has a lower
// ordinal, so the cutover already gave its whole chain theirs. Rows that already carry one keep it.
func backfillSymbolGenerations(ctx context.Context, database *gorm.DB, marker *SymbolGenerationBackfill) error {
	var snapshots []ModuleSnapshot
	if err := database.WithContext(ctx).Select("id", "ordinal", "base_snapshot_id").Order("ordinal").Find(&snapshots).Error; err != nil {
		return fmt.Errorf("load snapshots to backfill: %w", err)
	}
	ordinals := make(map[uuid.UUID]int64, len(snapshots))
	for _, snapshot := range snapshots {
		ordinals[snapshot.ID] = snapshot.Ordinal
	}
	for _, snapshot := range snapshots {
		if snapshot.Ordinal <= marker.ThroughOrdinal {
			continue
		}
		if snapshot.BaseSnapshotID != nil && ordinals[*snapshot.BaseSnapshotID] >= snapshot.Ordinal {
			return fmt.Errorf("snapshot %s has base %s with ordinal %d, not below its own %d", snapshot.ID, *snapshot.BaseSnapshotID, ordinals[*snapshot.BaseSnapshotID], snapshot.Ordinal)
		}
		if err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := backfillSnapshotGenerations(ctx, tx, snapshot); err != nil {
				return err
			}
			marker.ThroughOrdinal = snapshot.Ordinal
			return tx.Model(marker).Update("through_ordinal", marker.ThroughOrdinal).Error
		}); err != nil {
			return fmt.Errorf("backfill the symbol generations of snapshot %s: %w", snapshot.ID, err)
		}
	}
	return nil
}

func backfillSnapshotGenerations(ctx context.Context, tx *gorm.DB, snapshot ModuleSnapshot) error {
	var deltas []SymbolDelta
	if err := tx.WithContext(ctx).Select("symbol_handle", "operation", "content_fp").
		Where("snapshot_ordinal = ? AND content_generation IS NULL", snapshot.Ordinal).Order("symbol_handle").Find(&deltas).Error; err != nil {
		return fmt.Errorf("load symbol deltas: %w", err)
	}
	if len(deltas) == 0 {
		return nil
	}
	prior := map[int64]SymbolGeneration{}
	if snapshot.BaseSnapshotID != nil {
		handles := make([]int64, len(deltas))
		for i, delta := range deltas {
			handles[i] = delta.SymbolHandle
		}
		var err error
		if prior, err = SymbolGenerations(ctx, tx, *snapshot.BaseSnapshotID, handles); err != nil {
			return err
		}
	}
	byGeneration := map[int64][]int64{}
	for _, delta := range deltas {
		known, found := prior[delta.SymbolHandle]
		generation, err := NextContentGeneration(known, found, delta.Operation, delta.ContentFP)
		if err != nil {
			return fmt.Errorf("symbol handle %d: %w", delta.SymbolHandle, err)
		}
		byGeneration[generation] = append(byGeneration[generation], delta.SymbolHandle)
	}
	for generation, handles := range byGeneration {
		for start := 0; start < len(handles); start += definedLookupBatch {
			if err := tx.WithContext(ctx).Model(&SymbolDelta{}).
				Where("snapshot_ordinal = ? AND symbol_handle IN ?", snapshot.Ordinal, handles[start:min(start+definedLookupBatch, len(handles))]).
				Update("content_generation", generation).Error; err != nil {
				return fmt.Errorf("record content generation %d: %w", generation, err)
			}
		}
	}
	return nil
}

// requireSymbolGenerations refuses a database whose symbol generation cutover is missing or incomplete.
func requireSymbolGenerations(ctx context.Context, database *gorm.DB) error {
	if !database.Migrator().HasTable(&SymbolGenerationBackfill{}) {
		return ErrSymbolGenerationCutover
	}
	var complete []bool
	if err := database.WithContext(ctx).Model(&SymbolGenerationBackfill{}).Where("id = 1").Pluck("complete", &complete).Error; err != nil {
		return fmt.Errorf("read the symbol generation cutover: %w", err)
	}
	if len(complete) != 1 || !complete[0] {
		return ErrSymbolGenerationCutover
	}
	return nil
}
