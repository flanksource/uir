package uir

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

type RecordField struct {
	nodeBase
	Label        string          `json:"label,omitempty"`
	FieldType    RecordFieldType `json:"fieldType"`
	TypeRef      *TypeReference  `json:"typeRef,omitempty" gorm:"serializer:json"`
	DefaultValue *TypedValue     `json:"defaultValue,omitempty"`
	ReadOnly     bool            `json:"readOnly,omitempty"`
	WriteOnly    bool            `json:"writeOnly,omitempty"`
	Validation   FieldValidation `json:"validation,omitempty"`
	Visibility   Visibility      `json:"visibility,omitempty"`
}

type RecordReference struct {
	Name                string              `json:"name"`
	Mapping             string              `json:"mapping,omitempty"`
	RecordReferenceType RecordReferenceType `json:"recordReferenceType,omitempty"`
}

type ValidationValue struct {
	StringValue *string                    `json:"stringValue,omitempty"`
	NumberValue *float64                   `json:"numberValue,omitempty"`
	BoolValue   *bool                      `json:"boolValue,omitempty"`
	ArrayValue  []ValidationValue          `json:"arrayValue,omitempty"`
	ObjectValue map[string]ValidationValue `json:"objectValue,omitempty"`
	Code        *string                    `json:"code,omitempty"`
}

type ValidationRuleOptions struct {
	Message *string                    `json:"message,omitempty"`
	Groups  []string                   `json:"groups,omitempty"`
	Always  *bool                      `json:"always,omitempty"`
	Each    *bool                      `json:"each,omitempty"`
	Context map[string]ValidationValue `json:"context,omitempty"`
}

type ValidationRule struct {
	Provider string                 `json:"provider,omitempty"`
	Name     string                 `json:"name"`
	Args     []ValidationValue      `json:"args,omitempty"`
	Options  *ValidationRuleOptions `json:"options,omitempty"`
}

type GeneratorHint struct {
	Language   string                     `json:"language,omitempty"`
	Framework  string                     `json:"framework,omitempty"`
	Properties map[string]ValidationValue `json:"properties,omitempty"`
}

type FieldValidation struct {
	// Applies to strings and indicates the minimum length
	MinLength *int `json:"minLength,omitempty"`
	// Applies to strings and indicates the maximum length
	MaxLength *int `json:"maxLength,omitempty"`
	// Applies to arrays and indicates the maximum number of items
	MaxItems *int `json:"maxItems,omitempty"`
	// Applies to arrays and indicates the minimum number of items
	MinItems *int `json:"minItems,omitempty"`
	// Applies to objects and indicates the minimum number of properties
	MinProperties *int `json:"minProperties,omitempty"`
	// Applies to objects and indicates the maximum number of properties
	MaxProperties *int `json:"maxProperties,omitempty"`
	// Applies to arrays and indicates whether the items need to be unique
	UniqueItems bool `json:"uniqueItems,omitempty"`

	// Applies to arrays and indicates whether the items need to be sorted
	SortedItems bool   `json:"sortedItems,omitempty"`
	Regex       string `json:"regex,omitempty"`
	// Applies to strings or arrays and indicates the allowed pattern
	Enum []string `json:"enum,omitempty"`

	// Applies to numeric types and indicates the minimum value
	Min *float64 `json:"min,omitempty"`

	MinExclusive *float64 `json:"minExclusive,omitempty"`
	MaxExclusive *float64 `json:"maxExclusive,omitempty"`
	Max          *float64 `json:"max,omitempty"`
	// Date validation, with support for timestamps and date math
	MinDate *string `json:"minDate,omitempty"`
	// Date validation, with support for timestamps and date math
	MaxDate  *string `json:"maxDate,omitempty"`
	Required bool    `json:"required,omitempty"`
	Unique   bool    `json:"unique,omitempty"`
	NonEmpty bool    `json:"nonEmpty,omitempty"`
	// Expressions are custom validation rules that return true or false
	Expressions []Expression `json:"expressions,omitempty"`
	// Rules preserves provider-specific validation directives such as
	// class-validator decorators without losing portability of the generic fields.
	Rules []ValidationRule `json:"rules,omitempty"`
	// Hints allows language/framework-specific generation hints to be attached
	// without overloading metadata properties.
	Hints []GeneratorHint `json:"hints,omitempty"`
	// UnitTypes is a list of allowed unit types for the field
	UnitTypes []UnitType `json:"unitTypes,omitempty"`
	// Conditions carry validation rules that apply only when their condition is met,
	// in the order they should be evaluated.
	Conditions []ConditionalFieldValidation `json:"conditions,omitempty"`
}

