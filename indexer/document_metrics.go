package indexer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

type metricPublication struct {
	document storage.Document
	handles  map[string]int64
}

func publishDeclarationMetrics(ctx context.Context, database *gorm.DB, publication metricPublication) error {
	var content storage.DocumentContent
	if err := json.Unmarshal(publication.document.Content, &content); err != nil {
		return fmt.Errorf("read metrics of document %s: %w", publication.document.ID, err)
	}
	rows := make([]storage.DeclarationMetrics, 0, len(content.Symbols))
	for _, symbol := range content.Symbols {
		row := storage.DeclarationMetrics{DocumentOrdinal: publication.document.Ordinal, SymbolKey: symbol.Key, Version: storage.MetricsVersion,
			NameLine: symbol.Name[0], NameColumn: symbol.Name[1], NameEndLine: symbol.Name[2], NameEndColumn: symbol.Name[3]}
		if symbol.ID != nil {
			handle, found := publication.handles[*symbol.ID]
			if !found {
				return fmt.Errorf("document %s metric declaration %q has no symbol handle", publication.document.ID, symbol.Key)
			}
			row.SymbolHandle = &handle
		}
		if symbol.Metrics != nil {
			row.ParamCount, row.BodyLines = &symbol.Metrics.ParamCount, symbol.Metrics.BodyLines
		}
		rows = append(rows, row)
	}
	return storage.StoreDeclarationMetrics(ctx, database, rows)
}
