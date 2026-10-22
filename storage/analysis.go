package storage

import "github.com/google/uuid"

type AnalysisMode string

const (
	AnalysisSyntax AnalysisMode = "syntax"
	AnalysisTyped  AnalysisMode = "typed"
)

type AnalysisHead struct {
	LocationID        uuid.UUID    `gorm:"column:location_id;primaryKey"`
	Mode              AnalysisMode `gorm:"column:mode;primaryKey"`
	ConfigurationHash string       `gorm:"column:configuration_hash;primaryKey"`
	SnapshotID        uuid.UUID    `gorm:"column:snapshot_id"`
	Version           int64        `gorm:"column:version"`
	Inputs            JSON         `gorm:"column:inputs"`
}

func (AnalysisHead) TableName() string { return "analysis_heads" }
