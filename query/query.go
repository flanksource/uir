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
	// OperationCallers selects the call occurrences that target one symbol.
	OperationCallers Operation = "callers"
	// OperationCallees selects the call occurrences one symbol's declaration encloses.
	OperationCallees Operation = "callees"
	// OperationUnresolvedCalls selects call edges whose target is unresolved.
	OperationUnresolvedCalls Operation = "unresolved_calls"
	// OperationReferences selects every non-declaring occurrence of one symbol.
	OperationReferences Operation = "references"
	// OperationDefinitions selects the declarations of one symbol.
	OperationDefinitions Operation = "definitions"
	// OperationImplementations selects the types that implement one interface.
	OperationImplementations Operation = "implementations"
	// OperationSearch selects declared symbols by name prefix.
	OperationSearch Operation = "search"
)

// indexed reports whether the operation reads symbols and postings rather than scanning documents.
func (operation Operation) indexed() bool {
	switch operation {
	case OperationNodes, OperationUnresolvedCalls:
		return false
	}
	return true
}

// Predicate is one exact structured field comparison produced by the PEG parser.
type Predicate struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// Query is the typed syntax tree consumed by the resolution pipeline. Dispatch asks callers to
// include calls through the interfaces the target's receiver implements; Search is the search input.
type Query struct {
	Operation  Operation   `json:"operation"`
	Predicates []Predicate `json:"predicates,omitempty"`
	Dispatch   bool        `json:"dispatch,omitempty"`
	Search     string      `json:"search,omitempty"`
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
