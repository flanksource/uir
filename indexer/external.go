package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	"gorm.io/gorm"
)

// ErrInvalidPublication marks a publication Publish refused before writing anything, because of what it
// says rather than what the database holds: a missing field, a bad path or hash, an unknown kind or
// symbol, or a document the storage validation rejects.
var ErrInvalidPublication = errors.New("invalid publication")

// Publish writes a non-Go producer's root through the same publication core as Go indexing. It
// validates the whole publication first, against the database's kinds and the kinds it declares, and
// turns it into the extraction IndexModules would have produced; it then registers the declared kinds,
// and indexModule registers the root and its external location, compares the extraction with the head,
// and publishes an import snapshot with its documents, postings, source and symbol deltas, stats, and
// head, in one transaction. A publication whose revision and documents equal the head's is reported
// unchanged and publishes nothing. Every failure names the root and the document or symbol at fault.
func Publish(ctx context.Context, database *gorm.DB, publication Publication) (ModuleResult, error) {
	if database == nil {
		return ModuleResult{}, errors.New("UIR index database is required")
	}
	result, err := publishExternal(ctx, database, publication)
	if err != nil {
		return ModuleResult{}, fmt.Errorf("publish %q: %w", publication.RootKey, err)
	}
	return result, nil
}

func publishExternal(ctx context.Context, database *gorm.DB, publication Publication) (ModuleResult, error) {
	startedAt := time.Now().UTC()
	registered, err := storage.LoadSymbolKinds(ctx, database)
	if err != nil {
		return ModuleResult{}, err
	}
	extraction, err := publication.validatedExtraction(registered, startedAt)
	if err != nil {
		return ModuleResult{}, fmt.Errorf("%w: %w", ErrInvalidPublication, err)
	}
	for _, kind := range publication.Kinds {
		if _, err := storage.RegisterKind(ctx, database, kind.Name, kind.Category); err != nil {
			return ModuleResult{}, err
		}
	}
	var result ModuleResult
	err = storage.RetryAllocationConflicts(ctx, database, func(transaction *gorm.DB) error {
		var publishErr error
		result, _, publishErr = indexModule(ctx, transaction, extraction, map[string]storage.ModuleLocation{}, ModuleOptions{Reason: storage.ReasonImport})
		return publishErr
	})
	return result, err
}

// validatedExtraction checks the publication without writing anything and returns its extraction.
func (publication Publication) validatedExtraction(registered symbolhandle.Kinds, startedAt time.Time) (moduleExtraction, error) {
	if err := publication.validate(); err != nil {
		return moduleExtraction{}, err
	}
	kinds, err := publication.declaredKinds(registered)
	if err != nil {
		return moduleExtraction{}, err
	}
	rows, err := publication.symbolRows(kinds)
	if err != nil {
		return moduleExtraction{}, err
	}
	return publication.extraction(kinds, rows, startedAt)
}

// extraction is the publication as the publication core takes a Go module's: one discovered root at
// the external location, its files, and per package an input hash over its documents, with every
// document's postings and definitions derived from its content.
func (publication Publication) extraction(kinds symbolhandle.Kinds, rows map[string]storage.Symbol, startedAt time.Time) (moduleExtraction, error) {
	configuration := externalConfigurationHash()
	files := make([]discoveredFile, 0, len(publication.Documents))
	byPackage := map[string][]PublishedDocument{}
	for _, document := range publication.Documents {
		files = append(files, document.file())
		byPackage[document.PackagePath] = append(byPackage[document.PackagePath], document)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].PathKey < files[j].PathKey })
	extraction := moduleExtraction{
		root: discoveredRoot{
			RootKey: publication.RootKey, Name: publication.Name, LocalPath: publication.Location.URI, Kind: storage.LocationExternal,
			Revision: publication.Revision, WorktreeState: storage.WorktreeUnknown, LastModifiedAt: startedAt,
			ContentSetHash: contentSetHash(files, nil), ConfigurationHash: configuration, Files: files,
		},
		indexerVersion: ExternalIndexerVersion, indexStartedAt: startedAt, documents: map[string]extractedDocument{},
		symbols: rows, coverage: storage.CoverageIndexed,
	}
	inputHashes := make(map[string]string, len(byPackage))
	for _, packagePath := range sortedKeys(byPackage) {
		extracted, err := publication.extractPackage(&extraction, kinds, packagePath, byPackage[packagePath])
		if err != nil {
			return moduleExtraction{}, err
		}
		inputHashes[packagePath] = extracted.inputHash
		extraction.packages = append(extraction.packages, extracted)
	}
	extraction.contextHash = contextHash(configuration, inputHashes)
	var err error
	extraction.diagnostics, err = snapshotDiagnostics(extraction.packages)
	return extraction, err
}

