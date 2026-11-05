package storage

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
// content fingerprint and generation; allSymbolGenerationsSQL is the same for every handle the chain
// names.
var (
	symbolGenerationsSQL    = symbolGenerationsQuery("WHERE d.symbol_handle IN ?")
	allSymbolGenerationsSQL = symbolGenerationsQuery("")
)

func symbolGenerationsQuery(filter string) string {
	return baseChainSQL + fmt.Sprintf(`,
ranked AS (
  SELECT d.symbol_handle, d.content_fp, d.content_generation,
         ROW_NUMBER() OVER (PARTITION BY d.symbol_handle ORDER BY c.depth) AS handle_rank
  FROM %[1]s d JOIN chain c ON c.ordinal = d.snapshot_ordinal
  %[2]s
)
SELECT t.depth AS tail_depth, t.base_id AS tail_base_id, r.root_count,
       e.symbol_handle, e.content_fp, e.content_generation
FROM tail t CROSS JOIN roots r LEFT JOIN ranked e ON e.handle_rank = 1
ORDER BY e.symbol_handle`, SymbolDelta{}.TableName(), filter)
}

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
		if err := collectGenerations(snapshotID, rows, generations); err != nil {
			return nil, err
		}
	}
	return generations, nil
}

// allSymbolGenerations is SymbolGenerations for every handle the snapshot's base chain names, in one
// read of the chain.
func allSymbolGenerations(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID) (map[int64]SymbolGeneration, error) {
	var rows []effectiveSymbolRow
	if err := database.WithContext(ctx).Raw(allSymbolGenerationsSQL, snapshotID, maxBaseChainDepth).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load symbol generations for snapshot %s: %w", snapshotID, err)
	}
	generations := map[int64]SymbolGeneration{}
	if err := collectGenerations(snapshotID, rows, generations); err != nil {
		return nil, err
	}
	return generations, nil
}

