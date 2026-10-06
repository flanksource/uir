package query

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// snapshotCacheSize bounds how many snapshots' active documents and defined symbols a pipeline keeps.
const snapshotCacheSize = 256

// indexCache is what one pipeline keeps across its queries. Published rows never change: a snapshot's
// active documents and defined symbols, a document's content, a symbol's row and handle, and a symbol
// module's number. Those are kept until the handle layout changes, which discards and rebuilds the
// index, or a prune deletes history (storage.PruneHistory), after which a deleted symbol may come back
// under another handle. What heads and primaries select, and the kind registry, can change with every
// publication, so they live in a generation that a query keeps only while the cache token still matches.
type indexCache struct {
	mu         sync.Mutex
	generation *cacheGeneration
	symbols    *symbolFacts
	modules    map[int32]string
	snapshots  *lru[uuid.UUID, *snapshotFacts]
	documents  *lru[uuid.UUID, decodedDocument]
	capacity   int
}

func newIndexCache(documents int) *indexCache {
	cache := &indexCache{capacity: documents}
	cache.reset()
	return cache
}

// reset discards everything kept; the caller holds mu or owns the cache alone.
func (cache *indexCache) reset() {
	cache.symbols = newSymbolFacts()
	cache.modules = map[int32]string{}
	cache.snapshots = newLRU[uuid.UUID, *snapshotFacts](snapshotCacheSize)
	cache.documents = newLRU[uuid.UUID, decodedDocument](cache.capacity)
}

// cacheToken is what a query reads to decide whether the kept generation still holds: every location
// head with its compare-and-set version, every primary location, the handle layout, the prune epoch,
// and the sizes of the kind registry and of the root and location registrations, all of which only grow.
type cacheToken struct {
	heads     string
	layout    string
	prunes    int64
	kinds     int64
	roots     int64
	locations int64
}

// tokenCountsSQL reads the scalar parts of the cache token in one statement. No history_prunes row
// means nothing was ever pruned.
const tokenCountsSQL = `SELECT
  (SELECT layout FROM symbol_layout WHERE id = 1) AS layout,
  (SELECT COALESCE(MAX(epoch), 0) FROM history_prunes) AS prunes,
  (SELECT COUNT(*) FROM symbol_kinds) AS kinds,
  (SELECT COUNT(*) FROM modules) AS roots,
  (SELECT COUNT(*) FROM locations) AS locations`

func readCacheToken(ctx context.Context, database *gorm.DB) (cacheToken, error) {
	var heads []storage.ModuleLocationHead
	if err := database.WithContext(ctx).Order("location_id").Find(&heads).Error; err != nil {
		return cacheToken{}, fmt.Errorf("read the location heads: %w", err)
	}
	var primaries []storage.ModulePrimary
	if err := database.WithContext(ctx).Order("root_id").Find(&primaries).Error; err != nil {
		return cacheToken{}, fmt.Errorf("read the primary locations: %w", err)
	}
	var counts struct {
		Layout    *string `gorm:"column:layout"`
		Prunes    int64   `gorm:"column:prunes"`
		Kinds     int64   `gorm:"column:kinds"`
		Roots     int64   `gorm:"column:roots"`
		Locations int64   `gorm:"column:locations"`
	}
	if err := database.WithContext(ctx).Raw(tokenCountsSQL).Scan(&counts).Error; err != nil {
		return cacheToken{}, fmt.Errorf("read the index registrations: %w", err)
	}
	if counts.Layout == nil {
		return cacheToken{}, fmt.Errorf("database records no symbol handle layout; open it read-write once or run uir reindex")
	}
	var state strings.Builder
	for _, head := range heads {
		fmt.Fprintf(&state, "%s %s %d\n", head.LocationID, head.SnapshotID, head.Version)
	}
	for _, primary := range primaries {
		fmt.Fprintf(&state, "%s > %s\n", primary.RootID, primary.LocationID)
	}
	return cacheToken{heads: state.String(), layout: *counts.Layout, prunes: counts.Prunes, kinds: counts.Kinds, roots: counts.Roots, locations: counts.Locations}, nil
}

// cacheGeneration is what holds while the cache token does: the kind registry and the snapshots each
// scope selection resolved to.
type cacheGeneration struct {
	token      cacheToken
	kinds      symbolhandle.Kinds
	mu         sync.Mutex
	selections map[selectionKey]moduleScopeSelection
}

// selectionKey is a scope selection that depends only on the registrations and heads.
type selectionKey struct {
	rootKey, snapshotID string
	allHeads            bool
}

// current is the generation of the database as it is now: the kept one while its token matches, else a
// new one, which discards everything when the handle layout changed or a prune deleted history.
func (cache *indexCache) current(ctx context.Context, database *gorm.DB) (*cacheGeneration, error) {
	token, err := readCacheToken(ctx, database)
	if err != nil {
		return nil, err
	}
	cache.mu.Lock()
	kept := cache.generation
	cache.mu.Unlock()
	if kept != nil && kept.token == token {
		return kept, nil
	}
	kinds, err := storage.LoadSymbolKinds(ctx, database)
	if err != nil {
		return nil, err
	}
	generation := &cacheGeneration{token: token, kinds: kinds, selections: map[selectionKey]moduleScopeSelection{}}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if kept := cache.generation; kept != nil && (kept.token.layout != token.layout || kept.token.prunes != token.prunes) {
		cache.reset()
	}
	cache.generation = generation
	return generation, nil
}

