package storage

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EffectiveSymbol is one symbol a snapshot defines, with the fingerprints of its declaration.
type EffectiveSymbol struct {
	Handle  int64
	ShapeFP *int64
	BodyFP  *int64
}

// effectiveSymbolsSQL keeps the first symbol delta per handle along the base chain (newest link first)
// and drops tombstones, as effectiveDeltasSQL does for paths. The walk columns come on every row.
var effectiveSymbolsSQL = baseChainSQL + fmt.Sprintf(`,
ranked AS (
  SELECT d.symbol_handle, d.operation, d.shape_fp, d.body_fp,
         ROW_NUMBER() OVER (PARTITION BY d.symbol_handle ORDER BY c.depth) AS handle_rank
  FROM %[1]s d JOIN chain c ON c.ordinal = d.snapshot_ordinal
)
SELECT t.depth AS tail_depth, t.base_id AS tail_base_id, r.root_count,
       e.symbol_handle, e.shape_fp, e.body_fp
FROM tail t CROSS JOIN roots r LEFT JOIN ranked e ON e.handle_rank = 1 AND e.operation = 'set'
ORDER BY e.symbol_handle`, SymbolDelta{}.TableName())

type effectiveSymbolRow struct {
	TailDepth    int
	TailBaseID   *uuid.UUID
	RootCount    int
	SymbolHandle *int64
	ShapeFP      *int64 `gorm:"column:shape_fp"`
	BodyFP       *int64 `gorm:"column:body_fp"`
}

// EffectiveSymbols is a snapshot's defined-symbol set, sorted by handle: one recursive query over
// symbol_deltas along the base chain, the newest delta per handle, tombstones dropped. A base cycle, a
// dangling base, a chain spanning roots, and a missing snapshot are errors.
func EffectiveSymbols(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID) ([]EffectiveSymbol, error) {
	if database == nil {
		return nil, fmt.Errorf("load effective symbols for snapshot %s: database is required", snapshotID)
	}
	var rows []effectiveSymbolRow
	if err := database.WithContext(ctx).Raw(effectiveSymbolsSQL, snapshotID, maxBaseChainDepth).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load effective symbols for snapshot %s: %w", snapshotID, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("snapshot %s does not exist", snapshotID)
	}
	if err := (baseChain{TailDepth: rows[0].TailDepth, TailBaseID: rows[0].TailBaseID, RootCount: rows[0].RootCount}).check(snapshotID); err != nil {
		return nil, err
	}
	symbols := make([]EffectiveSymbol, 0, len(rows))
	for _, row := range rows {
		if row.SymbolHandle == nil {
			continue
		}
		if row.ShapeFP == nil || row.BodyFP == nil {
			return nil, fmt.Errorf("symbol delta for handle %d in snapshot %s chain is a set without fingerprints", *row.SymbolHandle, snapshotID)
		}
		symbols = append(symbols, EffectiveSymbol{Handle: *row.SymbolHandle, ShapeFP: row.ShapeFP, BodyFP: row.BodyFP})
	}
	return symbols, nil
}
