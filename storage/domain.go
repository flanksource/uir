package storage

import "github.com/flanksource/uir"

type SymbolPropertySchema struct {
	Name   string `gorm:"column:name;primaryKey" json:"name"`
	Schema JSON   `gorm:"column:schema" json:"schema"`
}

func (SymbolPropertySchema) TableName() string { return "symbol_property_schemas" }

type RelationshipKind struct {
	Name   string               `gorm:"column:name;primaryKey" json:"name"`
	Type   uir.RelationshipType `gorm:"column:type" json:"type"`
	Schema JSON                 `gorm:"column:schema" json:"schema,omitempty"`
}

func (RelationshipKind) TableName() string { return "relationship_kinds" }

type DocumentRelationship struct {
	Source     string               `json:"source"`
	Targets    []string             `json:"targets"`
	Reference  string               `json:"reference,omitempty"`
	Kind       string               `json:"kind"`
	Props      map[string]any       `json:"props,omitempty"`
	Resolution string               `json:"resolution"`
	Evidence   RelationshipEvidence `json:"evidence"`
}

type RelationshipEvidence struct {
	Range Range    `json:"range"`
	Bytes ByteSpan `json:"bytes"`
	Text  string   `json:"text"`
}
