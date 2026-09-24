package uir

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

type Expression struct {
	SourceCode     `json:",inline"`
	ExpressionType ExpressionType `json:"type,omitempty"`
	Expression     string         `json:"expression,omitempty"`
}

// Location represents a file location with line range
type Location struct {
	Path         string     `json:"path,omitempty"`
	StartLine    *int       `json:"start_line,omitempty"`
	EndLine      *int       `json:"end_line,omitempty"`
	Column       *int       `json:"column,omitempty"`
	LastModified *time.Time `json:"last_modified,omitempty"`
}

func (l Location) IsEmpty() bool {
	return l.Path == "" && l.StartLine == nil && l.EndLine == nil && l.Column == nil
}

func (l Location) GetSignature() string {
	return l.String()
}

func (l Location) String() string {
	s := l.Path
	if l.StartLine != nil {
		s += fmt.Sprintf(":%d", *l.StartLine)
		if l.EndLine != nil && *l.EndLine != *l.StartLine {
			s += fmt.Sprintf("-%d", *l.EndLine)
		}
		if l.Column != nil {
			s += fmt.Sprintf(":%d", *l.Column)
		}
	}
	return s
}

// Value implements driver.Valuer interface for database serialization.
func (l Location) Value() (driver.Value, error) {
	if l.IsEmpty() {
		return nil, nil
	}
	data, err := json.Marshal(l)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Location: %w", err)
	}
	return string(data), nil
}

// Scan implements sql.Scanner interface for database deserialization.
func (l *Location) Scan(value interface{}) error {
	if value == nil {
		*l = Location{}
		return nil
	}

	var data []byte
	switch v := value.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		return fmt.Errorf("failed to scan Location: expected string or []byte, got %T", value)
	}

	err := json.Unmarshal(data, l)
	if err != nil {
		return fmt.Errorf("failed to unmarshal Location: %w", err)
	}
	return nil
}

type ParamsDef []RecordField

// Value implements driver.Valuer interface for database serialization.
// Serializes ParamsDef as JSON for storage in database.
func (p ParamsDef) Value() (driver.Value, error) {
	if len(p) == 0 {
		return nil, nil
	}

	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ParamsDef: %w", err)
	}
	return string(data), nil
}

// Scan implements sql.Scanner interface for database deserialization.
// Parses JSON from database into ParamsDef slice.
func (p *ParamsDef) Scan(value interface{}) error {
	if value == nil {
		*p = nil
		return nil
	}

	var data []byte
	switch v := value.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		return fmt.Errorf("failed to scan ParamsDef: expected string or []byte, got %T", value)
	}

	err := json.Unmarshal(data, p)
	if err != nil {
		return fmt.Errorf("failed to unmarshal ParamsDef: %w", err)
	}

	return nil
}

type Annotation struct {
	Identifier `json:",inline"`
	Value      TypedValue            `json:"value,omitempty"`
	Values     map[string]TypedValue `json:"values,omitempty"`
}

type Metadata struct {
	Annotations []Annotation   `json:"annotations,omitempty" gorm:"serializer:json"`
	Comments    []Comment      `json:"comments,omitempty" gorm:"serializer:json"`
	Properties  map[string]any `json:"properties,omitempty" gorm:"serializer:json"`
}

type EnvironmentIdentifier struct {
	// Name of the environment e.g. "production", "staging", "dev"
	Environment string `json:"environment,omitempty"`
	URN         string `json:"urn,omitempty"`
}

func (e EnvironmentIdentifier) String() string {
	return e.Environment
}

// VariableRef represents a reference to a variable in the current scope of a block
type VariableRef struct {
	// The name or path of a variable in the current scope
	Name string `json:"name"`
	// Optional default value if the variable is null in the block
	DefaultValue *TypedValue `json:"defaultValue,omitempty"`
}

func (v VariableRef) GetType() string {
	return string(ASTStatementRef)
}

func (v VariableRef) String() string {
	return v.Name
}

type ScopedVariableRef struct {
	VariableRef `json:",inline"`
	Identifier  `json:",inline"`
}

// GetStatementType implements Statement.
func (v ScopedVariableRef) GetStatementType() StatementType {
	return ASTStatementRefScopeVar
}

func (v ScopedVariableRef) GetType() StatementType {
	return ASTStatementRefPackageVar
}

func (v ScopedVariableRef) String() string {
	return v.Identifier.String() + "." + v.Name
}

func (v ScopedVariableRef) GetSignature() string {
	return v.String()
}

type SourceCode struct {
	Location `json:",inline"`
	Content  *string `json:"content,omitempty"`
	Language *string `json:"language,omitempty"`
}
