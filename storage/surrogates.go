package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const surrogateLookupBatch = 256

// The natural keys that make a repeated insert of the same root or document a no-op. Naming them keeps
// a conflict on any other unique key, such as an ordinal, an error.
var (
	documentKeyConflict = clause.OnConflict{Columns: []clause.Column{{Name: "root_id"}, {Name: "path_key"}, {Name: "input_hash"}}, DoNothing: true}
	rootKeyConflict     = clause.OnConflict{Columns: []clause.Column{{Name: "root_key"}}, DoNothing: true}
)

// surrogatePair is one (key, value) row of a surrogate lookup.
type surrogatePair[K comparable, V any] struct {
	Key   K `gorm:"column:lookup_key"`
	Value V `gorm:"column:lookup_value"`
}

// loadSurrogates maps the keys that have a row to their value through keyColumn and valueColumn of
// table, in IN batches.
func loadSurrogates[K comparable, V any](ctx context.Context, database *gorm.DB, table, keyColumn, valueColumn, subject string, keys []K) (map[K]V, error) {
	if database == nil {
		return nil, fmt.Errorf("look up %s: database is required", subject)
	}
	result := make(map[K]V, len(keys))
	for start := 0; start < len(keys); start += surrogateLookupBatch {
		var rows []surrogatePair[K, V]
		batch := keys[start:min(start+surrogateLookupBatch, len(keys))]
		if err := database.WithContext(ctx).Table(table).Select(keyColumn+" AS lookup_key, "+valueColumn+" AS lookup_value").
			Where(keyColumn+" IN ?", batch).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("look up %d %ss: %w", len(batch), subject, err)
		}
		for _, row := range rows {
			result[row.Key] = row.Value
		}
	}
	return result, nil
}

// lookupSurrogates is loadSurrogates where a key without a row is an error naming subject and the key.
func lookupSurrogates[K comparable, V any](ctx context.Context, database *gorm.DB, table, keyColumn, valueColumn, subject string, keys []K) (map[K]V, error) {
	result, err := loadSurrogates[K, V](ctx, database, table, keyColumn, valueColumn, subject, keys)
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		if _, found := result[key]; !found {
			return nil, fmt.Errorf("%s %v does not exist", subject, key)
		}
	}
	return result, nil
}

// SymbolHandles maps canonical symbol ids to their handles; an unknown id is an error.
func SymbolHandles(ctx context.Context, database *gorm.DB, ids []string) (map[string]int64, error) {
	return lookupSurrogates[string, int64](ctx, database, Symbol{}.TableName(), "id", "handle", "symbol id", ids)
}

// SymbolIDs maps handles back to canonical symbol ids; an unknown handle is an error.
func SymbolIDs(ctx context.Context, database *gorm.DB, handles []int64) (map[int64]string, error) {
	return lookupSurrogates[int64, string](ctx, database, Symbol{}.TableName(), "handle", "id", "symbol handle", handles)
}

// DocumentOrdinals maps document ids to their ordinals; an unknown id is an error.
func DocumentOrdinals(ctx context.Context, database *gorm.DB, ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	return lookupSurrogates[uuid.UUID, int64](ctx, database, Document{}.TableName(), "id", "ordinal", "document id", ids)
}

// DocumentIDs maps document ordinals back to document ids; an unknown ordinal is an error.
func DocumentIDs(ctx context.Context, database *gorm.DB, ordinals []int64) (map[int64]uuid.UUID, error) {
	return lookupSurrogates[int64, uuid.UUID](ctx, database, Document{}.TableName(), "ordinal", "id", "document ordinal", ordinals)
}

// RootOrdinal is the ordinal of a module root, the root discriminator of symbol_postings.
func RootOrdinal(ctx context.Context, database *gorm.DB, rootID uuid.UUID) (int32, error) {
	ordinals, err := lookupSurrogates[uuid.UUID, int32](ctx, database, ModuleRoot{}.TableName(), "id", "ordinal", "module root", []uuid.UUID{rootID})
	return ordinals[rootID], err
}

