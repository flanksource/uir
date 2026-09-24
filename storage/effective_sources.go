package storage

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// maxBaseChainDepth caps the base-chain walk. A recursive CTE cannot detect a cycle by itself, so a
// chain that reaches this depth and still has a base is reported as a cycle or runaway history.
const maxBaseChainDepth = 100_000

const revisionLookupBatch = 256

// effectiveDeltasSQL walks the base chain of one snapshot and returns the first delta per path
// (newest link first), tombstones included, together with the chain's tail and root count so a
// truncated chain, a dangling base, or a chain spanning roots fails instead of resolving short.
// A snapshot that does not exist yields no rows.
var effectiveDeltasSQL = fmt.Sprintf(`WITH RECURSIVE chain(id, base_id, root_id, depth) AS (
  SELECT id, base_snapshot_id, root_id, 0 FROM %[1]s WHERE id = ?
  UNION ALL
  SELECT s.id, s.base_snapshot_id, s.root_id, c.depth + 1
  FROM %[1]s s JOIN chain c ON s.id = c.base_id
  WHERE c.depth < ?
),
tail AS (
  SELECT depth, base_id FROM chain ORDER BY depth DESC LIMIT 1
),
roots AS (
  SELECT COUNT(DISTINCT root_id) AS root_count FROM chain
),
ranked AS (
  SELECT d.path_key, d.root_id, d.revision_id, d.operation,
         ROW_NUMBER() OVER (PARTITION BY d.path_key ORDER BY c.depth) AS path_rank
  FROM %[2]s d JOIN chain c ON c.id = d.snapshot_id
)
SELECT t.depth AS tail_depth, t.base_id AS tail_base_id, r.root_count,
       e.path_key, e.root_id, e.revision_id, e.operation
FROM tail t CROSS JOIN roots r LEFT JOIN ranked e ON e.path_rank = 1`,
	ModuleSnapshot{}.TableName(), SourceDelta{}.TableName())

type effectiveDeltaRow struct {
	TailDepth  int
	TailBaseID *uuid.UUID
	RootCount  int
	PathKey    *string
	RootID     *uuid.UUID
	RevisionID *uuid.UUID
	Operation  *SourceOperation
}

// EffectiveSources resolves a snapshot's path → source revision view: one recursive query takes the
// newest delta per path across the base chain, tombstones drop their path, and the referenced
// revisions are loaded in batches and checked against the delta's root and path.
func EffectiveSources(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID) (map[string]SourceRevision, error) {
	if database == nil {
		return nil, fmt.Errorf("load effective sources for snapshot %s: database is required", snapshotID)
	}
	deltas, err := effectiveDeltas(ctx, database, snapshotID)
	if err != nil {
		return nil, err
	}
	return loadDeltaRevisions(ctx, database, deltas)
}

func effectiveDeltas(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID) ([]SourceDelta, error) {
	var rows []effectiveDeltaRow
	if err := database.WithContext(ctx).Raw(effectiveDeltasSQL, snapshotID, maxBaseChainDepth).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load effective source deltas for snapshot %s: %w", snapshotID, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("snapshot %s does not exist", snapshotID)
	}
	chain := rows[0]
	switch {
	case chain.TailBaseID != nil && chain.TailDepth >= maxBaseChainDepth:
		return nil, fmt.Errorf("snapshot %s base chain exceeds %d base links: base cycle or runaway history", snapshotID, maxBaseChainDepth)
	case chain.TailBaseID != nil:
		return nil, fmt.Errorf("snapshot %s base chain references missing snapshot %s", snapshotID, *chain.TailBaseID)
	case chain.RootCount != 1:
		return nil, fmt.Errorf("snapshot %s base chain spans %d roots", snapshotID, chain.RootCount)
	}
	deltas := make([]SourceDelta, 0, len(rows))
	for _, row := range rows {
		if row.PathKey == nil {
			continue
		}
		if row.RootID == nil || row.Operation == nil {
			return nil, fmt.Errorf("source delta %q of snapshot %s chain has no root or operation", *row.PathKey, snapshotID)
		}
		deltas = append(deltas, SourceDelta{RootID: *row.RootID, PathKey: *row.PathKey, RevisionID: row.RevisionID, Operation: *row.Operation})
	}
	return deltas, nil
}

func loadDeltaRevisions(ctx context.Context, database *gorm.DB, deltas []SourceDelta) (map[string]SourceRevision, error) {
	pending := make(map[uuid.UUID]SourceDelta)
	for _, delta := range deltas {
		switch delta.Operation {
		case SourceDelete:
			if delta.RevisionID != nil {
				return nil, fmt.Errorf("deleted source %q has a revision ID", delta.PathKey)
			}
		case SourceSet:
			if delta.RevisionID == nil {
				return nil, fmt.Errorf("source %q has no revision ID", delta.PathKey)
			}
			if prior, exists := pending[*delta.RevisionID]; exists {
				return nil, fmt.Errorf("sources %q and %q reference the same revision %s", prior.PathKey, delta.PathKey, *delta.RevisionID)
			}
			pending[*delta.RevisionID] = delta
		default:
			return nil, fmt.Errorf("source %q has invalid delta operation %q", delta.PathKey, delta.Operation)
		}
	}
	ids := make([]uuid.UUID, 0, len(pending))
	for id := range pending {
		ids = append(ids, id)
	}
	result := make(map[string]SourceRevision, len(pending))
	for start := 0; start < len(ids); start += revisionLookupBatch {
		var revisions []SourceRevision
		if err := database.WithContext(ctx).Where("id IN ?", ids[start:min(start+revisionLookupBatch, len(ids))]).Find(&revisions).Error; err != nil {
			return nil, fmt.Errorf("load effective source revisions: %w", err)
		}
		for _, revision := range revisions {
			delta := pending[revision.ID]
			if revision.RootID != delta.RootID || revision.PathKey != delta.PathKey {
				return nil, fmt.Errorf("source delta %q points to revision for root %s and path %q", delta.PathKey, revision.RootID, revision.PathKey)
			}
			result[delta.PathKey] = revision
			delete(pending, revision.ID)
		}
	}
	for id, delta := range pending {
		return nil, fmt.Errorf("source %q references missing revision %s", delta.PathKey, id)
	}
	return result, nil
}
