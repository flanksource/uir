package storage

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type JSON json.RawMessage

func (value JSON) Value() (driver.Value, error) {
	if len(value) == 0 {
		return "{}", nil
	}
	if !json.Valid(value) {
		return nil, invalidJSONError(value)
	}
	return string(value), nil
}

func (value *JSON) Scan(source any) error {
	if source == nil {
		*value = nil
		return nil
	}
	var raw []byte
	switch source := source.(type) {
	case string:
		raw = []byte(source)
	case []byte:
		raw = source
	default:
		return fmt.Errorf("scan UIR JSON from %T", source)
	}
	if !json.Valid(raw) {
		return invalidJSONError(raw)
	}
	*value = append((*value)[:0], raw...)
	return nil
}

func (JSON) GormDataType() string { return "json" }

func (value JSON) String() string { return string(value) }

func (value JSON) MarshalJSON() ([]byte, error) {
	if len(value) == 0 {
		return []byte("{}"), nil
	}
	if !json.Valid(value) {
		return nil, invalidJSONError(value)
	}
	return value, nil
}

func (value *JSON) UnmarshalJSON(raw []byte) error {
	if !json.Valid(raw) {
		return invalidJSONError(raw)
	}
	*value = append((*value)[:0], raw...)
	return nil
}

func (JSON) GormDBDataType(database *gorm.DB, _ *schema.Field) string {
	if database.Name() == "postgres" {
		return "JSONB"
	}
	return "TEXT"
}

func invalidJSONError(value []byte) error {
	return fmt.Errorf("invalid UIR JSON (%d bytes)", len(value))
}

type Project struct {
	ID         uuid.UUID `gorm:"column:id;primaryKey"`
	ProjectKey string    `gorm:"column:project_key"`
	Name       string    `gorm:"column:name"`
	Properties JSON      `gorm:"column:properties"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

func (Project) TableName() string { return "uir_projects" }

type SnapshotState string

const (
	SnapshotBuilding SnapshotState = "building"
	SnapshotReady    SnapshotState = "ready"
	SnapshotFailed   SnapshotState = "failed"
)

type Snapshot struct {
	ID                uuid.UUID     `gorm:"column:id;primaryKey"`
	ProjectID         uuid.UUID     `gorm:"column:project_id"`
	State             SnapshotState `gorm:"column:state"`
	RevisionSetHash   string        `gorm:"column:revision_set_hash"`
	ConfigurationHash string        `gorm:"column:configuration_hash"`
	ExtractorVersion  string        `gorm:"column:extractor_version"`
	PayloadSchema     string        `gorm:"column:payload_schema"`
	DocumentPayload   JSON          `gorm:"column:document_payload"`
	StartedAt         time.Time     `gorm:"column:started_at"`
	CompletedAt       *time.Time    `gorm:"column:completed_at"`
	Properties        JSON          `gorm:"column:properties"`
}

func (Snapshot) TableName() string { return "uir_snapshots" }

type ProjectHead struct {
	ProjectID   uuid.UUID `gorm:"column:project_id;primaryKey"`
	SnapshotID  uuid.UUID `gorm:"column:snapshot_id"`
	Version     int64     `gorm:"column:version"`
	ActivatedAt time.Time `gorm:"column:activated_at"`
}

func (ProjectHead) TableName() string { return "uir_project_heads" }

type Root struct {
	ID                   uuid.UUID  `gorm:"column:id;primaryKey"`
	SnapshotID           uuid.UUID  `gorm:"column:snapshot_id"`
	ParentRootID         *uuid.UUID `gorm:"column:parent_root_id"`
	RootKey              string     `gorm:"column:root_key"`
	Kind                 string     `gorm:"column:kind"`
	MountPath            string     `gorm:"column:mount_path"`
	RepositoryKey        *string    `gorm:"column:repository_key"`
	RepositoryURI        *string    `gorm:"column:repository_uri"`
	Revision             *string    `gorm:"column:revision"`
	ContentSetHash       string     `gorm:"column:content_set_hash"`
	SubmodulePath        *string    `gorm:"column:submodule_path"`
	PathCase             string     `gorm:"column:path_case"`
	NormalizationVersion string     `gorm:"column:normalization_version"`
	LocalPath            *string    `gorm:"column:local_path"`
	Properties           JSON       `gorm:"column:properties"`
}

func (Root) TableName() string { return "uir_roots" }

type Source struct {
	ID          uuid.UUID  `gorm:"column:id;primaryKey"`
	RootID      uuid.UUID  `gorm:"column:root_id"`
	PathKey     string     `gorm:"column:path_key"`
	DisplayPath string     `gorm:"column:display_path"`
	Kind        string     `gorm:"column:kind"`
	Language    string     `gorm:"column:language"`
	ContentHash string     `gorm:"column:content_hash"`
	SizeBytes   int64      `gorm:"column:size_bytes"`
	ModifiedAt  *time.Time `gorm:"column:modified_at"`
	Properties  JSON       `gorm:"column:properties"`
}

func (Source) TableName() string { return "uir_sources" }

type Node struct {
	ID            uuid.UUID  `gorm:"column:id;primaryKey"`
	SnapshotID    uuid.UUID  `gorm:"column:snapshot_id"`
	RootID        uuid.UUID  `gorm:"column:root_id"`
	ParentID      *uuid.UUID `gorm:"column:parent_id"`
	ChildSlot     string     `gorm:"column:child_slot"`
	Ordinal       int        `gorm:"column:ordinal"`
	NodeType      string     `gorm:"column:node_type"`
	IdentityKey   string     `gorm:"column:identity_key"`
	SymbolKey     string     `gorm:"column:symbol_key"`
	Module        string     `gorm:"column:module"`
	Package       string     `gorm:"column:package"`
	TypeName      string     `gorm:"column:type_name"`
	Method        string     `gorm:"column:method"`
	Field         string     `gorm:"column:field"`
	Signature     string     `gorm:"column:signature"`
	Language      string     `gorm:"column:language"`
	Traits        JSON       `gorm:"column:traits"`
	PayloadSchema string     `gorm:"column:payload_schema"`
	Payload       JSON       `gorm:"column:payload"`
	SemanticHash  string     `gorm:"column:semantic_hash"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
}