// NextModuleOrdinal is one more than the largest module ordinal, or 1 when there is none. Read it inside
// the transaction that inserts the root; a concurrent insert of the same ordinal fails its unique key
// and surfaces as ErrAllocationConflict.
func NextModuleOrdinal(ctx context.Context, database *gorm.DB) (int32, error) {
	next, err := nextOrdinal(ctx, database, ModuleRoot{}.TableName())
	return int32(next), err
}

// NextDocumentOrdinal is one more than the largest document ordinal, or 1 when there is none.
func NextDocumentOrdinal(ctx context.Context, database *gorm.DB) (int64, error) {
	return nextOrdinal(ctx, database, Document{}.TableName())
}

// NextSnapshotOrdinal is one more than the largest snapshot ordinal, or 1 when there is none.
func NextSnapshotOrdinal(ctx context.Context, database *gorm.DB) (int64, error) {
	return nextOrdinal(ctx, database, ModuleSnapshot{}.TableName())
}

func nextOrdinal(ctx context.Context, database *gorm.DB, table string) (int64, error) {
	var largest []int64
	if err := database.WithContext(ctx).Table(table).Order("ordinal DESC").Limit(1).Pluck("ordinal", &largest).Error; err != nil {
		return 0, fmt.Errorf("read the largest %s ordinal: %w", table, err)
	}
	if len(largest) == 0 {
		return 1, nil
	}
	return largest[0] + 1, nil
}

// CreateDocument inserts a document unless its (root, path, input hash) key exists, reporting whether
// this call inserted it. Any other unique conflict, such as a concurrent publisher taking the same
// ordinal, is an error, marked ErrAllocationConflict when it lost a race.
func CreateDocument(ctx context.Context, database *gorm.DB, document *Document) (bool, error) {
	if document.Ordinal < 1 {
		return false, fmt.Errorf("document for %q has no ordinal", document.PathKey)
	}
	created := database.WithContext(ctx).Clauses(documentKeyConflict).Create(document)
	if created.Error != nil {
		return false, allocationConflict(created.Error, fmt.Sprintf("create document for %q with ordinal %d", document.PathKey, document.Ordinal))
	}
	return created.RowsAffected == 1, nil
}

// CreateSnapshot inserts a published snapshot under the next snapshot ordinal. Call it inside the
// publication transaction; a concurrent publisher that took the same ordinal makes the insert fail with
// ErrAllocationConflict.
func CreateSnapshot(ctx context.Context, database *gorm.DB, snapshot *ModuleSnapshot) error {
	if snapshot.Ordinal != 0 {
		return errors.New("CreateSnapshot assigns the snapshot ordinal itself")
	}
	ordinal, err := NextSnapshotOrdinal(ctx, database)
	if err != nil {
		return err
	}
	snapshot.Ordinal = ordinal
	if err := database.WithContext(ctx).Create(snapshot).Error; err != nil {
		return allocationConflict(err, fmt.Sprintf("create snapshot %s with ordinal %d", snapshot.ID, ordinal))
	}
	return nil
}

// CreateModuleRoot inserts a module root under the next ordinal unless its root key exists.
func CreateModuleRoot(ctx context.Context, database *gorm.DB, root *ModuleRoot) error {
	if root.Ordinal != 0 {
		return errors.New("CreateModuleRoot assigns the root ordinal itself")
	}
	ordinal, err := NextModuleOrdinal(ctx, database)
	if err != nil {
		return err
	}
	root.Ordinal = ordinal
	if err := database.WithContext(ctx).Clauses(rootKeyConflict).Create(root).Error; err != nil {
		return allocationConflict(err, fmt.Sprintf("create module root %q with ordinal %d", root.RootKey, ordinal))
	}
	return nil
}
