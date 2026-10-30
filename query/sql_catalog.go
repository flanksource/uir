package query

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

type SQLCatalog struct {
	Functions []SQLFunction         `json:"functions"`
	Kinds     []SQLKind             `json:"kinds"`
	Fields    map[string]string     `json:"fields"`
	Domains   storage.DomainCatalog `json:"domains"`
}

type SQLKind struct {
	Name     string                `json:"name"`
	Category symbolhandle.Category `json:"category"`
}

func (pipeline *Pipeline) QueryCatalog(ctx context.Context) (SQLCatalog, error) {
	if pipeline == nil || pipeline.database == nil {
		return SQLCatalog{}, fmt.Errorf("UIR query database is required")
	}
	kinds, err := storage.LoadSymbolKinds(ctx, pipeline.database)
	if err != nil {
		return SQLCatalog{}, err
	}
	domains, err := storage.LoadDomainCatalog(ctx, pipeline.database)
	if err != nil {
		return SQLCatalog{}, err
	}
	fields, err := sqlPropertyFields(&sqlSource{}, kinds, domains)
	if err != nil {
		return SQLCatalog{}, err
	}
	registered := make([]SQLKind, 0, len(kinds.All()))
	maps.Copy(fields, sqlFileFields)
	for _, kind := range kinds.All() {
		registered = append(registered, SQLKind{Name: kind.Name, Category: kind.Category})
	}
	return SQLCatalog{Functions: slices.Clone(SQLFunctions), Kinds: registered, Fields: fields, Domains: domains}, nil
}
