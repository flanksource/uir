package storage

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

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

// Field is a serialized Go field projection inside a document symbol entry, not a table model.
type Field struct {
	NodeID          uuid.UUID
	Role            string
	Ordinal         *int
	Label           string
	FieldType       string
	NativeType      string
	MaxLength       *int
	Precision       *int
	Scale           *int
	EnumValues      JSON
	TypeRef         JSON
	DefaultValue    JSON
	Validation      JSON
	Visibility      string
	IsNullable      bool
	IsPrimaryKey    bool
	IsAutoIncrement bool
	IsUnique        bool
	IsReadOnly      bool
	IsWriteOnly     bool
}
