package storage

import (
	"context"
	"fmt"
	"reflect"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// StoreSourceMetrics inserts immutable counts and verifies any concurrently stored or reused counts.
func StoreSourceMetrics(ctx context.Context, database *gorm.DB, rows []SourceMetrics) error {
	if len(rows) == 0 {
		return nil
	}
	for start := 0; start < len(rows); start += documentLookupBatch {
		batch := rows[start:min(start+documentLookupBatch, len(rows))]
		ids := make([]uuid.UUID, 0, len(batch))
		for _, row := range batch {
			if row.SourceRevisionID == uuid.Nil || row.Version != MetricsVersion || row.Lines < 0 {
				return fmt.Errorf("invalid source metrics for revision %s", row.SourceRevisionID)
			}
			ids = append(ids, row.SourceRevisionID)
		}
		if err := database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&batch).Error; err != nil {
			return fmt.Errorf("store source metrics: %w", err)
		}
		var stored []SourceMetrics
		if err := database.WithContext(ctx).Where("source_revision_id IN ? AND version = ?", ids, MetricsVersion).Find(&stored).Error; err != nil {
			return fmt.Errorf("verify source metrics: %w", err)
		}
		byID := make(map[uuid.UUID]SourceMetrics, len(stored))
		for _, row := range stored {
			byID[row.SourceRevisionID] = row
		}
		for _, expected := range batch {
			if actual, found := byID[expected.SourceRevisionID]; !found || actual != expected {
				return fmt.Errorf("source metrics of revision %s changed without a metric version change", expected.SourceRevisionID)
			}
		}
	}
	return nil
}

// StoreDeclarationMetrics atomically accompanies document publication in the caller's transaction.
func StoreDeclarationMetrics(ctx context.Context, database *gorm.DB, rows []DeclarationMetrics) error {
	for start := 0; start < len(rows); start += documentLookupBatch {
		batch := rows[start:min(start+documentLookupBatch, len(rows))]
		ordinals := make([]int64, 0, len(batch))
		for _, row := range batch {
			if row.DocumentOrdinal < 1 || row.SymbolKey == "" || row.Version != MetricsVersion {
				return fmt.Errorf("invalid declaration metrics for document %d, symbol %q", row.DocumentOrdinal, row.SymbolKey)
			}
			ordinals = append(ordinals, row.DocumentOrdinal)
		}
		if err := database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&batch).Error; err != nil {
			return fmt.Errorf("store declaration metrics: %w", err)
		}
		var stored []DeclarationMetrics
		if err := database.WithContext(ctx).Where("document_ordinal IN ?", ordinals).Find(&stored).Error; err != nil {
			return fmt.Errorf("verify declaration metrics: %w", err)
		}
		byKey := make(map[struct {
			ordinal int64
			key     string
		}]DeclarationMetrics, len(stored))
		for _, row := range stored {
			byKey[struct {
				ordinal int64
				key     string
			}{row.DocumentOrdinal, row.SymbolKey}] = row
		}
		for _, expected := range batch {
			actual, found := byKey[struct {
				ordinal int64
				key     string
			}{expected.DocumentOrdinal, expected.SymbolKey}]
			if !found || !reflect.DeepEqual(actual, expected) {
				return fmt.Errorf("declaration metrics for document %d, symbol %q differ from the immutable document", expected.DocumentOrdinal, expected.SymbolKey)
			}
		}
	}
	return nil
}
