package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const publicationBatch = 256

func createPackageCoverage(ctx context.Context, database *gorm.DB, snapshot storage.ModuleSnapshot, packages []extractedPackage) error {
	rows := make([]storage.PackageCoverage, 0, len(packages))
	for _, extracted := range packages {
		diagnostics, err := json.Marshal(extracted.diagnostics)
		if err != nil {
			return fmt.Errorf("encode diagnostics of package %q: %w", extracted.path, err)
		}
		rows = append(rows, storage.PackageCoverage{
			SnapshotID: snapshot.ID, RootID: snapshot.RootID, PackagePath: extracted.path, InputHash: extracted.inputHash,
			ExportShapeHash: extracted.exportShapeHash, Coverage: extracted.coverage, FileCount: extracted.fileCount,
			Diagnostics: storage.JSON(diagnostics),
		})
	}
	if len(rows) == 0 {
		return nil
	}
	if err := database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 128).Error; err != nil {
		return fmt.Errorf("create package coverage for snapshot %s: %w", snapshot.ID, err)
	}
	var stored []storage.PackageCoverage
	if err := database.WithContext(ctx).Where("snapshot_id = ?", snapshot.ID).Find(&stored).Error; err != nil {
		return fmt.Errorf("load package coverage for snapshot %s: %w", snapshot.ID, err)
	}
	if len(stored) != len(rows) {
		return fmt.Errorf("snapshot %s stored %d package coverage rows, expected %d", snapshot.ID, len(stored), len(rows))
	}
	expected := make(map[string]string, len(rows))
	for _, row := range rows {
		expected[row.PackagePath] = row.InputHash
	}
	for _, row := range stored {
		if row.InputHash != expected[row.PackagePath] {
			return fmt.Errorf("package %q in snapshot %s has input hash %s, expected %s", row.PackagePath, snapshot.ID, row.InputHash, expected[row.PackagePath])
		}
	}
	return nil
}

// createSiblingShapes records the workspace sibling packages the snapshot's load read. An extraction
// whose context equals an existing snapshot's describes exactly what that snapshot holds against the
// siblings' current sources, so it records, or refreshes, the snapshot's rows from what it read.
func createSiblingShapes(ctx context.Context, database *gorm.DB, snapshot storage.ModuleSnapshot, siblings map[string]siblingRecord) error {
	if len(siblings) == 0 {
		return nil
	}
	rows := make([]storage.SnapshotSiblingShape, 0, len(siblings))
	for _, path := range sortedKeys(siblings) {
		rows = append(rows, storage.SnapshotSiblingShape{
			SnapshotID: snapshot.ID, ImportPath: path, ExportShapeHash: siblings[path].shape, SourceDigest: siblings[path].digest,
		})
	}
	upsert := clause.OnConflict{
		Columns:   []clause.Column{{Name: "snapshot_id"}, {Name: "import_path"}},
		DoUpdates: clause.AssignmentColumns([]string{"export_shape_hash", "source_digest"}),
	}
	if err := database.WithContext(ctx).Clauses(upsert).CreateInBatches(rows, publicationBatch).Error; err != nil {
		return fmt.Errorf("record sibling export shapes of snapshot %s: %w", snapshot.ID, err)
	}
	return nil
}

// createPostings writes a new document's postings under its ordinal, its root's ordinal, and each
// symbol's handle.
func createPostings(ctx context.Context, database *gorm.DB, document storage.Document, rootOrdinal int32, postings []extractedPosting, handles map[string]int64) error {
	if len(postings) == 0 {
		return nil
	}
	rows := make([]storage.SymbolPosting, len(postings))
	for i, posting := range postings {
		handle, found := handles[posting.symbolID]
		if !found {
			return fmt.Errorf("posting of %q names symbol %s, which has no handle", document.PathKey, posting.symbolID)
		}
		rows[i] = storage.SymbolPosting{
			DocumentOrdinal: document.Ordinal, RootOrdinal: rootOrdinal, SymbolHandle: handle, Role: posting.role, OccurrenceCount: posting.occurrences,
		}
	}
	if err := database.WithContext(ctx).CreateInBatches(rows, publicationBatch).Error; err != nil {
		return fmt.Errorf("create %d postings for %q: %w", len(rows), document.PathKey, err)
	}
	return nil
}

