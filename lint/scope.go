package lint

import (
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// lintScope is the snapshots one analysis mode's rules read, with what closures need of each, loaded once
// and shared by every rule of the mode.
type lintScope struct {
	database  *gorm.DB
	query     query.ModuleScopeOptions
	snapshots []*scopeSnapshot
	locations map[string]string
}

// scopeSnapshot is one snapshot of a scope, its root key and checkout, and its defined symbols, document
// membership (path → ordinal, and each ordinal's document id) and symbol definitions, each loaded when a
// closure first needs it. Closures and stored rows name documents by id, since pruning frees ordinals
// that a later document can be given.
type scopeSnapshot struct {
	snapshot    storage.ModuleSnapshot
	rootKey     string
	location    string
	symbols     []storage.EffectiveSymbol
	documents   map[string]int64
	documentIDs map[int64]uuid.UUID
	definitions map[int64][]int64
}

func loadScope(ctx context.Context, database *gorm.DB, options query.ModuleScopeOptions) (*lintScope, error) {
	ids := options.SnapshotIDs
	if options.SnapshotID != "" {
		ids = []string{options.SnapshotID}
	}
	var snapshots []storage.ModuleSnapshot
	if err := database.WithContext(ctx).Where("id IN ?", ids).Order("id").Find(&snapshots).Error; err != nil {
		return nil, fmt.Errorf("load the lint scope: %w", err)
	}
	scope := &lintScope{database: database, query: options, locations: map[string]string{}}
	for _, snapshot := range snapshots {
		var root storage.ModuleRoot
		if err := database.WithContext(ctx).Where("id = ?", snapshot.RootID).Take(&root).Error; err != nil {
			return nil, fmt.Errorf("load the root of snapshot %s: %w", snapshot.ID, err)
		}
		var location storage.ModuleLocation
		if err := database.WithContext(ctx).Where("id = ?", snapshot.LocationID).Take(&location).Error; err != nil {
			return nil, fmt.Errorf("load the location of snapshot %s: %w", snapshot.ID, err)
		}
		scope.snapshots = append(scope.snapshots, &scopeSnapshot{snapshot: snapshot, rootKey: root.RootKey, location: location.CanonicalPath})
		scope.locations[root.RootKey] = location.CanonicalPath
	}
	return scope, nil
}

func (snapshot *scopeSnapshot) effectiveSymbols(ctx context.Context, database *gorm.DB) ([]storage.EffectiveSymbol, error) {
	if snapshot.symbols == nil {
		symbols, err := storage.EffectiveSymbols(ctx, database, snapshot.snapshot.ID)
		if err != nil {
			return nil, err
		}
		snapshot.symbols = append([]storage.EffectiveSymbol{}, symbols...)
	}
	return snapshot.symbols, nil
}

func (snapshot *scopeSnapshot) activeDocuments(ctx context.Context, database *gorm.DB) (map[string]int64, error) {
	if snapshot.documents == nil {
		documents, err := storage.EffectiveDocumentOrdinals(ctx, database, snapshot.snapshot.ID)
		if err != nil {
			return nil, err
		}
		ordinals := make([]int64, 0, len(documents))
		for _, ordinal := range documents {
			ordinals = append(ordinals, ordinal)
		}
		ids, err := storage.DocumentIDs(ctx, database, ordinals)
		if err != nil {
			return nil, err
		}
		snapshot.documents, snapshot.documentIDs = documents, ids
	}
	return snapshot.documents, nil
}

// definitionDocuments maps every symbol the snapshot defines to the sorted ordinals of its active
// documents that hold its definition.
func (snapshot *scopeSnapshot) definitionDocuments(ctx context.Context, database *gorm.DB) (map[int64][]int64, error) {
	if snapshot.definitions != nil {
		return snapshot.definitions, nil
	}
	documents, err := snapshot.activeDocuments(ctx, database)
	if err != nil {
		return nil, err
	}
	ordinals := make([]int64, 0, len(documents))
	for _, ordinal := range documents {
		ordinals = append(ordinals, ordinal)
	}
	slices.Sort(ordinals)
	postings, err := storage.DefinitionPostings(ctx, database, ordinals)
	if err != nil {
		return nil, err
	}
	snapshot.definitions = map[int64][]int64{}
	for _, posting := range postings {
		snapshot.definitions[posting.SymbolHandle] = append(snapshot.definitions[posting.SymbolHandle], posting.DocumentOrdinal)
	}
	return snapshot.definitions, nil
}

// rootIDs are the scope's roots in id order.
func (scope *lintScope) rootIDs() []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(scope.snapshots))
	for _, snapshot := range scope.snapshots {
		ids = append(ids, snapshot.snapshot.RootID)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return ids
}