func (Node) TableName() string { return "uir_nodes" }

type NodeLocation struct {
	ID        uuid.UUID `gorm:"column:id;primaryKey"`
	RootID    uuid.UUID `gorm:"column:root_id"`
	NodeID    uuid.UUID `gorm:"column:node_id"`
	SourceID  uuid.UUID `gorm:"column:source_id"`
	Role      string    `gorm:"column:role"`
	Ordinal   int       `gorm:"column:ordinal"`
	StartLine *int      `gorm:"column:start_line"`
	EndLine   *int      `gorm:"column:end_line"`
	Column    *int      `gorm:"column:column"`
	IsPrimary bool      `gorm:"column:is_primary"`
}

func (NodeLocation) TableName() string { return "uir_node_locations" }

type Field struct {
	NodeID          uuid.UUID `gorm:"column:node_id;primaryKey"`
	Role            string    `gorm:"column:role"`
	Ordinal         *int      `gorm:"column:ordinal"`
	Label           string    `gorm:"column:label"`
	FieldType       string    `gorm:"column:field_type"`
	NativeType      string    `gorm:"column:native_type"`
	MaxLength       *int      `gorm:"column:max_length"`
	Precision       *int      `gorm:"column:precision"`
	Scale           *int      `gorm:"column:scale"`
	EnumValues      JSON      `gorm:"column:enum_values"`
	TypeRef         JSON      `gorm:"column:type_ref"`
	DefaultValue    JSON      `gorm:"column:default_value"`
	Validation      JSON      `gorm:"column:validation"`
	Visibility      string    `gorm:"column:visibility"`
	IsNullable      bool      `gorm:"column:is_nullable"`
	IsPrimaryKey    bool      `gorm:"column:is_primary_key"`
	IsAutoIncrement bool      `gorm:"column:is_auto_increment"`
	IsUnique        bool      `gorm:"column:is_unique"`
	IsReadOnly      bool      `gorm:"column:is_read_only"`
	IsWriteOnly     bool      `gorm:"column:is_write_only"`
}

func (Field) TableName() string { return "uir_fields" }

type Relationship struct {
	ID               uuid.UUID  `gorm:"column:id;primaryKey"`
	SnapshotID       uuid.UUID  `gorm:"column:snapshot_id"`
	FromRootID       uuid.UUID  `gorm:"column:from_root_id"`
	FromNodeID       uuid.UUID  `gorm:"column:from_node_id"`
	ToSnapshotID     *uuid.UUID `gorm:"column:to_snapshot_id"`
	ToNodeID         *uuid.UUID `gorm:"column:to_node_id"`
	EdgeKey          string     `gorm:"column:edge_key"`
	ToProjectKey     *string    `gorm:"column:to_project_key"`
	ToRootKey        *string    `gorm:"column:to_root_key"`
	ToIdentityKey    string     `gorm:"column:to_identity_key"`
	ToSymbolKey      string     `gorm:"column:to_symbol_key"`
	ToIdentifier     JSON       `gorm:"column:to_identifier"`
	RelationshipType string     `gorm:"column:relationship_type"`
	SourceID         *uuid.UUID `gorm:"column:source_id"`
	StartLine        *int       `gorm:"column:start_line"`
	EndLine          *int       `gorm:"column:end_line"`
	Column           *int       `gorm:"column:column"`
	StatementPath    string     `gorm:"column:statement_path"`
	Text             string     `gorm:"column:text"`
	Payload          JSON       `gorm:"column:payload"`
}

func (Relationship) TableName() string { return "uir_relationships" }