// ConditionalFieldValidation applies Validation only when Condition holds. It is a
// pair rather than a map entry because Expression is a struct: encoding/json refuses
// struct map keys, so a keyed shape made the enclosing UIR unmarshalable.
type ConditionalFieldValidation struct {
	Condition  Expression      `json:"condition"`
	Validation FieldValidation `json:"validation"`
}

// Value implements driver.Valuer interface for database serialization.
// Serializes FieldValidation as JSON for storage in database.
func (fv FieldValidation) Value() (driver.Value, error) {
	data, err := json.Marshal(fv)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FieldValidation: %w", err)
	}
	return string(data), nil
}

// Scan implements sql.Scanner interface for database deserialization.
// Parses JSON from database into FieldValidation struct.
func (fv *FieldValidation) Scan(value interface{}) error {
	if value == nil {
		*fv = FieldValidation{}
		return nil
	}

	var data []byte
	switch v := value.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		return fmt.Errorf("failed to scan FieldValidation: expected string or []byte, got %T", value)
	}

	err := json.Unmarshal(data, fv)
	if err != nil {
		return fmt.Errorf("failed to unmarshal FieldValidation: %w", err)
	}

	return nil
}

type Condition string

type RecordValidation struct {
	UniqueFields []string `json:"uniqueFields,omitempty"`
	// Indicates total number of records allowed
	MinRecords *int `json:"minRecords,omitempty"`
	// Indicates total number of records allowed
	MaxRecords *int     `json:"maxRecords,omitempty"`
	AnyOf      []string `json:"anyOf,omitempty"`
	OneOf      []string `json:"oneOf,omitempty"`
	AllOf      []string `json:"allOf,omitempty"`
	NoneOf     []string `json:"noneOf,omitempty"`
	// Expressions are custom validation rules that return true or false
	Expressions []Expression `json:"expressions,omitempty"`
	// Hints provides record-level generator defaults that fields can override.
	Hints []GeneratorHint `json:"hints,omitempty"`
	// Conditions carry validation rules that apply only when their condition is met,
	// in the order they should be evaluated.
	Conditions []ConditionalRecordValidation `json:"conditions,omitempty"`
}

// ConditionalRecordValidation is the record-level counterpart of
// ConditionalFieldValidation.
type ConditionalRecordValidation struct {
	Condition  Expression       `json:"condition"`
	Validation RecordValidation `json:"validation"`
}

type ASTRecord struct {
	nodeBase
	// Extends a record by including all the fields and references from the referenced record
	// When there are conflicts the final record takes precedence, followed by the first record in the extends list etc.
	// This allows for reusing and composing records in a flexible way
	Extends     []ASTRecord       `json:"extends,omitempty"`
	Description string            `json:"description,omitempty"`
	RecordType  RecordType        `json:"recordType,omitempty"`
	Fields      []RecordField     `json:"fields,omitempty"`
	Examples    []string          `json:"examples,omitempty"`
	Validation  RecordValidation  `json:"validation,omitempty"`
	References  []RecordReference `json:"references,omitempty"`
}

// Normalizes the record by combining all records referencing in the Extends field
func (r *ASTRecord) Normalize() ASTRecord {
	normalized := *r
	// TODO implement
	return normalized
}

// PersistentBodyMixin implementation for ASTRecord
func (r ASTRecord) GetPersistentBody() json.RawMessage {
	if len(r.Fields) == 0 {
		return nil
	}
	data, err := json.Marshal(r.Fields)
	if err != nil {
		return nil
	}
	return data
}

func (r *ASTRecord) LoadPersistentBody(data json.RawMessage) error {
	if len(data) == 0 {
		r.Fields = nil
		return nil
	}
	var fields []RecordField
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("failed to unmarshal ASTRecord fields: %w", err)
	}
	r.Fields = fields
	return nil
}

func (f RecordField) GetChildren() []Node {
	return nil
}

func (f RecordField) GetType() NodeType {
	return NodeTypeField
}

func (e ASTRecord) GetChildren() []Node {
	children := make([]Node, len(e.Fields))
	for i, f := range e.Fields {
		children[i] = f
	}
	return children
}