func collectGenerations(snapshotID uuid.UUID, rows []effectiveSymbolRow, generations map[int64]SymbolGeneration) error {
	if len(rows) == 0 {
		return fmt.Errorf("snapshot %s does not exist", snapshotID)
	}
	if err := (baseChain{TailDepth: rows[0].TailDepth, TailBaseID: rows[0].TailBaseID, RootCount: rows[0].RootCount}).check(snapshotID); err != nil {
		return err
	}
	for _, row := range rows {
		if row.SymbolHandle == nil {
			continue
		}
		if row.Generation == nil {
			return fmt.Errorf("symbol delta for handle %d in snapshot %s chain has no content generation: %w", *row.SymbolHandle, snapshotID, ErrSymbolGenerationCutover)
		}
		generations[*row.SymbolHandle] = SymbolGeneration{Generation: *row.Generation, ContentFP: row.ContentFP}
	}
	return nil
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

// generationMemo bounds the base chains' generations the backfill keeps for later dependants.
const generationMemo = 32

// cutOverSymbolGenerations completes the symbol generation cutover after migration, on every read-write
// open: when any symbol delta lacks a generation, such as rows an older uir binary published after the
// cutover completed, every snapshot from the lowest such row's on gets its generations in ordinal order,
// one transaction per snapshot with the marker, and the marker is set complete last. The lookup is served
// by the partial index symbol_deltas_missing_generation_idx. An interrupted cutover resumes on the next
// open.
func cutOverSymbolGenerations(ctx context.Context, database *gorm.DB) error {
	var markers []SymbolGenerationBackfill
	if err := database.WithContext(ctx).Where("id = 1").Find(&markers).Error; err != nil {
		return fmt.Errorf("read the symbol generation cutover: %w", err)
	}
	var lowest []int64
	if err := database.WithContext(ctx).Model(&SymbolDelta{}).Where("content_generation IS NULL").
		Order("snapshot_ordinal").Limit(1).Pluck("snapshot_ordinal", &lowest).Error; err != nil {
		return fmt.Errorf("find symbol deltas without a generation: %w", err)
	}
	marker := SymbolGenerationBackfill{ID: 1, Complete: len(lowest) == 0}
	if len(lowest) == 1 {
		marker.ThroughOrdinal = lowest[0] - 1
	} else if len(markers) == 1 {
		if markers[0].Complete {
			return nil
		}
		marker.ThroughOrdinal = markers[0].ThroughOrdinal
	}
	if err := database.WithContext(ctx).Save(&marker).Error; err != nil {
		return fmt.Errorf("start the symbol generation cutover: %w", err)
	}
	if marker.Complete {
		return nil
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
// content generation. A base has a lower ordinal, so the cutover already gave its whole chain theirs.
// Each snapshot's handle → generation map is derived from its base's plus its own deltas and kept while
// a later snapshot still builds on it (up to generationMemo of them), so a chain is read from the
// database only where its base is not kept. Rows that already carry a generation keep it.
func backfillSymbolGenerations(ctx context.Context, database *gorm.DB, marker *SymbolGenerationBackfill) error {
	var snapshots []ModuleSnapshot
	if err := database.WithContext(ctx).Select("id", "ordinal", "base_snapshot_id").Order("ordinal").Find(&snapshots).Error; err != nil {
		return fmt.Errorf("load snapshots to backfill: %w", err)
	}
	ordinals := make(map[uuid.UUID]int64, len(snapshots))
	dependants := map[uuid.UUID]int{}
	for _, snapshot := range snapshots {
		ordinals[snapshot.ID] = snapshot.Ordinal
		if snapshot.Ordinal > marker.ThroughOrdinal && snapshot.BaseSnapshotID != nil {
			dependants[*snapshot.BaseSnapshotID]++
		}
	}
	memo := map[uuid.UUID]map[int64]SymbolGeneration{}
	for _, snapshot := range snapshots {
		if snapshot.Ordinal <= marker.ThroughOrdinal {
			continue
		}
		generations := map[int64]SymbolGeneration{}
		if base := snapshot.BaseSnapshotID; base != nil {
			if ordinals[*base] >= snapshot.Ordinal {
				return fmt.Errorf("snapshot %s has base %s with ordinal %d, not below its own %d", snapshot.ID, *base, ordinals[*base], snapshot.Ordinal)
			}
			prior, kept := memo[*base]
			if !kept {
				var err error
				if prior, err = allSymbolGenerations(ctx, database, *base); err != nil {
					return fmt.Errorf("backfill the symbol generations of snapshot %s: %w", snapshot.ID, err)
				}
			}
			dependants[*base]--
			switch {
			case dependants[*base] == 0:
				delete(memo, *base)
				generations = prior
			case kept:
				generations = maps.Clone(prior)
			case len(memo) < generationMemo:
				memo[*base] = prior
				generations = maps.Clone(prior)
			default:
				generations = prior
			}
		}
		if err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := backfillSnapshotGenerations(ctx, tx, snapshot, generations); err != nil {
				return err
			}
			marker.ThroughOrdinal = snapshot.Ordinal
			return tx.Model(marker).Update("through_ordinal", marker.ThroughOrdinal).Error
		}); err != nil {
			return fmt.Errorf("backfill the symbol generations of snapshot %s: %w", snapshot.ID, err)
		}
		if dependants[snapshot.ID] > 0 && len(memo) < generationMemo {
			memo[snapshot.ID] = generations
		}
	}
	return nil
}

// backfillSnapshotGenerations gives the snapshot's deltas without a generation theirs from generations,
// its base chain's, and then applies all of its deltas to generations, so it becomes the snapshot's.
func backfillSnapshotGenerations(ctx context.Context, tx *gorm.DB, snapshot ModuleSnapshot, generations map[int64]SymbolGeneration) error {
	var deltas []SymbolDelta
	if err := tx.WithContext(ctx).Select("symbol_handle", "operation", "content_fp", "content_generation").
		Where("snapshot_ordinal = ?", snapshot.Ordinal).Order("symbol_handle").Find(&deltas).Error; err != nil {
		return fmt.Errorf("load symbol deltas: %w", err)
	}
	byGeneration := map[int64][]int64{}
	next := make(map[int64]SymbolGeneration, len(deltas))
	for _, delta := range deltas {
		generation := delta.ContentGeneration
		if generation == nil {
			prior, found := generations[delta.SymbolHandle]
			computed, err := NextContentGeneration(prior, found, delta.Operation, delta.ContentFP)
			if err != nil {
				return fmt.Errorf("symbol handle %d: %w", delta.SymbolHandle, err)
			}
			generation = &computed
			byGeneration[computed] = append(byGeneration[computed], delta.SymbolHandle)
		}
		next[delta.SymbolHandle] = SymbolGeneration{Generation: *generation, ContentFP: delta.ContentFP}
	}
	maps.Copy(generations, next)
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
