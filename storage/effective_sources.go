package storage

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func EffectiveSources(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID) (map[string]SourceRevision, error) {
	if database == nil {
		return nil, fmt.Errorf("load effective sources for snapshot %s: database is required", snapshotID)
	}
	result := map[string]SourceRevision{}
	seenSnapshots := map[uuid.UUID]bool{}
	seenPaths := map[string]bool{}
	var rootID uuid.UUID
	for snapshotID != uuid.Nil {
		if seenSnapshots[snapshotID] {
			return nil, fmt.Errorf("snapshot base cycle at %s", snapshotID)
		}
		seenSnapshots[snapshotID] = true
		var snapshot ModuleSnapshot
		if err := database.WithContext(ctx).Where("id = ?", snapshotID).First(&snapshot).Error; err != nil {
			return nil, fmt.Errorf("load snapshot %s: %w", snapshotID, err)
		}
		if snapshot.State != SnapshotReady {
			return nil, fmt.Errorf("snapshot %s is %q, expected ready", snapshotID, snapshot.State)
		}
		if rootID == uuid.Nil {
			rootID = snapshot.RootID
		} else if snapshot.RootID != rootID {
			return nil, fmt.Errorf("snapshot %s belongs to root %s, expected %s", snapshotID, snapshot.RootID, rootID)
		}
		var deltas []SourceDelta
		if err := database.WithContext(ctx).Where("snapshot_id = ?", snapshotID).Find(&deltas).Error; err != nil {
			return nil, fmt.Errorf("load source deltas for snapshot %s: %w", snapshotID, err)
		}
		if err := applySourceDeltas(ctx, database, deltas, seenPaths, result); err != nil {
			return nil, err
		}
		if snapshot.BaseSnapshotID == nil {
			break
		}
		snapshotID = *snapshot.BaseSnapshotID
	}
	return result, nil
}

func applySourceDeltas(ctx context.Context, database *gorm.DB, deltas []SourceDelta, seen map[string]bool, result map[string]SourceRevision) error {
	pending := make(map[uuid.UUID]SourceDelta)
	for _, delta := range deltas {
		if seen[delta.PathKey] {
			continue
		}
		seen[delta.PathKey] = true
		switch delta.Operation {
		case SourceDelete:
			if delta.RevisionID != nil {
				return fmt.Errorf("deleted source %q has a revision ID", delta.PathKey)
			}
		case SourceSet:
			if delta.RevisionID == nil {
				return fmt.Errorf("source %q has no revision ID", delta.PathKey)
			}
			if prior, exists := pending[*delta.RevisionID]; exists {
				return fmt.Errorf("sources %q and %q reference the same revision %s", prior.PathKey, delta.PathKey, *delta.RevisionID)
			}
			pending[*delta.RevisionID] = delta
		default:
			return fmt.Errorf("source %q has invalid delta operation %q", delta.PathKey, delta.Operation)
		}
	}
	ids := make([]uuid.UUID, 0, len(pending))
	for id := range pending {
		ids = append(ids, id)
	}
	for start := 0; start < len(ids); start += 256 {
		var revisions []SourceRevision
		if err := database.WithContext(ctx).Where("id IN ?", ids[start:min(start+256, len(ids))]).Find(&revisions).Error; err != nil {
			return fmt.Errorf("load effective source revisions: %w", err)
		}
		for _, revision := range revisions {
			delta := pending[revision.ID]
			if revision.RootID != delta.RootID || revision.PathKey != delta.PathKey {
				return fmt.Errorf("source delta %q points to revision for root %s and path %q", delta.PathKey, revision.RootID, revision.PathKey)
			}
			result[delta.PathKey] = revision
			delete(pending, revision.ID)
		}
	}
	for id, delta := range pending {
		return fmt.Errorf("source %q references missing revision %s", delta.PathKey, id)
	}
	return nil
}
