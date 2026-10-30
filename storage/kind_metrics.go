package storage

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// measuresFormat is the first document format whose symbols may carry producer metric values.
const measuresFormat = 10

// MetricSpec describes one non-negative integer metric a custom kind declares for its symbols.
type MetricSpec struct {
	Description string `json:"description"`
}

// SymbolKindMetric is one metric a registered kind declares.
type SymbolKindMetric struct {
	Kind        string `gorm:"column:kind;primaryKey"`
	Name        string `gorm:"column:name;primaryKey"`
	Description string `gorm:"column:description"`
}

func (SymbolKindMetric) TableName() string { return "symbol_kind_metrics" }

// DeclarationMeasure is one producer metric value of a declaration in an immutable document.
type DeclarationMeasure struct {
	DocumentOrdinal int64  `gorm:"column:document_ordinal;primaryKey;autoIncrement:false"`
	SymbolKey       string `gorm:"column:symbol_key;primaryKey"`
	Name            string `gorm:"column:name;primaryKey"`
	SymbolHandle    int64  `gorm:"column:symbol_handle"`
	Value           int64  `gorm:"column:value"`
}

func (DeclarationMeasure) TableName() string { return "declaration_measures" }

var metricNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// callableMetricNames are the fixed callable metrics a query requests as symbol.metrics.<name>.
var callableMetricNames = []string{"param_count", "body_lines"}

// ValidateMetricName accepts a lower snake_case identifier that is not a callable metric name.
func ValidateMetricName(name string) error {
	if !metricNamePattern.MatchString(name) {
		return fmt.Errorf("metric %q must be a lower snake_case identifier", name)
	}
	if slices.Contains(callableMetricNames, name) {
		return fmt.Errorf("metric %q is a callable metric and cannot be declared by a kind", name)
	}
	return nil
}

// EvolveKindMetrics refuses a declaration that removes or redescribes a metric the kind registered;
// a kind may only gain metrics.
func EvolveKindMetrics(kind string, registered, declared map[string]MetricSpec) error {
	for _, name := range slices.Sorted(maps.Keys(registered)) {
		spec, found := declared[name]
		if !found {
			return fmt.Errorf("kind %s metric %q is no longer declared; a registered metric cannot be removed", kind, name)
		}
		if spec != registered[name] {
			return fmt.Errorf("kind %s metric %q changes its description from %q to %q; a registered metric is immutable", kind, name, registered[name].Description, spec.Description)
		}
	}
	return nil
}

// validateMeasures checks what a document alone decides about a symbol's measures: its format, a
// canonical id to store them under, and non-negative values. Which names it must carry is the domain
// catalog's (DomainCatalog.Validate).
func validateMeasures(version int, symbol DocumentSymbol) error {
	if len(symbol.Measures) == 0 {
		return nil
	}
	if version < measuresFormat {
		return fmt.Errorf("symbol %s of kind %s: measures require document format %d or newer", symbol.Key, symbol.Kind, measuresFormat)
	}
	if symbol.ID == nil {
		return fmt.Errorf("symbol %s of kind %s: measures require a declaration with a canonical id", symbol.Key, symbol.Kind)
	}
	for _, name := range slices.Sorted(maps.Keys(symbol.Measures)) {
		if value := symbol.Measures[name]; value < 0 {
			return fmt.Errorf("symbol %s of kind %s: metric %q has negative value %d", symbol.Key, symbol.Kind, name, value)
		}
	}
	return nil
}

