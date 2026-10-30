package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrNoImplementsFacts marks a snapshot published before implements facts became snapshot-level: its
// documents carried them, so it has no implements_facts row and no implements deltas to read.
var ErrNoImplementsFacts = errors.New("snapshot predates snapshot-level implements facts; reindex its checkout")

// ImplementsPair is one implements fact: the handle of a declared type and of an interface it satisfies.
type ImplementsPair struct {
	Type, Interface int64
}

// ImplementsSide names the handle an ImplementsFilter restricts.
type ImplementsSide string

const (
	ImplementsByType      ImplementsSide = "type"
	ImplementsByInterface ImplementsSide = "interface"
)

// ImplementsFilter restricts EffectiveImplements to the pairs whose type, or whose interface, is one of
// Handles. The zero filter reads every pair and checks the count the snapshot's facts row records.
type ImplementsFilter struct {
	By      ImplementsSide
	Handles []int64
}

// implementsSQL keeps the first implements delta per (type, interface) pair along the base chain, newest
// link first, and drops tombstones; %s restricts the deltas before ranking, which partitions by pair, so
// the restriction does not change which delta wins. pair_count is the chain head's facts row, null when
// the snapshot has none.
const implementsSQL = `,
ranked AS (
  SELECT d.type_handle, d.interface_handle, d.operation,
         ROW_NUMBER() OVER (PARTITION BY d.type_handle, d.interface_handle ORDER BY c.depth) AS pair_rank
  FROM %[1]s d JOIN chain c ON c.ordinal = d.snapshot_ordinal
  WHERE %[3]s
)
SELECT t.depth AS tail_depth, t.base_id AS tail_base_id, r.root_count,
       (SELECT f.pair_count FROM %[2]s f JOIN chain h ON h.ordinal = f.snapshot_ordinal WHERE h.depth = 0) AS pair_count,
       e.type_handle, e.interface_handle
FROM tail t CROSS JOIN roots r LEFT JOIN ranked e ON e.pair_rank = 1 AND e.operation = 'set'
ORDER BY e.type_handle, e.interface_handle`

type implementsRow struct {
	TailDepth       int
	TailBaseID      *uuid.UUID
	RootCount       int
	PairCount       *int64
	TypeHandle      *int64
	InterfaceHandle *int64
}

// EffectiveImplements is a snapshot's implements facts the filter selects, sorted by type and interface
// handle: one recursive query over implements_deltas along the base chain per batch of at most 256
// handles. A snapshot without facts is ErrNoImplementsFacts; a base cycle, a dangling base, a chain
// spanning roots, and a missing snapshot are errors, as for EffectiveSymbols.
func EffectiveImplements(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID, filter ImplementsFilter) ([]ImplementsPair, error) {
	if database == nil {
		return nil, fmt.Errorf("load implements facts for snapshot %s: database is required", snapshotID)
	}
	if filter.By == "" {
		if len(filter.Handles) > 0 {
			return nil, fmt.Errorf("load implements facts for snapshot %s: handles given without a side", snapshotID)
		}
		pairs, count, err := implementsRows(ctx, database, snapshotID, "1 = 1")
		if err == nil && int64(len(pairs)) != count {
			err = fmt.Errorf("snapshot %s records %d implements facts, its base chain resolves %d", snapshotID, count, len(pairs))
		}
		return pairs, err
	}
	if filter.By != ImplementsByType && filter.By != ImplementsByInterface {
		return nil, fmt.Errorf("load implements facts for snapshot %s: unknown side %q", snapshotID, filter.By)
	}
	asked := slices.Clone(filter.Handles)
	slices.Sort(asked)
	asked = slices.Compact(asked)
	var pairs []ImplementsPair
	for start := 0; start < len(asked); start += definedLookupBatch {
		found, _, err := implementsRows(ctx, database, snapshotID, "d."+string(filter.By)+"_handle IN ?", asked[start:min(start+definedLookupBatch, len(asked))])
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, found...)
	}
	return pairs, nil
}

func implementsRows(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID, restriction string, arguments ...any) ([]ImplementsPair, int64, error) {
	query := baseChainSQL + fmt.Sprintf(implementsSQL, ImplementsDelta{}.TableName(), ImplementsFacts{}.TableName(), restriction)
	var rows []implementsRow
	if err := database.WithContext(ctx).Raw(query, append([]any{snapshotID, maxBaseChainDepth}, arguments...)...).Scan(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("load implements facts for snapshot %s: %w", snapshotID, err)
	}
	if len(rows) == 0 {
		return nil, 0, fmt.Errorf("snapshot %s does not exist", snapshotID)
	}
	if err := (baseChain{TailDepth: rows[0].TailDepth, TailBaseID: rows[0].TailBaseID, RootCount: rows[0].RootCount}).check(snapshotID); err != nil {
		return nil, 0, err
	}
	if rows[0].PairCount == nil {
		return nil, 0, fmt.Errorf("snapshot %s: %w", snapshotID, ErrNoImplementsFacts)
	}
	pairs := make([]ImplementsPair, 0, len(rows))
	for _, row := range rows {
		if row.TypeHandle != nil {
			pairs = append(pairs, ImplementsPair{Type: *row.TypeHandle, Interface: *row.InterfaceHandle})
		}
	}
	return pairs, *rows[0].PairCount, nil
}
