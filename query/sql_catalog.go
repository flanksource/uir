package query

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

// SQLCatalog is what a SQL query can name: its functions and kinds, the fields of a symbol source,
// the fields of each table function (files(), package_imports()) by name, and the domain catalogue.
type SQLCatalog struct {
	Functions []SQLFunction                `json:"functions"`
	Kinds     []SQLKind                    `json:"kinds"`
	Fields    map[string]string            `json:"fields"`
	Tables    map[string]map[string]string `json:"tables"`
	Domains   storage.DomainCatalog        `json:"domains"`
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
	tables := make(map[string]map[string]string, len(sqlTables))
	for name, table := range sqlTables {
		tables[name] = maps.Clone(table.fields)
	}
	registered := make([]SQLKind, 0, len(kinds.All()))
	for _, kind := range kinds.All() {
		registered = append(registered, SQLKind{Name: kind.Name, Category: kind.Category})
	}
	return SQLCatalog{Functions: slices.Clone(SQLFunctions), Kinds: registered, Fields: fields, Tables: tables, Domains: domains}, nil
}
