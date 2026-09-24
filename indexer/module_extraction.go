package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
)

// moduleExtraction is everything publication writes for one root, computed before any transaction.
// reusedHead is set instead of the extracted facts when the root's head snapshot is reusable.
type moduleExtraction struct {
	root        discoveredRoot
	reusedHead  uuid.UUID
	contextHash string
	packages    []extractedPackage
	documents   map[string]extractedDocument
	symbols     map[string]storage.Symbol
	coverage    storage.Coverage
	diagnostics storage.JSON
}

// extractedPackage is one package_coverage row.
type extractedPackage struct {
	path            string
	inputHash       string
	exportShapeHash *string
	coverage        storage.Coverage
	fileCount       int
	diagnostics     []packageDiagnostic
}

// extractedDocument is one file's document with the symbol rows and postings it needs.
type extractedDocument struct {
	inputHash       string
	coverage        storage.Coverage
	content         storage.JSON
	symbolCount     int
	occurrenceCount int
	symbolIDs       []string
	postings        []storage.SymbolPosting
}

// extractModule type-checks the root, hashes each package's inputs, and renders every document.
func extractModule(ctx context.Context, loadPackages packageLoader, root discoveredRoot, includeTests bool) (moduleExtraction, error) {
	load, err := loadTyped(ctx, loadPackages, root, includeTests)
	if err != nil {
		return moduleExtraction{}, err
	}
	resolver := newSymbolResolver(load.origins)
	shapes, builder := newExportShapes(resolver, &load), newDocumentBuilder(resolver)
	grouped := map[string][]discoveredFile{}
	for _, file := range root.Files {
		grouped[file.PackagePath] = append(grouped[file.PackagePath], file)
	}
	extraction := moduleExtraction{root: root, documents: map[string]extractedDocument{}}
	inputHashes := map[string]string{}
	var coverages []storage.Coverage
	for _, packagePath := range sortedKeys(grouped) {
		extracted, err := extractPackage(&load, shapes, builder, &extraction, packagePath, grouped[packagePath])
		if err != nil {
			return moduleExtraction{}, fmt.Errorf("extract package %q: %w", packagePath, err)
		}
		inputHashes[packagePath] = extracted.inputHash
		coverages = append(coverages, extracted.coverage)
		extraction.packages = append(extraction.packages, extracted)
	}
	extraction.symbols = resolver.rows
	extraction.contextHash = contextHash(root.ConfigurationHash, inputHashes)
	extraction.coverage = weakestCoverage(coverages)
	if extraction.diagnostics, err = snapshotDiagnostics(extraction.packages); err != nil {
		return moduleExtraction{}, err
	}
	for path, document := range extraction.documents {
		if document, err = withSymbolFacts(document, resolver.rows); err != nil {
			return moduleExtraction{}, fmt.Errorf("document for %q: %w", path, err)
		}
		extraction.documents[path] = document
	}
	return extraction, nil
}

func extractPackage(load *typedLoad, shapes *exportShapes, builder *documentBuilder, extraction *moduleExtraction, packagePath string, files []discoveredFile) (extractedPackage, error) {
	state, err := load.classifyPackage(files)
	if err != nil {
		return extractedPackage{}, err
	}
	imports, err := shapes.importsOf(state.variants)
	if err != nil {
		return extractedPackage{}, err
	}
	extracted := extractedPackage{
		path: packagePath, inputHash: packageInputHash(load.root.ConfigurationHash, packagePath, files, imports),
		coverage: state.coverage, fileCount: len(files), diagnostics: uniqueDiagnostics(state.diagnostics),
	}
	if state.coverage == storage.CoverageIndexed {
		shape, err := shapes.of(state.exportVariant())
		if err != nil {
			return extractedPackage{}, err
		}
		extracted.exportShapeHash = &shape
	}
	for _, file := range files {
		content, coverage, err := renderDocument(builder, state, file, fileDiagnostics(extracted.diagnostics, file.PathKey))
		if err != nil {
			return extractedPackage{}, err
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			return extractedPackage{}, fmt.Errorf("encode document for %q: %w", file.PathKey, err)
		}
		extraction.documents[file.PathKey] = extractedDocument{
			inputHash: extracted.inputHash, coverage: coverage, content: storage.JSON(encoded),
			symbolCount: len(content.Symbols), occurrenceCount: len(content.Occurrences),
		}
	}
	return extracted, nil
}

