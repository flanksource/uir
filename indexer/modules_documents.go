package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
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

// pendingDocument is a document publication will insert or, under force, verify.
type pendingDocument struct {
	document  storage.Document
	extracted extractedDocument
	existing  *storage.Document
}

// publishDocuments ensures the document keyed (root, path, package input hash) exists for every
// file. An existing document is reused unless force re-verifies it, in which case the fresh
// extraction must equal it: a different result needs a new indexer version. New documents get their
// symbol rows and handles first, then the next document ordinals, then their postings; postings of a
// document another writer inserted are never written twice. It returns the handle of every symbol the
// new documents name.
func publishDocuments(ctx context.Context, database *gorm.DB, publication snapshotPublication, current map[string]storage.SourceRevision, result *ModuleResult) (map[string]int64, error) {
	extraction := publication.extraction
	var pending []pendingDocument
	for _, file := range extraction.root.Files {
		extracted, found := extraction.documents[file.PathKey]
		if !found {
			return nil, fmt.Errorf("no document was extracted for %q", file.PathKey)
		}
		revision := current[file.PathKey]
		document := storage.Document{
			ID: uuid.New(), RootID: revision.RootID, PathKey: file.PathKey, SourceRevisionID: revision.ID,
			PackagePath: file.PackagePath, InputHash: extracted.inputHash, IndexerVersion: IndexerVersion,
			Coverage: extracted.coverage, SymbolCount: extracted.symbolCount, OccurrenceCount: extracted.occurrenceCount,
			Content: extracted.content,
		}
		if _, err := storage.DecodeDocument(document, revision); err != nil {
			return nil, fmt.Errorf("validate extracted document: %w", err)
		}
		existing, found, err := loadDocument(ctx, database, revision.RootID, file.PathKey, extracted.inputHash)
		if err != nil {
			return nil, err
		}
		if found && existing.SourceRevisionID != revision.ID {
			return nil, fmt.Errorf("document %s for %q describes revision %s, expected %s", existing.ID, file.PathKey, existing.SourceRevisionID, revision.ID)
		}
		switch {
		case !found:
			pending = append(pending, pendingDocument{document: document, extracted: extracted})
		case publication.force:
			pending = append(pending, pendingDocument{document: document, extracted: extracted, existing: &existing})
		default:
			result.ReusedFiles++
		}
	}
	handles, err := ensureSymbols(ctx, database, extraction.symbols, pending)
	if err != nil {
		return nil, err
	}
	if err := assignDocumentOrdinals(ctx, database, pending); err != nil {
		return nil, err
	}
	for _, next := range pending {
		if err := publishDocument(ctx, database, next, publication.root.Ordinal, handles); err != nil {
			return nil, err
		}
		result.ParsedFiles++
	}
	return handles, nil
}

// assignDocumentOrdinals numbers the documents this publication inserts from one past the largest
// stored ordinal, in file order.
func assignDocumentOrdinals(ctx context.Context, database *gorm.DB, pending []pendingDocument) error {
	next, err := storage.NextDocumentOrdinal(ctx, database)
	if err != nil {
		return err
	}
	for i := range pending {
		if pending[i].existing == nil {
			pending[i].document.Ordinal = next
			next++
		}
	}
	return nil
}

func publishDocument(ctx context.Context, database *gorm.DB, next pendingDocument, rootOrdinal int32, handles map[string]int64) error {
	document := next.document
	if next.existing == nil {
		inserted, err := storage.CreateDocument(ctx, database, &document)
		if err != nil {
			return err
		}
		if inserted {
			return createPostings(ctx, database, document, rootOrdinal, next.extracted.postings, handles)
		}
		stored, found, err := loadDocument(ctx, database, document.RootID, document.PathKey, document.InputHash)
		if err != nil || !found {
			return errors.Join(fmt.Errorf("load the document another publisher stored for %q", document.PathKey), err)
		}
		next.existing = &stored
	}
	same, err := sameJSON(next.existing.Content, document.Content)
	if err != nil {
		return fmt.Errorf("compare document for %q: %w", document.PathKey, err)
	}
	if !same || next.existing.Coverage != document.Coverage {
		return fmt.Errorf("document for %q under input hash %s changed without an indexer version change", document.PathKey, document.InputHash)
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

// ensureSymbols gives the symbol rows the pending documents need their handles and upserts them,
// owners before the symbols they own, refreshing the derived search_name and visibility of existing
// rows, and re-reads them: an existing row must carry exactly the expected canonical key, facts, and
// handle. It returns each needed symbol's handle.
func ensureSymbols(ctx context.Context, database *gorm.DB, symbols map[string]storage.Symbol, pending []pendingDocument) (map[string]int64, error) {
	needed := map[string]storage.Symbol{}
	for _, next := range pending {
		for _, id := range next.extracted.symbolIDs {
			needed[id] = symbols[id]
		}
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

func loadDocument(ctx context.Context, database *gorm.DB, rootID uuid.UUID, pathKey, inputHash string) (storage.Document, bool, error) {
	var document storage.Document
	err := database.WithContext(ctx).Where("root_id = ? AND path_key = ? AND input_hash = ?", rootID, pathKey, inputHash).First(&document).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return storage.Document{}, false, nil
	}
	if err != nil {
		return storage.Document{}, false, fmt.Errorf("load document for %q: %w", pathKey, err)
	}
	return document, true, nil
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
