package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ModuleLocationView struct {
	ID             uuid.UUID `json:"id"`
	RootKey        string    `json:"root_key"`
	CanonicalPath  string    `json:"canonical_path"`
	Kind           string    `json:"kind"`
	MountPath      string    `json:"mount_path"`
	Primary        bool      `json:"primary"`
	HeadSnapshotID uuid.UUID `json:"head_snapshot_id"`
	HeadVersion    int64     `json:"head_version"`
}

// SnapshotKind is what a snapshot is to its checkout: a head the checkout published (current or
// superseded), a historical commit indexed without moving the head, or a selected dependency version.
type SnapshotKind string

const (
	SnapshotKindHead       SnapshotKind = "head"
	SnapshotKindHistorical SnapshotKind = "historical"
	SnapshotKindVersioned  SnapshotKind = "versioned"
)

// ModuleSnapshotView is one snapshot of a checkout. Its change counts are against BaseSnapshotID.
type ModuleSnapshotView struct {
	ID             uuid.UUID      `json:"id"`
	RootKey        string         `json:"root_key"`
	CanonicalPath  string         `json:"canonical_path"`
	BaseSnapshotID *uuid.UUID     `json:"base_snapshot_id,omitempty"`
	Revision       string         `json:"revision"`
	GitCommit      string         `json:"git_commit,omitempty"`
	ModuleVersion  string         `json:"module_version,omitempty"`
	LastModifiedAt *time.Time     `json:"last_modified_at,omitempty"`
	WorktreeState  WorktreeState  `json:"worktree_state"`
	Coverage       Coverage       `json:"coverage"`
	Kind           SnapshotKind   `json:"kind"`
	Reason         SnapshotReason `json:"reason"`
	IndexStartedAt *time.Time     `json:"index_started_at,omitempty"`
	StartedAt      time.Time      `json:"started_at"`
	CompletedAt    time.Time      `json:"completed_at"`
	TaskRunID      *string        `json:"task_run_id,omitempty"`
	Head           bool           `json:"head"`
	HeadVersion    int64          `json:"head_version,omitempty"`
	// The SnapshotStats fields, flat so that table output gives each its own column.
	FileCount       int64 `json:"file_count"`
	SymbolCount     int64 `json:"symbol_count"`
	OccurrenceCount int64 `json:"occurrence_count"`
	SourceBytes     int64 `json:"source_bytes"`
	FilesAdded      int   `json:"files_added"`
	FilesChanged    int   `json:"files_changed"`
	FilesDeleted    int   `json:"files_deleted"`
	SymbolsChanged  int   `json:"symbols_changed"`
}

type ModuleSnapshotListOptions struct {
	RootKey  string
	Location string
	Limit    int
	Offset   int
}

func ModuleLocations(ctx context.Context, database *gorm.DB, rootKey string) ([]ModuleLocationView, error) {
	if database == nil || rootKey == "" {
		return nil, errors.New("module locations require a database and root key")
	}
	var root ModuleRoot
	if err := database.WithContext(ctx).Where("root_key = ?", rootKey).First(&root).Error; err != nil {
		return nil, fmt.Errorf("load module root %q: %w", rootKey, err)
	}
	var primary ModulePrimary
	if err := database.WithContext(ctx).Where("root_id = ?", root.ID).First(&primary).Error; err != nil {
		return nil, fmt.Errorf("load primary location for %q: %w", rootKey, err)
	}
	var locations []ModuleLocation
	if err := database.WithContext(ctx).Where("root_id = ?", root.ID).Order("canonical_path").Find(&locations).Error; err != nil {
		return nil, fmt.Errorf("list locations for %q: %w", rootKey, err)
	}
	var heads []ModuleLocationHead
	if err := database.WithContext(ctx).Where("root_id = ?", root.ID).Find(&heads).Error; err != nil {
		return nil, fmt.Errorf("list location heads for %q: %w", rootKey, err)
	}
	byLocation := make(map[uuid.UUID]ModuleLocationHead, len(heads))
	for _, head := range heads {
		byLocation[head.LocationID] = head
	}
	result := make([]ModuleLocationView, 0, len(locations))
	for _, location := range locations {
		head, exists := byLocation[location.ID]
		if !exists {
			return nil, fmt.Errorf("location %s for %q has no published head", location.ID, rootKey)
		}
		result = append(result, ModuleLocationView{
			ID: location.ID, RootKey: rootKey, CanonicalPath: location.CanonicalPath,
			Kind: location.Kind, MountPath: location.MountPath, Primary: location.ID == primary.LocationID,
			HeadSnapshotID: head.SnapshotID, HeadVersion: head.Version,
		})
	}
	return result, nil
}