func (document PublishedDocument) file() discoveredFile {
	return discoveredFile{
		PathKey: document.PathKey, PackagePath: document.PackagePath, Content: []byte(document.Source),
		ContentHash: document.ContentHash, SizeBytes: int64(len(document.Source)),
	}
}

// extractPackage adds a package's documents to the extraction and returns its coverage row. Every
// document is indexed under the package's input hash, passes the storage validation it will pass again
// when written, names only published symbols, and declares only symbols of this root and package:
// queries select a symbol in its own module's root, and symbol deltas group declarations by package.
func (publication Publication) extractPackage(extraction *moduleExtraction, kinds symbolhandle.Kinds, packagePath string, documents []PublishedDocument) (extractedPackage, error) {
	files := make([]discoveredFile, len(documents))
	declared := make([]storage.DocumentContent, len(documents))
	contents := make(map[string]storage.JSON, len(documents))
	for i, document := range documents {
		encoded, err := json.Marshal(document.Content)
		if err != nil {
			return extractedPackage{}, fmt.Errorf("encode document %q: %w", document.PathKey, err)
		}
		files[i], declared[i], contents[document.PathKey] = document.file(), document.Content, encoded
	}
	inputHash := externalInputHash(extraction.root.ConfigurationHash, packagePath, files, contents)
	for _, document := range documents {
		extracted := extractedDocument{
			inputHash: inputHash, coverage: storage.CoverageIndexed, content: contents[document.PathKey],
			symbolCount: len(document.Content.Symbols), occurrenceCount: len(document.Content.Occurrences),
		}
		if _, err := storage.DecodeDocument(document.unwritten(extracted), storage.SourceRevision{PathKey: document.PathKey, PackagePath: document.PackagePath}, storage.WithKinds(kinds)); err != nil {
			return extractedPackage{}, err
		}
		extracted, err := withSymbolFacts(extracted, extraction.symbols)
		if err != nil {
			return extractedPackage{}, fmt.Errorf("document %q: %w", document.PathKey, err)
		}
		if err := publication.requireOwnDeclarations(document, extraction.symbols); err != nil {
			return extractedPackage{}, err
		}
		extraction.documents[document.PathKey] = extracted
	}
	exportShape := externalExportShape(declared)
	return extractedPackage{
		path: packagePath, inputHash: inputHash, exportShapeHash: &exportShape, coverage: storage.CoverageIndexed,
		fileCount: len(documents), diagnostics: []packageDiagnostic{},
	}, nil
}

// unwritten is the document row the extraction will write, before it has an id, a root, or a revision.
func (document PublishedDocument) unwritten(extracted extractedDocument) storage.Document {
	return storage.Document{
		PathKey: document.PathKey, PackagePath: document.PackagePath, InputHash: extracted.inputHash, IndexerVersion: ExternalIndexerVersion,
		Coverage: extracted.coverage, SymbolCount: extracted.symbolCount, OccurrenceCount: extracted.occurrenceCount, Content: extracted.content,
	}
}

func (publication Publication) requireOwnDeclarations(document PublishedDocument, rows map[string]storage.Symbol) error {
	for _, symbol := range document.Content.Symbols {
		if symbol.ID == nil {
			continue
		}
		if row := rows[*symbol.ID]; row.ModuleKey != publication.RootKey || row.PackagePath != document.PackagePath {
			return fmt.Errorf("document %q declares %s of root %q and package %q; it may declare only symbols of root %q and package %q",
				document.PathKey, publishedName(rows, row), row.ModuleKey, row.PackagePath, publication.RootKey, document.PackagePath)
		}
	}
	return nil
}
