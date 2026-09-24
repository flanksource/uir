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
// symbol rows first and their postings after; postings of a document another writer inserted are
// never written twice.
func publishDocuments(ctx context.Context, database *gorm.DB, extraction moduleExtraction, current map[string]storage.SourceRevision, force bool, result *ModuleResult) error {
	var pending []pendingDocument
	for _, file := range extraction.root.Files {
		extracted, found := extraction.documents[file.PathKey]
		if !found {
			return fmt.Errorf("no document was extracted for %q", file.PathKey)
		}
		revision := current[file.PathKey]
		document := storage.Document{
			ID: uuid.New(), RootID: revision.RootID, PathKey: file.PathKey, SourceRevisionID: revision.ID,
			PackagePath: file.PackagePath, InputHash: extracted.inputHash, IndexerVersion: IndexerVersion,
			Coverage: extracted.coverage, SymbolCount: extracted.symbolCount, OccurrenceCount: extracted.occurrenceCount,
			Content: extracted.content,
		}
		if _, err := storage.DecodeDocument(document, revision); err != nil {
			return fmt.Errorf("validate extracted document: %w", err)
		}
		existing, found, err := loadDocument(ctx, database, revision.RootID, file.PathKey, extracted.inputHash)
		if err != nil {
			return err
		}
		if found && existing.SourceRevisionID != revision.ID {
			return fmt.Errorf("document %s for %q describes revision %s, expected %s", existing.ID, file.PathKey, existing.SourceRevisionID, revision.ID)
		}
		switch {
		case !found:
			pending = append(pending, pendingDocument{document: document, extracted: extracted})
		case force:
			pending = append(pending, pendingDocument{document: document, extracted: extracted, existing: &existing})
		default:
			result.ReusedFiles++
		}
	}
	if err := ensureSymbols(ctx, database, extraction.symbols, pending); err != nil {
		return err
	}
	for _, next := range pending {
		if err := publishDocument(ctx, database, next); err != nil {
			return err
		}
		result.ParsedFiles++
	}
	return nil
}

func publishDocument(ctx context.Context, database *gorm.DB, next pendingDocument) error {
	document := next.document
	if next.existing == nil {
		if err := database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&document).Error; err != nil {
			return fmt.Errorf("create document for %q: %w", document.PathKey, err)
		}
		stored, found, err := loadDocument(ctx, database, document.RootID, document.PathKey, document.InputHash)
		if err != nil || !found {
			return errors.Join(fmt.Errorf("load published document for %q", document.PathKey), err)
		}
		if stored.ID == document.ID {
			return createPostings(ctx, database, document, next.extracted.postings)
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

func createPostings(ctx context.Context, database *gorm.DB, document storage.Document, postings []storage.SymbolPosting) error {
	if len(postings) == 0 {
		return nil
	}
	rows := make([]storage.SymbolPosting, len(postings))
	for i, posting := range postings {
		posting.DocumentID, posting.RootID = document.ID, document.RootID
		rows[i] = posting
	}
	if err := database.WithContext(ctx).CreateInBatches(rows, publicationBatch).Error; err != nil {
		return fmt.Errorf("create %d postings for %q: %w", len(rows), document.PathKey, err)
	}
	return nil
}

// ensureSymbols upserts the symbol rows the pending documents need, owners before the symbols they
// own, refreshing the derived search_name and visibility of existing rows, and re-reads them: an
// existing row must carry exactly the expected canonical key and facts.
func ensureSymbols(ctx context.Context, database *gorm.DB, symbols map[string]storage.Symbol, pending []pendingDocument) error {
	needed := map[string]storage.Symbol{}
	for _, next := range pending {
		for _, id := range next.extracted.symbolIDs {
			needed[id] = symbols[id]
		}
	}
	if len(needed) == 0 {
		return nil
	}
	depth := func(row storage.Symbol) int {
		levels := 0
		for owner := row.OwnerID; owner != nil; owner = symbols[*owner].OwnerID {
			levels++
		}
		return levels
	}
	rows := make([]storage.Symbol, 0, len(needed))
	for _, id := range sortedKeys(needed) {
		rows = append(rows, needed[id])
	}
	sort.SliceStable(rows, func(i, j int) bool { return depth(rows[i]) < depth(rows[j]) })
	if err := storage.UpsertSymbols(ctx, database, rows, publicationBatch); err != nil {
		return err
	}
	ids := sortedKeys(needed)
	for start := 0; start < len(ids); start += publicationBatch {
		batch := ids[start:min(start+publicationBatch, len(ids))]
		var stored []storage.Symbol
		if err := database.WithContext(ctx).Where("id IN ?", batch).Find(&stored).Error; err != nil {
			return fmt.Errorf("load published symbols: %w", err)
		}
		if len(stored) != len(batch) {
			return fmt.Errorf("published %d symbols, found %d", len(batch), len(stored))
		}
		for _, row := range stored {
			if err := sameSymbol(row, needed[row.ID]); err != nil {
				return err
			}
		}
	}
	return nil
}

func sameSymbol(stored, expected storage.Symbol) error {
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
