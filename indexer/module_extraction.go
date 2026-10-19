package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/types"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"github.com/flanksource/uir/indexer/syntax"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
)

// moduleExtraction is everything publication writes for one root, computed before any transaction.
// reusedHead is set instead of the extracted facts when the root's head snapshot is reusable.
// indexerVersion is what produced the documents: IndexerVersion for Go, ExternalIndexerVersion for a
// publication.
type moduleExtraction struct {
	root           discoveredRoot
	reusedHead     uuid.UUID
	baseSnapshotID uuid.UUID
	requestedBase  uuid.UUID
	indexerVersion string
	indexStartedAt time.Time
	contextHash    string
	packages       []extractedPackage
	documents      map[string]extractedDocument
	symbols        map[string]storage.Symbol
	coverage       storage.Coverage
	// coverageDeterministic is whether extracting the same commit, configuration and dependency set
	// again must reach coverage again (typedLoad.deterministicCoverage).
	coverageDeterministic bool
	diagnostics           storage.JSON
	// siblingShapes is every workspace sibling package the load read, keyed by package path, with the
	// shape a direct import presented and the digest of its sources; the map is nil when one import
	// path presented two shapes, so the snapshot records none.
	siblingShapes map[string]siblingRecord
	// implements are the snapshot's implements facts; a producer without types has none.
	implements []implementsFact
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

// extractedDocument is one file's document with the symbol rows and postings it needs, and the
// fingerprints of every canonical symbol it declares. A reused document is the base snapshot's stored
// one, base, for a package whose input hash equals the base's: it carries no content, postings or
// definitions, since publication neither writes it nor compares its symbols.
type extractedDocument struct {
	reused          bool
	base            storage.Document
	inputHash       string
	coverage        storage.Coverage
	content         storage.JSON
	symbolCount     int
	occurrenceCount int
	symbolIDs       []string
	postings        []extractedPosting
	definitions     []definition
}

// extractedPosting is one posting of a document before its ordinal and the symbol's handle exist.
type extractedPosting struct {
	symbolID    string
	role        storage.PostingRole
	occurrences int
}

// extractOptions is what extractModule type-checks and which stored documents it may reuse.
type extractOptions struct {
	loadPackages packageLoader
	root         discoveredRoot
	includeTests bool
	// base is the active documents of the snapshot the result will be published on, by package path.
	// A package whose input hash equals its base's keeps the base's documents instead of rendering.
	base         map[string]basePackage
	baseSnapshot storage.ModuleSnapshot
}

// extractModule type-checks the root, hashes each package's inputs, and renders every document whose
// package's inputs differ from the base's.
func extractModule(ctx context.Context, options extractOptions) (moduleExtraction, error) {
	startedAt := time.Now().UTC()
	root := options.root
	var load typedLoad
	err := Phase(ctx, "typecheck", func(ctx context.Context) (err error) {
		load, err = loadTyped(ctx, options.loadPackages, root, options.includeTests)
		PhaseCount(ctx, "packages", int64(len(load.origins)))
		PhaseCount(ctx, "files", int64(len(load.files)))
		PhaseCount(ctx, "parsed", int64(len(load.parsed)))
		return err
	})
	if err != nil {
		var loadFailure packageLoadError
		if !root.Historical || !errors.As(err, &loadFailure) || ctx.Err() != nil || !sourceResolutionFailure(err) {
			return moduleExtraction{}, err
		}
		load = typedLoad{
			root: root, origins: map[*types.Package]packageOrigin{}, files: map[string]typedFile{},
			ignored: map[string]bool{}, directories: map[string]bool{}, parsed: map[string]string{}, loadFailure: err.Error(),
		}
		for _, file := range root.Files {
			load.directories[filepath.Dir(file.AbsolutePath)] = true
		}
	}
	var extraction moduleExtraction
	err = Phase(ctx, "extract", func(ctx context.Context) (err error) {
		extraction, err = extractTypedLoad(ctx, &load, options.base, startedAt)
		return err
	})
	return extraction, err
}

// packageExtraction is the state extractPackage shares across one root's packages. typed are the
// packages that type-checked, whose declared types the implements pass relates.
type packageExtraction struct {
	load        *typedLoad
	shapes      *exportShapes
	builder     *documentBuilder
	extraction  *moduleExtraction
	base        map[string]basePackage
	rendered    int
	inputHashes map[string]string
	coverages   []storage.Coverage
	typed       []packageState
}

// extractTypedLoad hashes each package's inputs of a type-checked root, renders every document of a
// package whose input hash differs from base's, and computes the snapshot's implements facts.
func extractTypedLoad(ctx context.Context, load *typedLoad, base map[string]basePackage, startedAt time.Time) (moduleExtraction, error) {
	root := load.root
	resolver := newSymbolResolver(load.origins)
	extraction := moduleExtraction{
		root: root, indexerVersion: IndexerVersion, indexStartedAt: startedAt, documents: map[string]extractedDocument{}, siblingShapes: map[string]siblingRecord{},
	}
	extractor := &packageExtraction{
		load: load, shapes: newExportShapes(resolver, load), builder: newDocumentBuilder(resolver), extraction: &extraction, base: base, inputHashes: map[string]string{},
	}
	if err := Phase(ctx, "render-documents", extractor.extractPackages); err != nil {
		return moduleExtraction{}, err
	}
	if err := Phase(ctx, "implements", func(ctx context.Context) (err error) {
		extraction.implements, err = snapshotImplements(load, resolver, extractor.shapes, extractor.typed)
		PhaseCount(ctx, "facts", int64(len(extraction.implements)))
		return err
	}); err != nil {
		return moduleExtraction{}, err
	}
	extraction.symbols = resolver.rows
	extraction.contextHash = contextHash(root.ConfigurationHash, extractor.inputHashes)
	extraction.coverage = weakestCoverage(extractor.coverages)
	extraction.coverageDeterministic = load.deterministicCoverage()
	var err error
	if extraction.diagnostics, err = snapshotDiagnostics(extraction.packages); err != nil {
		return moduleExtraction{}, err
	}
	return extraction, nil
}

// extractPackages extracts the root's packages in path order and records its sibling sources.
func (extractor *packageExtraction) extractPackages(ctx context.Context) error {
	extraction := extractor.extraction
	grouped := map[string][]discoveredFile{}
	for _, file := range extractor.load.root.Files {
		grouped[file.PackagePath] = append(grouped[file.PackagePath], file)
	}
	for _, packagePath := range sortedKeys(grouped) {
		extracted, err := extractor.extractPackage(ctx, packagePath, grouped[packagePath])
		if err != nil {
			return fmt.Errorf("extract package %q: %w", packagePath, err)
		}
		extractor.inputHashes[packagePath] = extracted.inputHash
		extractor.coverages = append(extractor.coverages, extracted.coverage)
		extraction.packages = append(extraction.packages, extracted)
	}
	if err := extractor.shapes.recordSiblingSources(extraction.siblingShapes); err != nil {
		return err
	}
	PhaseCount(ctx, "packages", int64(len(extraction.packages)))
	PhaseCount(ctx, "documents", int64(len(extraction.documents)))
	PhaseCount(ctx, "rendered", int64(extractor.rendered))
	return nil
}

func (extractor *packageExtraction) extractPackage(ctx context.Context, packagePath string, files []discoveredFile) (extractedPackage, error) {
	load, shapes, extraction := extractor.load, extractor.shapes, extractor.extraction
	state, err := load.classifyPackage(files)
	if err != nil {
		return extractedPackage{}, err
	}
	if load.root.Historical && state.coverage == storage.CoveragePartial {
		state.coverage = storage.CoverageSyntax
	}
	if state.coverage == storage.CoverageIndexed || state.coverage == storage.CoveragePartial {
		extractor.typed = append(extractor.typed, state)
	}
	imports, err := shapes.importsOf(state.variants)
	if err != nil {
		return extractedPackage{}, err
	}
	if extraction.siblingShapes != nil {
		conflict, err := shapes.recordSiblings(state.variants, extraction.siblingShapes)
		if err != nil {
			return extractedPackage{}, err
		}
		if conflict {
			extraction.siblingShapes = nil
		}
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
	if base, found := extractor.base[packagePath]; found && base.inputHash == extracted.inputHash && base.coverage == extracted.coverage && reflect.DeepEqual(base.diagnostics, extracted.diagnostics) {
		return extracted, reuseBaseDocuments(extraction, base, files)
	}
	for _, file := range files {
		content, coverage, err := renderDocument(extractor.builder, state, file, fileDiagnostics(extracted.diagnostics, file.PathKey))
		if err != nil {
			return extractedPackage{}, err
		}
		document := extractedDocument{
			inputHash: extracted.inputHash, coverage: coverage,
			symbolCount: len(content.Symbols), occurrenceCount: len(content.Occurrences),
		}
		if err := Phase(ctx, "symbol-facts", func(ctx context.Context) (err error) {
			document, err = withSymbolFacts(document, content, extractor.builder.resolver.rows)
			PhaseCount(ctx, "symbols", int64(len(document.symbolIDs)))
			return err
		}); err != nil {
			return extractedPackage{}, fmt.Errorf("document for %q: %w", file.PathKey, err)
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			return extractedPackage{}, fmt.Errorf("encode document for %q: %w", file.PathKey, err)
		}
		document.content = storage.JSON(encoded)
		extraction.documents[file.PathKey] = document
		extractor.rendered++
	}
	return extracted, nil
}

// reuseBaseDocuments keeps the base's bound document for every file of a package whose input hash
// equals the base's. The input hash covers the package's files, so the base binds a document for each.
func reuseBaseDocuments(extraction *moduleExtraction, base basePackage, files []discoveredFile) error {
	for _, file := range files {
		stored, found := base.documents[file.PathKey]
		if !found {
			return fmt.Errorf("base package %q under input hash %s has no document for %q", file.PackagePath, base.inputHash, file.PathKey)
		}
		extraction.documents[file.PathKey] = extractedDocument{
			inputHash: base.inputHash, coverage: stored.Coverage, symbolCount: stored.SymbolCount, occurrenceCount: stored.OccurrenceCount, reused: true, base: stored,
		}
	}
	return nil
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
		indexed, err := syntax.Extract(file.PathKey, file.PackagePath, file.Content)
		if err != nil {
			return storage.DocumentContent{}, "", err
		}
		content := indexed.Document()
		content.Diagnostics = diagnostics
		return content, storage.CoverageSyntax, nil
	}
	content, err := builder.typedDocument(file, state.typed[file.PathKey], diagnostics, state.coverage == storage.CoveragePartial)
	return content, state.coverage, err
}

// withSymbolFacts verifies a typed document against the symbol rows it names and derives its
// postings: definition from symbols, reference from every occurrence with a symbol, and embeds from
// each embeds entry, none for builtins. It also collects the rows the document needs,
// owners included, so publication can write them first.
func withSymbolFacts(document extractedDocument, content storage.DocumentContent, rows map[string]storage.Symbol) (extractedDocument, error) {
	if document.reused || document.coverage != storage.CoverageIndexed && document.coverage != storage.CoveragePartial {
		return document, nil
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
	document.postings = make([]extractedPosting, 0, len(counts))
	for key, occurrences := range counts {
		document.postings = append(document.postings, extractedPosting{symbolID: key.symbol, role: key.role, occurrences: occurrences})
	}
	sort.Slice(document.postings, func(i, j int) bool {
		left, right := document.postings[i], document.postings[j]
		return left.symbolID < right.symbolID || (left.symbolID == right.symbolID && left.role < right.role)
	})
	for _, symbol := range content.Symbols {
		if document.definitions, err = appendDefinition(document.definitions, symbol); err != nil {
			return extractedDocument{}, err
		}
	}
	return document, nil
}

type postingKey struct {
	symbol string
	role   storage.PostingRole
}

// countSymbolFacts counts a document's postings and collects every symbol id it names, checking each
// against the known rows and each entry's kind and visibility against its row.
func countSymbolFacts(content storage.DocumentContent, rows map[string]storage.Symbol) (map[postingKey]int, map[string]bool, error) {
	counts, needed := map[postingKey]int{}, map[string]bool{}
	count := func(id string, role storage.PostingRole) error {
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
		if err := count(*symbol.ID, storage.RoleDefinition); err != nil {
			return nil, nil, err
		}
		if row := rows[*symbol.ID]; row.Kind != symbol.Kind || row.Visibility != symbol.Visibility {
			return nil, nil, fmt.Errorf("symbol %s is a %s %s entry but a %s %s row", *symbol.ID, symbol.Visibility, symbol.Kind, row.Visibility, row.Kind)
		}
		for _, embedded := range symbol.Embeds {
			if err := count(embedded, storage.RoleEmbeds); err != nil {
				return nil, nil, err
			}
		}
	}
	for _, occurrence := range content.Occurrences {
		if occurrence.Symbol != nil {
			if err := count(*occurrence.Symbol, storage.RoleReference); err != nil {
				return nil, nil, err
			}
		}
	}
	for _, relationship := range content.Relationships {
		if err := count(relationship.Source, storage.RoleRelationshipSource); err != nil {
			return nil, nil, err
		}
		for _, target := range relationship.Targets {
			if err := count(target, storage.RoleRelationshipTarget); err != nil {
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
