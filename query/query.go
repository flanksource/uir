// Package query parses and resolves database-backed UIR queries.
package query

import (
	"errors"

	"gorm.io/gorm"
)

// Operation identifies the query result shape and graph direction.
type Operation string

const (
	OperationResolve           Operation = "resolve"
	OperationIncoming          Operation = "incoming"
	OperationOutgoing          Operation = "outgoing"
	OperationDefinition        Operation = "definition"
	OperationImplementers      Operation = "implementers"
	OperationMethods           Operation = "methods"
	OperationTransitiveCallers Operation = "transitive_callers"
	OperationSet               Operation = "set"
	OperationPath              Operation = "path"
)

// Query is the typed syntax tree consumed by the resolution pipeline.
type Query struct {
	Expr *Expr `json:"expr"`
}

type ExprKind string

const (
	ExprSymbol       ExprKind = "symbol"
	ExprSelector     ExprKind = "selector"
	ExprModifier     ExprKind = "modifier"
	ExprRelation     ExprKind = "relation"
	ExprIntersection ExprKind = "intersection"
	ExprUnion        ExprKind = "union"
	ExprPath         ExprKind = "path"
)

type Filter struct {
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

type Selector struct {
	Kind          string `json:"kind"`
	Pattern       string `json:"pattern"`
	ModulePattern string `json:"module_pattern,omitempty"`
}

type TypedModifier struct {
	Include  bool     `json:"include"`
	Selector Selector `json:"selector"`
}

type Expr struct {
	Kind      ExprKind        `json:"kind"`
	Symbol    string          `json:"symbol,omitempty"`
	Selector  *Selector       `json:"selector,omitempty"`
	Modifiers []TypedModifier `json:"modifiers,omitempty"`
	Relation  string          `json:"relation,omitempty"`
	Depth     int             `json:"depth,omitempty"`
	Filters   []Filter        `json:"filters,omitempty"`
	Left      *Expr           `json:"left,omitempty"`
	Right     *Expr           `json:"right,omitempty"`
}

// ResolutionStage records one successful, externally visible pipeline decision.
type ResolutionStage struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Pipeline resolves parsed queries against relational UIR storage.
type Pipeline struct {
	database *gorm.DB
}

// NewPipeline creates a query pipeline for an initialized UIR database.
func NewPipeline(database *gorm.DB) (*Pipeline, error) {
	if database == nil {
		return nil, errors.New("UIR query database is required")
	}
	return &Pipeline{database: database}, nil
}
