package storage

import (
	"time"

	"github.com/google/uuid"
)

type ModuleRoot struct {
	ID        uuid.UUID `gorm:"column:id;primaryKey"`
	RootKey   string    `gorm:"column:root_key"`
	Name      string    `gorm:"column:name"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (ModuleRoot) TableName() string { return "uir_module_roots" }

type ModuleLocation struct {
	ID               uuid.UUID  `gorm:"column:id;primaryKey"`
	RootID           uuid.UUID  `gorm:"column:root_id"`
	CanonicalPath    string     `gorm:"column:canonical_path"`
	ParentLocationID *uuid.UUID `gorm:"column:parent_location_id"`
	MountPath        string     `gorm:"column:mount_path"`
	Kind             string     `gorm:"column:kind"`
	RepositoryURI    *string    `gorm:"column:repository_uri"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
}

func (ModuleLocation) TableName() string { return "uir_module_locations" }

type ModuleSnapshot struct {
	ID                uuid.UUID     `gorm:"column:id;primaryKey"`
	RootID            uuid.UUID     `gorm:"column:root_id"`
	LocationID        uuid.UUID     `gorm:"column:location_id"`
	BaseSnapshotID    *uuid.UUID    `gorm:"column:base_snapshot_id"`
	State             SnapshotState `gorm:"column:state"`
	Revision          string        `gorm:"column:revision"`
	ContentSetHash    string        `gorm:"column:content_set_hash"`
	ConfigurationHash string        `gorm:"column:configuration_hash"`
	ExtractorVersion  string        `gorm:"column:extractor_version"`
	StartedAt         time.Time     `gorm:"column:started_at"`
	CompletedAt       *time.Time    `gorm:"column:completed_at"`
}

func (ModuleSnapshot) TableName() string { return "uir_module_snapshots" }

type ModuleLocationHead struct {
	RootID     uuid.UUID `gorm:"column:root_id"`
	LocationID uuid.UUID `gorm:"column:location_id;primaryKey"`
	SnapshotID uuid.UUID `gorm:"column:snapshot_id"`
	Version    int64     `gorm:"column:version"`
}

func (ModuleLocationHead) TableName() string { return "uir_module_location_heads" }

type ModulePrimary struct {
	RootID     uuid.UUID `gorm:"column:root_id;primaryKey"`
	LocationID uuid.UUID `gorm:"column:location_id"`
}

func (ModulePrimary) TableName() string { return "uir_module_primaries" }

type SourceRevision struct {
	ID               uuid.UUID `gorm:"column:id;primaryKey"`
	RootID           uuid.UUID `gorm:"column:root_id"`
	PathKey          string    `gorm:"column:path_key"`
	ContentHash      string    `gorm:"column:content_hash"`
	PackagePath      string    `gorm:"column:package_path"`
	ExtractorVersion string    `gorm:"column:extractor_version"`
	SizeBytes        int64     `gorm:"column:size_bytes"`
	Projection       JSON      `gorm:"column:projection"`
}

func (SourceRevision) TableName() string { return "uir_source_revisions" }

type SourceOperation string

const (
	SourceSet    SourceOperation = "set"
	SourceDelete SourceOperation = "delete"
)

type SourceDelta struct {
	SnapshotID uuid.UUID       `gorm:"column:snapshot_id;primaryKey"`
	RootID     uuid.UUID       `gorm:"column:root_id"`
	PathKey    string          `gorm:"column:path_key;primaryKey"`
	RevisionID *uuid.UUID      `gorm:"column:revision_id"`
	Operation  SourceOperation `gorm:"column:operation"`
}

func (SourceDelta) TableName() string { return "uir_source_deltas" }