// renderDocument is a file's document under its package's coverage: excluded files are empty and
// say why, a syntax package keeps the AST extraction, and the rest are typed.
func renderDocument(builder *documentBuilder, state packageState, file discoveredFile, diagnostics []storage.DocumentDiagnostic) (storage.DocumentContent, storage.Coverage, error) {
	if reason, excluded := state.excluded[file.PathKey]; excluded {
		return storage.DocumentContent{
			Version: storage.DocumentFormatVersion, PackagePath: file.PackagePath, Symbols: []storage.DocumentSymbol{},
			Occurrences: []storage.DocumentOccurrence{}, Diagnostics: []storage.DocumentDiagnostic{}, Excluded: reason,
		}, storage.CoverageExcluded, nil
	}
	if state.coverage == storage.CoverageSyntax {
		indexed, err := extractGoFile(file.PathKey, file.PackagePath, file.Content)
		if err != nil {
			return storage.DocumentContent{}, "", err
		}
		content := indexed.syntaxDocument()
		content.Diagnostics = diagnostics
		return content, storage.CoverageSyntax, nil
	}
	content, err := builder.typedDocument(file, state.typed[file.PathKey], diagnostics, state.coverage == storage.CoveragePartial)
	return content, state.coverage, err
}

// withSymbolFacts verifies a typed document against the symbol rows it names and derives its
// postings: definition from symbols, reference from every occurrence with a symbol, and implements
// from each implements entry, none for builtins. It also collects the rows the document needs,
// owners included, so publication can write them first.
func withSymbolFacts(document extractedDocument, rows map[string]storage.Symbol) (extractedDocument, error) {
	if document.coverage != storage.CoverageIndexed && document.coverage != storage.CoveragePartial {
		return document, nil
	}
	var content storage.DocumentContent
	if err := json.Unmarshal(document.content, &content); err != nil {
		return extractedDocument{}, err
	}
	counts, needed, err := countSymbolFacts(content, rows)
	if err != nil {
		return extractedDocument{}, err
	}
	for id := range needed {
		for owner := rows[id].OwnerID; owner != nil && !needed[*owner]; owner = rows[*owner].OwnerID {
			needed[*owner] = true
		}
	}
	document.symbolIDs = sortedKeys(needed)
	document.postings = make([]storage.SymbolPosting, 0, len(counts))
	for key, occurrences := range counts {
		document.postings = append(document.postings, storage.SymbolPosting{SymbolID: key.symbol, Role: key.role, OccurrenceCount: occurrences})
	}
	sort.Slice(document.postings, func(i, j int) bool {
		left, right := document.postings[i], document.postings[j]
		return left.SymbolID < right.SymbolID || (left.SymbolID == right.SymbolID && left.Role < right.Role)
	})
	return document, nil
}

type postingKey struct{ symbol, role string }

// countSymbolFacts counts a document's postings and collects every symbol id it names, checking each
// against the known rows and each entry's kind and visibility against its row.
func countSymbolFacts(content storage.DocumentContent, rows map[string]storage.Symbol) (map[postingKey]int, map[string]bool, error) {
	counts, needed := map[postingKey]int{}, map[string]bool{}
	count := func(id, role string) error {
		row, known := rows[id]
		if !known {
			return fmt.Errorf("symbol %s is not a known symbol", id)
		}
		needed[id] = true
		if row.Kind != "builtin" {
			counts[postingKey{id, role}]++
		}
		return nil
	}
	for _, symbol := range content.Symbols {
		if symbol.ID == nil {
			continue
		}
		if row := rows[*symbol.ID]; row.Kind != symbol.Kind || row.Visibility != symbol.Visibility {
			return nil, nil, fmt.Errorf("symbol %s is a %s %s entry but a %s %s row", *symbol.ID, symbol.Visibility, symbol.Kind, row.Visibility, row.Kind)
		}
		if err := count(*symbol.ID, "definition"); err != nil {
			return nil, nil, err
		}
		for _, implemented := range symbol.Implements {
			if err := count(implemented, "implements"); err != nil {
				return nil, nil, err
			}
		}
	}
	for _, occurrence := range content.Occurrences {
		if occurrence.Symbol != nil {
			if err := count(*occurrence.Symbol, "reference"); err != nil {
				return nil, nil, err
			}
		}
	}
	return counts, needed, nil
}

// snapshotDiagnostics summarizes, per package that is not fully indexed, its coverage and diagnostics.
func snapshotDiagnostics(packages []extractedPackage) (storage.JSON, error) {
	type summary struct {
		PackagePath string              `json:"package_path"`
		Coverage    storage.Coverage    `json:"coverage"`
		Diagnostics []packageDiagnostic `json:"diagnostics"`
	}
	summaries := []summary{}
	for _, extracted := range packages {
		if extracted.coverage != storage.CoverageIndexed {
			summaries = append(summaries, summary{PackagePath: extracted.path, Coverage: extracted.coverage, Diagnostics: extracted.diagnostics})
		}
	}
	encoded, err := json.Marshal(summaries)
	if err != nil {
		return nil, fmt.Errorf("encode snapshot diagnostics: %w", err)
	}
	return storage.JSON(encoded), nil
}
