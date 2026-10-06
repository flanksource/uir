// Package query parses and resolves database-backed UIR queries.
package query

import (
	"context"
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
	OperationInheritors        Operation = "inheritors"
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

// Selector is a typed selector. SymbolKind is the registered kind a kind: selector names, such as
// oipa.rule; Pattern then matches the names of that kind's symbols, every one when it was omitted.
// Owner is the glob an Entity:Field reference matches against the name of a field's direct owner;
// Pattern then matches the field's own name.
type Selector struct {
	Kind          string `json:"kind"`
	Pattern       string `json:"pattern"`
	ModulePattern string `json:"module_pattern,omitempty"`
	SymbolKind    string `json:"symbol_kind,omitempty"`
	Owner         string `json:"owner,omitempty"`
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

// Pipeline resolves parsed queries against relational UIR storage. One pipeline may serve many
// queries, concurrently: it keeps what a published index never changes (decoded documents, each
// snapshot's active documents and defined symbols, symbol rows and handles) across them, and checks on
// every query, against the location heads, primaries, handle layout, and kind registry, whether the
// rest it keeps still holds.
type Pipeline struct {
	database *gorm.DB
	options  pipelineOptions
	cache    *indexCache
	// query is set on the per-query copy session makes, which validates the cache once and keeps the
	// generation it validated in validated.
	query     bool
	validated *cacheGeneration
}

// generation is the cache generation the query runs against, validated once per query.
func (pipeline *Pipeline) generation(ctx context.Context) (*cacheGeneration, error) {
	if pipeline.validated != nil {
		return pipeline.validated, nil
	}
	generation, err := pipeline.cache.current(ctx, pipeline.database)
	if err != nil {
		return nil, err
	}
	if pipeline.query {
		pipeline.validated = generation
	}
	return generation, nil
}

// NewPipeline creates a query pipeline for an initialized UIR database.
func NewPipeline(database *gorm.DB, options ...PipelineOption) (*Pipeline, error) {
	if database == nil {
		return nil, errors.New("UIR query database is required")
	}
	configured := pipelineOptions{documents: DefaultDocumentCache}
	for _, option := range options {
		option(&configured)
	}
	if err := configured.validate(); err != nil {
		return nil, err
	}
	return &Pipeline{database: database, options: configured, cache: newIndexCache(configured.documents)}, nil
}
