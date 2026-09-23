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

type ModuleSnapshotView struct {
	ID             uuid.UUID     `json:"id"`
	RootKey        string        `json:"root_key"`
	CanonicalPath  string        `json:"canonical_path"`
	BaseSnapshotID *uuid.UUID    `json:"base_snapshot_id,omitempty"`
	State          SnapshotState `json:"state"`
	Revision       string        `json:"revision"`
	StartedAt      time.Time     `json:"started_at"`
	CompletedAt    *time.Time    `json:"completed_at,omitempty"`
	Head           bool          `json:"head"`
	HeadVersion    int64         `json:"head_version,omitempty"`
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
	rows := make([]ModuleSnapshotView, 0, len(snapshots))
	for _, snapshot := range snapshots {
		row := ModuleSnapshotView{
			ID: snapshot.ID, RootKey: root.RootKey, CanonicalPath: location.CanonicalPath,
			BaseSnapshotID: snapshot.BaseSnapshotID, State: snapshot.State, Revision: snapshot.Revision,
			StartedAt: snapshot.StartedAt, CompletedAt: snapshot.CompletedAt, Head: snapshot.ID == head.SnapshotID,
		}
		if row.Head {
			row.HeadVersion = head.Version
		}
		rows = append(rows, row)
	}
	return rows, total, nil
}
