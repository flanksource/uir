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

func (ModuleRoot) TableName() string { return "modules" }

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

func (ModuleLocation) TableName() string { return "locations" }

// WorktreeState records whether a snapshot's bytes are exactly its Git revision's bytes.
type WorktreeState string

const (
	WorktreeClean   WorktreeState = "clean"
	WorktreeDirty   WorktreeState = "dirty"
	WorktreeUnknown WorktreeState = "unknown"
)

// Coverage is the one extraction-coverage vocabulary shared by snapshots, packages, and documents.
type Coverage string

const (
	CoverageIndexed  Coverage = "indexed"
	CoveragePartial  Coverage = "partial"
	CoverageSyntax   Coverage = "syntax"
	CoverageExcluded Coverage = "excluded"
)

// ModuleSnapshot is a published index run; a row exists only once its publication committed.
type ModuleSnapshot struct {
	ID                uuid.UUID     `gorm:"column:id;primaryKey"`
	RootID            uuid.UUID     `gorm:"column:root_id"`
	LocationID        uuid.UUID     `gorm:"column:location_id"`
	BaseSnapshotID    *uuid.UUID    `gorm:"column:base_snapshot_id"`
	Revision          string        `gorm:"column:revision"`
	WorktreeState     WorktreeState `gorm:"column:worktree_state"`
	ContentSetHash    string        `gorm:"column:content_set_hash"`
	ConfigurationHash string        `gorm:"column:configuration_hash"`
	ContextHash       string        `gorm:"column:context_hash"`
	Coverage          Coverage      `gorm:"column:coverage"`
	PackageCount      int           `gorm:"column:package_count"`
	Diagnostics       JSON          `gorm:"column:diagnostics"`
	StartedAt         time.Time     `gorm:"column:started_at"`
	CompletedAt       time.Time     `gorm:"column:completed_at"`
}

func (ModuleSnapshot) TableName() string { return "snapshots" }

type ModuleLocationHead struct {
	RootID     uuid.UUID `gorm:"column:root_id"`
	LocationID uuid.UUID `gorm:"column:location_id;primaryKey"`
	SnapshotID uuid.UUID `gorm:"column:snapshot_id"`
	Version    int64     `gorm:"column:version"`
}

func (ModuleLocationHead) TableName() string { return "location_heads" }

type ModulePrimary struct {
	RootID     uuid.UUID `gorm:"column:root_id;primaryKey"`
	LocationID uuid.UUID `gorm:"column:location_id"`
}

func (ModulePrimary) TableName() string { return "primary_locations" }

type SourceRevision struct {
	ID          uuid.UUID `gorm:"column:id;primaryKey"`
	RootID      uuid.UUID `gorm:"column:root_id"`
	PathKey     string    `gorm:"column:path_key"`
	ContentHash string    `gorm:"column:content_hash"`
	PackagePath string    `gorm:"column:package_path"`
	SizeBytes   int64     `gorm:"column:size_bytes"`
}

func (SourceRevision) TableName() string { return "source_revisions" }

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

func (SourceDelta) TableName() string { return "source_deltas" }
