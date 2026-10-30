package query

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
)

type declarationMetricKey struct{ document, handle int64 }

func (index *indexContext) declarationMetrics(ctx context.Context, postings []scopedPosting) (map[declarationMetricKey]storage.DeclarationMetrics, error) {
	if !index.database.WithContext(ctx).Migrator().HasTable(&storage.DeclarationMetrics{}) {
		return nil, fmt.Errorf("declaration metric schema is unavailable; open the index read-write to upgrade it, then reindex or republish")
	}
	ordinals := map[int64]bool{}
	for _, posting := range postings {
		ordinals[index.scopes[posting.scope].documents[posting.document].Document.Ordinal] = true
	}
	ids := slices.Sorted(maps.Keys(ordinals))
	metrics := make(map[declarationMetricKey]storage.DeclarationMetrics)
	for start := 0; start < len(ids); start += lookupBatch {
		var rows []storage.DeclarationMetrics
		if err := index.database.WithContext(ctx).Where("document_ordinal IN ? AND version = ? AND symbol_handle IS NOT NULL", ids[start:min(start+lookupBatch, len(ids))], storage.MetricsVersion).Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load declaration metrics: %w", err)
		}
		for _, row := range rows {
			key := declarationMetricKey{row.DocumentOrdinal, *row.SymbolHandle}
			if _, duplicate := metrics[key]; duplicate {
				return nil, fmt.Errorf("document %d has several metric declarations of handle %d", key.document, key.handle)
			}
			metrics[key] = row
		}
	}
	return metrics, nil
}

func (index *indexContext) projectedDeclarations(ctx context.Context, postings []scopedPosting, kind string) ([]ModuleMatch, error) {
	metrics, err := index.declarationMetrics(ctx, postings)
	if err != nil {
		return nil, err
	}
	handles := map[int64]bool{}
	for _, posting := range postings {
		handle, found := index.symbols.handle(posting.symbol)
		if !found {
			return nil, fmt.Errorf("metric declaration has no handle for symbol %s", posting.symbol)
		}
		handles[handle] = true
	}
	symbols, err := storage.OwnedSymbols(ctx, index.database, slices.Sorted(maps.Keys(handles)))
	if err != nil {
		return nil, err
	}
	matches := make([]ModuleMatch, 0, len(postings))
	for _, posting := range postings {
		scope := index.scopes[posting.scope]
		active := scope.documents[posting.document]
		handle, known := index.symbols.handle(posting.symbol)
		if !known {
			return nil, fmt.Errorf("metric declaration has no handle for symbol %s", posting.symbol)
		}
		metric, found := metrics[declarationMetricKey{active.Document.Ordinal, handle}]
		if !found {
			return nil, missingDeclarationMetrics(scope, active, posting.symbol)
		}
		owned := symbols[handle]
		symbol := ModuleSymbol{ID: owned.ID, Kind: owned.Kind, ModuleKey: owned.ModuleKey, PackagePath: owned.PackagePath, Name: owned.Name, Owner: strings.Join(owned.Owners, ".")}
		matches = append(matches, ModuleMatch{Kind: kind, NodeKind: symbol.Kind, RootKey: scope.root.RootKey, Location: scope.location.CanonicalPath,
			SnapshotID: scope.snapshot.ID.String(), SourceID: active.Source.ID.String(), Path: active.Source.PathKey, PackagePath: active.Source.PackagePath,
			SymbolID: symbol.ID, Identifier: index.identifier(symbol), Role: "definition", Coverage: string(active.Document.Coverage),
			Line: &metric.NameLine, Column: &metric.NameColumn, EndLine: &metric.NameEndLine, EndColumn: &metric.NameEndColumn})
	}
	return matches, nil
}

// declarationMeasures loads the stored producer metric values of the postings' documents by
// declaration.
func (index *indexContext) declarationMeasures(ctx context.Context, postings []scopedPosting) (map[declarationMetricKey]map[string]int64, error) {
	if !index.database.WithContext(ctx).Migrator().HasTable(&storage.DeclarationMeasure{}) {
		return nil, fmt.Errorf("declaration measure schema is unavailable; open the index read-write to upgrade it, then republish")
	}
	ordinals := map[int64]bool{}
	for _, posting := range postings {
		ordinals[index.scopes[posting.scope].documents[posting.document].Document.Ordinal] = true
	}
	ids := slices.Sorted(maps.Keys(ordinals))
	measures := make(map[declarationMetricKey]map[string]int64)
	for start := 0; start < len(ids); start += lookupBatch {
		var rows []storage.DeclarationMeasure
		if err := index.database.WithContext(ctx).Where("document_ordinal IN ?", ids[start:min(start+lookupBatch, len(ids))]).Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load declaration measures: %w", err)
		}
		for _, row := range rows {
			key := declarationMetricKey{row.DocumentOrdinal, row.SymbolHandle}
			if measures[key] == nil {
				measures[key] = map[string]int64{}
			}
			measures[key][row.Name] = row.Value
		}
	}
	return measures, nil
}

func missingDeclarationMeasure(declaration recordDeclaration, symbol, kind, name string) error {
	return fmt.Errorf("snapshot %s source %q declaration %s of kind %s has no recorded value for metric %q; republish this revision with the measures its kind declares (document format %d) and query its new snapshot",
		declaration.scope.snapshot.ID, declaration.active.Source.PathKey, symbol, kind, name, storage.DocumentFormatVersion)
}

func missingDeclarationMetrics(scope indexScope, active storage.ActiveDocument, symbol string) error {
	return fmt.Errorf("snapshot %s source %q declaration %s has no recorded callable metrics; reindex or republish this revision with callable metrics and query its new snapshot", scope.snapshot.ID, active.Source.PathKey, symbol)
}
