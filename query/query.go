// Package query parses and resolves database-backed UIR queries.
package query

import (
	"errors"

	"gorm.io/gorm"
)

// Operation identifies the query result shape and graph direction.
type Operation string

const (
	// OperationNodes selects nodes without traversing relationships.
	OperationNodes Operation = "nodes"
	// OperationCallers selects nodes with resolved call edges to one target.
	OperationCallers Operation = "callers"
	// OperationCallees selects nodes reached by resolved call edges from one target.
	OperationCallees Operation = "callees"
	// OperationUnresolvedCalls selects call edges whose target is unresolved.
	OperationUnresolvedCalls Operation = "unresolved_calls"
)

// Predicate is one exact structured field comparison produced by the PEG parser.
type Predicate struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// Query is the typed syntax tree consumed by the resolution pipeline.
type Query struct {
	Operation  Operation   `json:"operation"`
	Predicates []Predicate `json:"predicates,omitempty"`
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