// neededSymbols is every symbol id the pending documents name, and every id an implements fact names
// that no stored row has yet, with its owners; stored are the handles of the implements ids that
// already have rows, which publication does not rewrite.
func neededSymbols(ctx context.Context, database *gorm.DB, extraction moduleExtraction, pending []pendingDocument) (map[string]bool, map[string]int64, error) {
	needed := map[string]bool{}
	for _, next := range pending {
		for _, id := range next.extracted.symbolIDs {
			needed[id] = true
		}
	}
	facts := map[string]bool{}
	for _, fact := range extraction.implements {
		for _, id := range []string{fact.typeID, fact.interfaceID} {
			if !needed[id] {
				facts[id] = true
			}
		}
	}
	stored, err := storage.StoredSymbolHandles(ctx, database, sortedKeys(facts))
	if err != nil {
		return nil, nil, err
	}
	for id := range facts {
		if _, found := stored[id]; found {
			continue
		}
		if _, known := extraction.symbols[id]; !known {
			return nil, nil, fmt.Errorf("implements fact names symbol %s, which extraction did not resolve", id)
		}
		for owner := &id; owner != nil && !needed[*owner]; owner = extraction.symbols[*owner].OwnerID {
			needed[*owner] = true
		}
	}
	return needed, stored, nil
}

// ensureSymbols gives the needed symbol rows their handles and upserts them, owners before the
// symbols they own, refreshing the derived search_name and visibility of existing rows, and re-reads
// them: an existing row must carry exactly the expected canonical key, facts, and handle. It returns
// each needed symbol's handle.
func ensureSymbols(ctx context.Context, database *gorm.DB, symbols map[string]storage.Symbol, wanted map[string]bool) (map[string]int64, error) {
	needed := map[string]storage.Symbol{}
	for id := range wanted {
		needed[id] = symbols[id]
	}
	if len(needed) == 0 {
		return map[string]int64{}, nil
	}
	ids := sortedKeys(needed)
	rows := make([]storage.Symbol, 0, len(needed))
	for _, id := range ids {
		rows = append(rows, needed[id])
	}
	sort.SliceStable(rows, func(i, j int) bool { return ownerDepth(symbols, rows[i]) < ownerDepth(symbols, rows[j]) })
	if err := storage.AssignSymbolHandles(ctx, database, rows); err != nil {
		return nil, err
	}
	if err := storage.UpsertSymbols(ctx, database, rows, publicationBatch); err != nil {
		return nil, err
	}
	for _, row := range rows {
		needed[row.ID] = row
	}
	handles := make(map[string]int64, len(ids))
	for start := 0; start < len(ids); start += publicationBatch {
		batch := ids[start:min(start+publicationBatch, len(ids))]
		var stored []storage.Symbol
		if err := database.WithContext(ctx).Where("id IN ?", batch).Find(&stored).Error; err != nil {
			return nil, fmt.Errorf("load published symbols: %w", err)
		}
		if len(stored) != len(batch) {
			return nil, fmt.Errorf("published %d symbols, found %d", len(batch), len(stored))
		}
		for _, row := range stored {
			if err := sameSymbol(row, needed[row.ID]); err != nil {
				return nil, err
			}
			handles[row.ID] = row.Handle
		}
	}
	return handles, nil
}

// ownerDepth is how many owners a symbol has, so owners can be written before the symbols they own.
func ownerDepth(symbols map[string]storage.Symbol, row storage.Symbol) int {
	levels := 0
	for owner := row.OwnerID; owner != nil; owner = symbols[*owner].OwnerID {
		levels++
	}
	return levels
}

func sameSymbol(stored, expected storage.Symbol) error {
	if stored.Handle != expected.Handle {
		return fmt.Errorf("symbol %s (%s) was stored by a concurrent publisher with handle %d, not %d: %w",
			stored.ID, expected.CanonicalKey, stored.Handle, expected.Handle, storage.ErrAllocationConflict)
	}
	if stored.CanonicalKey != expected.CanonicalKey {
		return fmt.Errorf("symbol %s has canonical key %q, expected %q", stored.ID, stored.CanonicalKey, expected.CanonicalKey)
	}
	same, err := sameJSON(stored.ParameterTypes, expected.ParameterTypes)
	if err != nil {
		return fmt.Errorf("compare parameter types of symbol %s: %w", stored.ID, err)
	}
	stored.ParameterTypes, expected.ParameterTypes = nil, nil
	if !same || !reflect.DeepEqual(stored, expected) {
		return fmt.Errorf("symbol %s (%s) is stored with different facts than extraction derived", stored.ID, expected.CanonicalKey)
	}
	return nil
}

func sameJSON(left, right storage.JSON) (bool, error) {
	var leftValue, rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return false, err
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return false, err
	}
	return reflect.DeepEqual(leftValue, rightValue), nil
}
