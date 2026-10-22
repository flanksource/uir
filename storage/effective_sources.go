package storage

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// maxBaseChainDepth caps the base-chain walk. A recursive CTE cannot detect a cycle by itself, so a
// chain that reaches this depth and still has a base is reported as a cycle or runaway history.
const maxBaseChainDepth = 100_000

const revisionLookupBatch = 256

// baseChainSQL is the head of every query over a snapshot's base chain: chain walks the bases of one
// snapshot (bound as the first parameter, the depth cap as the second), and tail and roots describe
// the walk so checkBaseChain can reject a truncated chain, a dangling base, or a chain spanning roots.
// Each link carries its ordinal, which symbol deltas are keyed by. A snapshot that does not exist
// yields an empty chain and no row.
var baseChainSQL = fmt.Sprintf(`WITH RECURSIVE chain(id, base_id, root_id, ordinal, depth) AS (
  SELECT id, base_snapshot_id, root_id, ordinal, 0 FROM %[1]s WHERE id = ?
  UNION ALL
  SELECT s.id, s.base_snapshot_id, s.root_id, s.ordinal, c.depth + 1
  FROM %[1]s s JOIN chain c ON s.id = c.base_id
  WHERE c.depth < ?
),
tail AS (
  SELECT depth, base_id FROM chain ORDER BY depth DESC LIMIT 1
),
roots AS (
  SELECT COUNT(DISTINCT root_id) AS root_count FROM chain
)`, ModuleSnapshot{}.TableName())

// effectiveDeltasSQL returns the first delta per path (newest link first), tombstones included.
var effectiveDeltasSQL = baseChainSQL + fmt.Sprintf(`,
ranked AS (
  SELECT d.path_key, d.root_id, d.revision_id, d.operation,
         ROW_NUMBER() OVER (PARTITION BY d.path_key ORDER BY c.depth) AS path_rank
  FROM %[1]s d JOIN chain c ON c.id = d.snapshot_id
  %%s
)
SELECT t.depth AS tail_depth, t.base_id AS tail_base_id, r.root_count,
       e.path_key, e.root_id, e.revision_id, e.operation
FROM tail t CROSS JOIN roots r LEFT JOIN ranked e ON e.path_rank = 1`, SourceDelta{}.TableName())

// baseChain is the part of every base-chain row that describes the walk.
type baseChain struct {
	TailDepth  int
	TailBaseID *uuid.UUID
	RootCount  int
}

// check fails when the walk stopped at the depth cap, reached a missing base, or crossed roots.
func (chain baseChain) check(snapshotID uuid.UUID) error {
	switch {
	case chain.TailBaseID != nil && chain.TailDepth >= maxBaseChainDepth:
		return fmt.Errorf("snapshot %s base chain exceeds %d base links: base cycle or runaway history", snapshotID, maxBaseChainDepth)
	case chain.TailBaseID != nil:
		return fmt.Errorf("snapshot %s base chain references missing snapshot %s", snapshotID, *chain.TailBaseID)
	case chain.RootCount != 1:
		return fmt.Errorf("snapshot %s base chain spans %d roots", snapshotID, chain.RootCount)
	}
	return nil
}

type effectiveDeltaRow struct {
	TailDepth  int
	TailBaseID *uuid.UUID
	RootCount  int
	PathKey    *string
	RootID     *uuid.UUID
	RevisionID *uuid.UUID
	Operation  *SourceOperation
}

// EffectiveSourcesOptions narrows source resolution before loading revisions.
// Nil Paths selects every path; empty non-nil Paths selects none. Prefix is literal.
type EffectiveSourcesOptions struct {
	SnapshotID uuid.UUID
	Paths      []string
	Prefix     string
	Suffix     string
}

// EffectiveSources resolves selected paths across the complete base chain, validates
// the chain and loads only surviving revisions. Missing paths are omitted.
func EffectiveSources(ctx context.Context, database *gorm.DB, options EffectiveSourcesOptions) (map[string]SourceRevision, error) {
	snapshotID := options.SnapshotID
	if database == nil {
		return nil, fmt.Errorf("load effective sources for snapshot %s: database is required", snapshotID)
	}
	options.Paths = slices.Clone(options.Paths)
	slices.Sort(options.Paths)
	options.Paths = slices.Compact(options.Paths)
	var deltas []SourceDelta
	for start := 0; ; start += revisionLookupBatch {
		selection := options
		if options.Paths != nil {
			selection.Paths = options.Paths[start:min(start+revisionLookupBatch, len(options.Paths))]
		}
		batch, err := effectiveDeltas(ctx, database, selection)
		if err != nil {
			return nil, err
		}
		deltas = append(deltas, batch...)
		if options.Paths == nil || start+revisionLookupBatch >= len(options.Paths) {
			break
		}
	}
	return loadDeltaRevisions(ctx, database, deltas)
}

func effectiveDeltas(ctx context.Context, database *gorm.DB, options EffectiveSourcesOptions) ([]SourceDelta, error) {
	snapshotID := options.SnapshotID
	var predicates []string
	args := []any{snapshotID, maxBaseChainDepth}
	if options.Paths != nil {
		predicates = append(predicates, "d.path_key IN ?")
		args = append(args, options.Paths)
	}
	if options.Prefix != "" {
		predicates = append(predicates, "substr(d.path_key, 1, length(?)) = ?")
		args = append(args, options.Prefix, options.Prefix)
	}
	if options.Suffix != "" {
		predicates = append(predicates, "substr(d.path_key, length(d.path_key) - length(?) + 1) = ?")
		args = append(args, options.Suffix, options.Suffix)
	}
	where := ""
	if len(predicates) > 0 {
		where = "WHERE " + strings.Join(predicates, " AND ")
	}
	var rows []effectiveDeltaRow
	if err := database.WithContext(ctx).Raw(fmt.Sprintf(effectiveDeltasSQL, where), args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load effective source deltas for snapshot %s: %w", snapshotID, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("snapshot %s does not exist", snapshotID)
	}
	if err := (baseChain{TailDepth: rows[0].TailDepth, TailBaseID: rows[0].TailBaseID, RootCount: rows[0].RootCount}).check(snapshotID); err != nil {
		return nil, err
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
