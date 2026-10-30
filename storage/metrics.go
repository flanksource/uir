package storage

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/flanksource/uir/storage/symbolhandle"
	"github.com/google/uuid"
)

// MetricsVersion identifies the semantics of stored physical-line and callable metrics.
const MetricsVersion = 1

// CallableMetrics describes a declaration's value parameters and physical body span.
// A nil BodyLines means the declaration has no body; nil metrics mean they were not recorded.
type CallableMetrics struct {
	Version    int  `json:"version"`
	ParamCount int  `json:"param_count"`
	BodyLines  *int `json:"body_lines"`
}

func (metrics *CallableMetrics) UnmarshalJSON(data []byte) error {
	var record struct {
		Version    *int            `json:"version"`
		ParamCount *int            `json:"param_count"`
		BodyLines  json.RawMessage `json:"body_lines"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return fmt.Errorf("decode callable metrics: %w", err)
	}
	if record.Version == nil || record.ParamCount == nil || len(record.BodyLines) == 0 {
		return fmt.Errorf("callable metrics require version, param_count and body_lines")
	}
	var bodyLines *int
	if err := json.Unmarshal(record.BodyLines, &bodyLines); err != nil {
		return fmt.Errorf("decode callable body_lines: %w", err)
	}
	*metrics = CallableMetrics{Version: *record.Version, ParamCount: *record.ParamCount, BodyLines: bodyLines}
	return nil
}

// SourceMetrics is the immutable line count of one source revision under a metric version.
type SourceMetrics struct {
	SourceRevisionID uuid.UUID `gorm:"column:source_revision_id;primaryKey"`
	Version          int       `gorm:"column:version;primaryKey;autoIncrement:false"`
	Lines            int       `gorm:"column:lines"`
}

func (SourceMetrics) TableName() string { return "source_metrics" }

// DeclarationMetrics projects declaration positions and optional callable metrics without payloads.
type DeclarationMetrics struct {
	DocumentOrdinal int64  `gorm:"column:document_ordinal;primaryKey;autoIncrement:false"`
	SymbolKey       string `gorm:"column:symbol_key;primaryKey"`
	SymbolHandle    *int64 `gorm:"column:symbol_handle"`
	Version         int    `gorm:"column:version"`
	NameLine        int    `gorm:"column:name_line"`
	NameColumn      int    `gorm:"column:name_column"`
	NameEndLine     int    `gorm:"column:name_end_line"`
	NameEndColumn   int    `gorm:"column:name_end_column"`
	ParamCount      *int   `gorm:"column:param_count"`
	BodyLines       *int   `gorm:"column:body_lines"`
}

func (DeclarationMetrics) TableName() string { return "declaration_metrics" }

func (metrics CallableMetrics) Validate(category symbolhandle.Category) error {
	if category != symbolhandle.CategoryCallable {
		return fmt.Errorf("callable metrics require a callable declaration")
	}
	if metrics.Version != MetricsVersion || metrics.ParamCount < 0 || metrics.BodyLines != nil && *metrics.BodyLines < 1 {
		return fmt.Errorf("invalid callable metrics: expected version %d, nonnegative param_count and positive body_lines or null", MetricsVersion)
	}
	return nil
}
