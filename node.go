package uir

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/clicky/api"
)

type Value struct {
	// Literal value as string, or SQL,  pkg.Type.method for method call, expr for expression
	Value string `json:"value,omitempty"`
	// formatted value like icon or colored text
	Text      api.Textable    `json:"text,omitempty"`
	FieldType RecordFieldType `json:"field_type,omitempty"`
	// being called
	Constant bool `json:"constant,omitempty"`
	// params to a method call
	Params map[string]Value `json:"params,omitempty"`
}

func (v Value) IsEmpty() bool {
	return v.Value == "" && len(v.Params) == 0 && (v.Text == nil || v.Text.String() == "")
}

type TypedValue struct {
	Str   *string    `json:"string,omitempty"`
	Float *float64   `json:"float,omitempty"`
	Int   *int64     `json:"int,omitempty"`
	Bool  *bool      `json:"bool,omitempty"`
	Date  *time.Time `json:"date,omitempty"`
	Unit  *Unit      `json:"unit,omitempty"`
}

func (t TypedValue) IsEmpty() bool {
	return t.GetValue() == nil
}

// GetValue returns the underlying value as interface{}.
func (t TypedValue) GetValue() any {
	if t.Str != nil {
		return *t.Str
	}
	if t.Float != nil {
		return *t.Float
	}
	if t.Int != nil {
		return *t.Int
	}
	if t.Bool != nil {
		return *t.Bool
	}
	if t.Date != nil {
		return *t.Date
	}
	if t.Unit != nil {
		return *t.Unit
	}
	return nil
}

func (t TypedValue) String() string {
	val := t.GetValue()
	if val == nil {
		return ""
	}
	s := fmt.Sprintf("%v", val)
	if t.Unit != nil {
		s = s + " " + t.Unit.String()
	}
	return s
}

// Value implements driver.Valuer interface for database serialization.
// Serializes TypedValue as JSON for storage in database.
func (t TypedValue) Value() (driver.Value, error) {
	if t.IsEmpty() {
		return nil, nil
	}

	data, err := json.Marshal(t)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal TypedValue: %w", err)
	}

	return string(data), nil
}

// Scan implements sql.Scanner interface for database deserialization.
// Parses JSON from database into TypedValue struct.
func (t *TypedValue) Scan(value interface{}) error {
	if value == nil {
		*t = TypedValue{}
		return nil
	}

	var data []byte
	switch v := value.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		return fmt.Errorf("failed to scan TypedValue: expected string or []byte, got %T", value)
	}

	err := json.Unmarshal(data, t)
	if err != nil {
		return fmt.Errorf("failed to unmarshal TypedValue: %w", err)
	}

	return nil
}

type Relationship interface {
	GetFrom() Node
	GetTo() Node
	GetRelationshipType() RelationshipType
	GetLocation() Location
}

type Commented interface {
	GetComments() []Comment
}

type Node interface {
	// GetType must return a static string representing the node type, used for polymorphic marshaling
	GetType() NodeType
	Hash() string
	GetChildren() []Node
	Pretty() api.Text
	GetLocation() Location
	GetLanguage() string
	// GetFullPath returns the full path of the node (e.g., module.package.type.method)
	GetIdentifier() Identifier
}