// validateDeclaredMeasures requires a symbol to carry exactly the metrics its kind declares.
func (catalog DomainCatalog) validateDeclaredMeasures(symbol DocumentSymbol) error {
	declared := catalog.Metrics[symbol.Kind]
	for _, name := range slices.Sorted(maps.Keys(symbol.Measures)) {
		if _, found := declared[name]; !found {
			return fmt.Errorf("symbol %s of kind %s records metric %q, which its kind does not declare", symbol.Key, symbol.Kind, name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(declared)) {
		if _, found := symbol.Measures[name]; !found {
			return fmt.Errorf("symbol %s of kind %s has no value for its kind's declared metric %q", symbol.Key, symbol.Kind, name)
		}
	}
	return nil
}

func loadKindMetrics(ctx context.Context, database *gorm.DB) (map[string]map[string]MetricSpec, error) {
	var rows []SymbolKindMetric
	if err := database.WithContext(ctx).Order("kind, name").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load symbol kind metrics: %w", err)
	}
	metrics := map[string]map[string]MetricSpec{}
	for _, row := range rows {
		if metrics[row.Kind] == nil {
			metrics[row.Kind] = map[string]MetricSpec{}
		}
		metrics[row.Kind][row.Name] = MetricSpec{Description: row.Description}
	}
	return metrics, nil
}

// registerKindMetrics adds a kind's new metrics and refuses, against what the transaction reads back,
// any registered metric the declaration removes or redescribes.
func registerKindMetrics(ctx context.Context, tx *gorm.DB, kind string, declared map[string]MetricSpec) error {
	if _, err := KindCode(ctx, tx, kind); err != nil {
		return err
	}
	rows := make([]SymbolKindMetric, 0, len(declared))
	for _, name := range slices.Sorted(maps.Keys(declared)) {
		if err := ValidateMetricName(name); err != nil {
			return fmt.Errorf("kind %s: %w", kind, err)
		}
		rows = append(rows, SymbolKindMetric{Kind: kind, Name: name, Description: declared[name].Description})
	}
	if len(rows) > 0 {
		if err := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
			return fmt.Errorf("register metrics of kind %s: %w", kind, err)
		}
	}
	var stored []SymbolKindMetric
	if err := tx.WithContext(ctx).Where("kind = ?", kind).Find(&stored).Error; err != nil {
		return fmt.Errorf("read metrics of kind %s: %w", kind, err)
	}
	registered := make(map[string]MetricSpec, len(stored))
	for _, row := range stored {
		registered[row.Name] = MetricSpec{Description: row.Description}
	}
	return EvolveKindMetrics(kind, registered, declared)
}

// StoreDeclarationMeasures writes a document's measures in the caller's publication transaction and
// verifies any stored by a concurrent or earlier publication of the same immutable document.
func StoreDeclarationMeasures(ctx context.Context, database *gorm.DB, rows []DeclarationMeasure) error {
	for start := 0; start < len(rows); start += documentLookupBatch {
		batch := rows[start:min(start+documentLookupBatch, len(rows))]
		ordinals := make([]int64, 0, len(batch))
		for _, row := range batch {
			if row.DocumentOrdinal < 1 || row.SymbolKey == "" || row.Value < 0 {
				return fmt.Errorf("invalid measure %q for document %d, symbol %q", row.Name, row.DocumentOrdinal, row.SymbolKey)
			}
			ordinals = append(ordinals, row.DocumentOrdinal)
		}
		if err := database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&batch).Error; err != nil {
			return fmt.Errorf("store declaration measures: %w", err)
		}
		var stored []DeclarationMeasure
		if err := database.WithContext(ctx).Where("document_ordinal IN ?", ordinals).Find(&stored).Error; err != nil {
			return fmt.Errorf("verify declaration measures: %w", err)
		}
		type measureKey struct {
			ordinal   int64
			key, name string
		}
		byKey := make(map[measureKey]DeclarationMeasure, len(stored))
		for _, row := range stored {
			byKey[measureKey{row.DocumentOrdinal, row.SymbolKey, row.Name}] = row
		}
		for _, expected := range batch {
			if actual, found := byKey[measureKey{expected.DocumentOrdinal, expected.SymbolKey, expected.Name}]; !found || !reflect.DeepEqual(actual, expected) {
				return fmt.Errorf("measure %q for document %d, symbol %q differs from the immutable document", expected.Name, expected.DocumentOrdinal, expected.SymbolKey)
			}
		}
	}
	return nil
}