func ModuleSnapshots(ctx context.Context, database *gorm.DB, options ModuleSnapshotListOptions) ([]ModuleSnapshotView, int64, error) {
	if database == nil || options.RootKey == "" || options.Location == "" {
		return nil, 0, errors.New("module snapshots require a database, root key, and location")
	}
	if options.Limit == 0 {
		options.Limit = 100
	}
	if options.Limit < 1 || options.Limit > 1000 || options.Offset < 0 {
		return nil, 0, fmt.Errorf("module snapshots limit must be 1..1000 and offset >= 0, got %d/%d", options.Limit, options.Offset)
	}
	var root ModuleRoot
	if err := database.WithContext(ctx).Where("root_key = ?", options.RootKey).First(&root).Error; err != nil {
		return nil, 0, fmt.Errorf("load module root %q: %w", options.RootKey, err)
	}
	var location ModuleLocation
	if err := database.WithContext(ctx).Where("root_id = ? AND canonical_path = ?", root.ID, options.Location).First(&location).Error; err != nil {
		return nil, 0, fmt.Errorf("load location %q for %q: %w", options.Location, options.RootKey, err)
	}
	var head ModuleLocationHead
	if err := database.WithContext(ctx).Where("location_id = ?", location.ID).First(&head).Error; err != nil {
		return nil, 0, fmt.Errorf("load location head for %q: %w", options.Location, err)
	}
	query := database.WithContext(ctx).Model(&ModuleSnapshot{}).Where("root_id = ? AND location_id = ?", root.ID, location.ID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count snapshots for %q: %w", options.Location, err)
	}
	var snapshots []ModuleSnapshot
	if err := query.Order("started_at DESC, id DESC").Limit(options.Limit).Offset(options.Offset).Find(&snapshots).Error; err != nil {
		return nil, 0, fmt.Errorf("list snapshots for %q: %w", options.Location, err)
	}
	bases, err := baseSnapshotIDs(ctx, database, snapshots)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]ModuleSnapshotView, 0, len(snapshots))
	for _, snapshot := range snapshots {
		stats, err := snapshot.Stats()
		if err != nil {
			return nil, 0, err
		}
		row := ModuleSnapshotView{
			ID: snapshot.ID, RootKey: root.RootKey, CanonicalPath: location.CanonicalPath,
			BaseSnapshotID: snapshot.BaseSnapshotID, Revision: snapshot.Revision, GitCommit: snapshot.GitCommit,
			ModuleVersion: snapshot.ModuleVersion, LastModifiedAt: snapshot.LastModifiedAt,
			WorktreeState: snapshot.WorktreeState, Coverage: snapshot.Coverage, Reason: snapshot.Reason, IndexStartedAt: snapshot.IndexStartedAt,
			StartedAt: snapshot.StartedAt, CompletedAt: snapshot.CompletedAt, TaskRunID: snapshot.TaskRunID,
			Head: snapshot.ID == head.SnapshotID, FileCount: stats.FileCount, SymbolCount: stats.SymbolCount,
			OccurrenceCount: stats.OccurrenceCount, SourceBytes: stats.SourceBytes, FilesAdded: stats.FilesAdded,
			FilesChanged: stats.FilesChanged, FilesDeleted: stats.FilesDeleted, SymbolsChanged: stats.SymbolsChanged,
		}
		row.Kind = snapshotKind(snapshot, row.Head || bases[snapshot.ID])
		if row.Head {
			row.HeadVersion = head.Version
		}
		rows = append(rows, row)
	}
	return rows, total, nil
}

// snapshotKind derives a snapshot's kind. A publication that leaves the head in place, a historical
// commit or a dependency version, never becomes a head and so never becomes another snapshot's base,
// while every head is either the current one or the base of the head that superseded it.
func snapshotKind(snapshot ModuleSnapshot, publishedAsHead bool) SnapshotKind {
	switch {
	case snapshot.ModuleVersion != "":
		return SnapshotKindVersioned
	case publishedAsHead:
		return SnapshotKindHead
	default:
		return SnapshotKindHistorical
	}
}

// baseSnapshotIDs reports which of snapshots another snapshot bases on.
func baseSnapshotIDs(ctx context.Context, database *gorm.DB, snapshots []ModuleSnapshot) (map[uuid.UUID]bool, error) {
	ids := make([]uuid.UUID, 0, len(snapshots))
	for _, snapshot := range snapshots {
		ids = append(ids, snapshot.ID)
	}
	bases := map[uuid.UUID]bool{}
	if len(ids) == 0 {
		return bases, nil
	}
	var referenced []uuid.UUID
	if err := database.WithContext(ctx).Model(&ModuleSnapshot{}).Distinct("base_snapshot_id").
		Where("base_snapshot_id IN ?", ids).Pluck("base_snapshot_id", &referenced).Error; err != nil {
		return nil, fmt.Errorf("find snapshots used as bases: %w", err)
	}
	for _, id := range referenced {
		bases[id] = true
	}
	return bases, nil
}
