package storage

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// definedLookupBatch bounds the handle IN list of one DefinedSymbols query.
const definedLookupBatch = 256

// definedSymbolsSQL is effectiveSymbolsSQL restricted, before ranking, to the handles bound as its
// third parameter; ranking partitions by handle, so the restriction does not change which delta wins.
var definedSymbolsSQL = baseChainSQL + fmt.Sprintf(`,
ranked AS (
  SELECT d.symbol_handle, d.operation,
         ROW_NUMBER() OVER (PARTITION BY d.symbol_handle ORDER BY c.depth) AS handle_rank
  FROM %[1]s d JOIN chain c ON c.ordinal = d.snapshot_ordinal
  WHERE d.symbol_handle IN ?
)
SELECT t.depth AS tail_depth, t.base_id AS tail_base_id, r.root_count, e.symbol_handle
FROM tail t CROSS JOIN roots r LEFT JOIN ranked e ON e.handle_rank = 1 AND e.operation = 'set'
ORDER BY e.symbol_handle`, SymbolDelta{}.TableName())

// DefinedSymbols is the subset of handles that the snapshot defines, sorted and distinct: the
// EffectiveSymbols set restricted to the handles asked about. Each query sends up to 256 handles as an
// IN list, which every link of the base chain probes on the (snapshot_ordinal, symbol_handle) key, so the
// cost follows the handles asked about rather than the size of the snapshot. (Reading handle ranges
// instead, one per module or one per H64a bucket ORed together, was slower on the fixture corpus: the
// ranges read every delta of a module, and SQLite scans rather than seeks an OR of ranges inside the
// chain join.) An empty list reads nothing. A base cycle, a dangling base, a chain spanning roots, and
// a missing snapshot are errors, as for EffectiveSymbols.
func DefinedSymbols(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID, handles []int64) ([]int64, error) {
	if database == nil {
		return nil, fmt.Errorf("load defined symbols for snapshot %s: database is required", snapshotID)
	}
	asked := slices.Clone(handles)
	slices.Sort(asked)
	asked = slices.Compact(asked)
	var defined []int64
	for start := 0; start < len(asked); start += definedLookupBatch {
		found, err := definedSymbolRows(ctx, database, snapshotID, asked[start:min(start+definedLookupBatch, len(asked))])
		if err != nil {
			return nil, err
		}
		defined = append(defined, found...)
	}
	return defined, nil
}

func definedSymbolRows(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID, handles []int64) ([]int64, error) {
	var rows []effectiveSymbolRow
	if err := database.WithContext(ctx).Raw(definedSymbolsSQL, snapshotID, maxBaseChainDepth, handles).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load defined symbols for snapshot %s: %w", snapshotID, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("snapshot %s does not exist", snapshotID)
	}
	if err := (baseChain{TailDepth: rows[0].TailDepth, TailBaseID: rows[0].TailBaseID, RootCount: rows[0].RootCount}).check(snapshotID); err != nil {
		return nil, err
	}
	defined := make([]int64, 0, len(rows))
	for _, row := range rows {
		if row.SymbolHandle != nil {
			defined = append(defined, *row.SymbolHandle)
		}
	}
	return defined, nil
}