func (generation *cacheGeneration) selection(key selectionKey) (moduleScopeSelection, bool) {
	generation.mu.Lock()
	defer generation.mu.Unlock()
	selection, found := generation.selections[key]
	return selection.clone(), found
}

func (generation *cacheGeneration) keepSelection(key selectionKey, selection moduleScopeSelection) {
	generation.mu.Lock()
	defer generation.mu.Unlock()
	generation.selections[key] = selection.clone()
}

func (selection moduleScopeSelection) clone() moduleScopeSelection {
	return moduleScopeSelection{scopes: append([]moduleScope(nil), selection.scopes...), warnings: append([]MissingHeadWarning{}, selection.warnings...)}
}

// symbolFacts are the symbol rows, query names, and handles the pipeline's queries have read.
type symbolFacts struct {
	mu       sync.RWMutex
	rows     map[string]storage.Symbol
	names    map[string]string
	byID     map[string]int64
	byHandle map[int64]string
}

func newSymbolFacts() *symbolFacts {
	return &symbolFacts{rows: map[string]storage.Symbol{}, names: map[string]string{}, byID: map[string]int64{}, byHandle: map[int64]string{}}
}

// symbolFacts is the symbol layer the queries of the current layout share.
func (cache *indexCache) symbolFacts() *symbolFacts {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.symbols
}

func (facts *symbolFacts) row(id string) (storage.Symbol, bool) {
	facts.mu.RLock()
	defer facts.mu.RUnlock()
	row, found := facts.rows[id]
	return row, found
}

// keepRows records loaded symbol rows and their handles.
func (facts *symbolFacts) keepRows(rows []storage.Symbol) error {
	facts.mu.Lock()
	defer facts.mu.Unlock()
	for _, row := range rows {
		if err := facts.addHandle(row.ID, row.Handle); err != nil {
			return err
		}
		facts.rows[row.ID] = row
	}
	return nil
}

func (facts *symbolFacts) name(id string) (string, bool) {
	facts.mu.RLock()
	defer facts.mu.RUnlock()
	name, found := facts.names[id]
	return name, found
}

func (facts *symbolFacts) keepName(id, name string) {
	facts.mu.Lock()
	defer facts.mu.Unlock()
	facts.names[id] = name
}

func (facts *symbolFacts) handle(id string) (int64, bool) {
	facts.mu.RLock()
	defer facts.mu.RUnlock()
	handle, found := facts.byID[id]
	return handle, found
}

func (facts *symbolFacts) id(handle int64) (string, bool) {
	facts.mu.RLock()
	defer facts.mu.RUnlock()
	id, found := facts.byHandle[handle]
	return id, found
}

func (facts *symbolFacts) keepHandles(handles map[string]int64) error {
	facts.mu.Lock()
	defer facts.mu.Unlock()
	for id, handle := range handles {
		if err := facts.addHandle(id, handle); err != nil {
			return err
		}
	}
	return nil
}

// addHandle records one id's handle; the caller holds mu for writing. A handle is never reassigned
// within a layout, so a second, different pairing is an error.
func (facts *symbolFacts) addHandle(id string, handle int64) error {
	if known, found := facts.byID[id]; found && known != handle {
		return fmt.Errorf("symbol %s has handle %d, earlier %d", id, handle, known)
	}
	if known, found := facts.byHandle[handle]; found && known != id {
		return fmt.Errorf("handle %d names symbol %s, earlier %s", handle, id, known)
	}
	facts.byID[id], facts.byHandle[handle] = handle, id
	return nil
}

// snapshotFacts are one snapshot's active documents, without content, and the handles its queries
// asked whether it defines.
type snapshotFacts struct {
	documents map[uuid.UUID]storage.ActiveDocument
	mu        sync.Mutex
	defined   map[int64]bool
}

// snapshot is the kept facts of one snapshot, read on first use.
func (cache *indexCache) snapshot(ctx context.Context, database *gorm.DB, id uuid.UUID) (*snapshotFacts, error) {
	if facts, found := cache.snapshots.get(id); found {
		return facts, nil
	}
	active, err := storage.ActiveDocuments(ctx, database, id, storage.ActiveDocumentOptions{})
	if err != nil {
		return nil, err
	}
	facts := &snapshotFacts{documents: make(map[uuid.UUID]storage.ActiveDocument, len(active)), defined: map[int64]bool{}}
	for _, document := range active {
		facts.documents[document.Document.ID] = document
	}
	cache.snapshots.put(id, facts)
	return facts, nil
}

// moduleKey is the module key a symbol module number names; the registry is read again on a miss,
// since numbers are only ever added, and a number it still lacks is an error.
func (cache *indexCache) moduleKey(ctx context.Context, database *gorm.DB, number int32) (string, error) {
	cache.mu.Lock()
	key, found := cache.modules[number]
	cache.mu.Unlock()
	if found {
		return key, nil
	}
	var modules []storage.SymbolModule
	if err := database.WithContext(ctx).Find(&modules).Error; err != nil {
		return "", fmt.Errorf("load the symbol modules: %w", err)
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for _, module := range modules {
		cache.modules[module.Number] = module.ModuleKey
	}
	if key, found = cache.modules[number]; !found {
		return "", fmt.Errorf("symbol module %d is not registered", number)
	}
	return key, nil
}
