package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SnapshotDependency struct {
	SnapshotID       uuid.UUID  `gorm:"column:snapshot_id;primaryKey" json:"-"`
	ModulePath       string     `gorm:"column:module_path;primaryKey" json:"module_path"`
	DeclaredVersion  string     `gorm:"column:declared_version" json:"declared_version"`
	Indirect         bool       `gorm:"column:indirect" json:"indirect"`
	ReplacePath      string     `gorm:"column:replace_path" json:"replace_path,omitempty"`
	ReplaceVersion   string     `gorm:"column:replace_version" json:"replace_version,omitempty"`
	SelectedVersion  string     `gorm:"column:selected_version" json:"selected_version,omitempty"`
	TargetSnapshotID *uuid.UUID `gorm:"column:target_snapshot_id" json:"target_snapshot_id,omitempty"`
	TargetRootKey    string     `gorm:"-" json:"target_root_key,omitempty"`
	TargetLocation   string     `gorm:"-" json:"target_location,omitempty"`
	UnresolvedReason string     `gorm:"column:unresolved_reason" json:"unresolved_reason,omitempty"`
}

func (SnapshotDependency) TableName() string { return "snapshot_dependencies" }

type SnapshotDependenciesResult struct {
	Captured bool                 `json:"captured"`
	Items    []SnapshotDependency `json:"items"`
}

func ModuleDependencies(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID) (SnapshotDependenciesResult, error) {
	if database == nil || snapshotID == uuid.Nil {
		return SnapshotDependenciesResult{}, errors.New("module dependencies require a database and snapshot ID")
	}
	var snapshot ModuleSnapshot
	if err := database.WithContext(ctx).Where("id = ?", snapshotID).Take(&snapshot).Error; err != nil {
		return SnapshotDependenciesResult{}, fmt.Errorf("load snapshot %s: %w", snapshotID, err)
	}
	result := SnapshotDependenciesResult{Captured: snapshot.DependencySetHash != nil, Items: []SnapshotDependency{}}
	if !result.Captured {
		return result, nil
	}
	if err := database.WithContext(ctx).Where("snapshot_id = ?", snapshotID).Order("module_path").Find(&result.Items).Error; err != nil {
		return SnapshotDependenciesResult{}, fmt.Errorf("load dependencies of snapshot %s: %w", snapshotID, err)
	}
	for i := range result.Items {
		edge := &result.Items[i]
		if edge.TargetSnapshotID == nil {
			continue
		}
		var target struct {
			RootKey       string `gorm:"column:root_key"`
			CanonicalPath string `gorm:"column:canonical_path"`
		}
		if err := database.WithContext(ctx).Table("snapshots AS snapshot").
			Select("root.root_key, location.canonical_path").
			Joins("JOIN modules AS root ON root.id = snapshot.root_id").
			Joins("JOIN locations AS location ON location.id = snapshot.location_id").
			Where("snapshot.id = ?", *edge.TargetSnapshotID).Take(&target).Error; err != nil {
			return SnapshotDependenciesResult{}, fmt.Errorf("load target snapshot %s: %w", *edge.TargetSnapshotID, err)
		}
		edge.TargetRootKey, edge.TargetLocation = target.RootKey, target.CanonicalPath
	}
	return result, nil
}

type SourceBlob struct {
	ContentHash string `gorm:"column:content_hash;primaryKey"`
	Content     []byte `gorm:"column:content"`
}

func (SourceBlob) TableName() string { return "source_blobs" }
