package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/flanksource/uir/indexer/syntax"
	"github.com/flanksource/uir/storage"
)

func (indexer *Indexer) extractAnalysis(ctx context.Context, root discoveredRoot, includeTests bool) (extraction moduleExtraction, inputs analysisInputs, err error) {
	if root.Historical && root.AnalysisMode == storage.AnalysisTyped {
		var scratch string
		scratch, err = os.MkdirTemp(filepath.Dir(root.LocalPath), "analysis-manifests-")
		if err != nil {
			return moduleExtraction{}, analysisInputs{}, err
		}
		defer func() {
			if cleanup := os.RemoveAll(scratch); cleanup != nil {
				err = errors.Join(err, fmt.Errorf("remove analysis manifests: %w", cleanup))
			}
		}()
		configuration := root.ConfigurationHash
		root, err = isolateRoot(ctx, root, includeTests, scratch)
		if err != nil {
			return moduleExtraction{}, analysisInputs{}, err
		}
		root.ConfigurationHash = configuration
	}
	inputs, err = analysisManifests(root)
	if err != nil {
		return moduleExtraction{}, inputs, err
	}
	if root.AnalysisMode == storage.AnalysisSyntax {
		extraction, err := extractSyntaxAnalysis(root)
		return extraction, inputs, err
	}
	load, err := loadTyped(ctx, indexer.loadPackages, root, includeTests)
	if err != nil {
		return moduleExtraction{}, inputs, err
	}
	for path := range load.parsed {
		directory := filepath.Dir(path)
		module, _, found, err := enclosingModule(directory)
		if err != nil {
			return moduleExtraction{}, inputs, err
		}
		if found && module != root.LocalPath {
			inputs.Packages[directory] = module
		}
	}
	for directory, module := range inputs.Packages {
		digest, err := packageSourceDigest(module, directory, load.parsed)
		if err != nil {
			return moduleExtraction{}, inputs, err
		}
		inputs.Digests[directory] = digest
	}
	extraction, err = extractTypedLoad(ctx, &load, nil, time.Now().UTC())
	return extraction, inputs, err
}

func extractSyntaxAnalysis(root discoveredRoot) (moduleExtraction, error) {
	extraction := moduleExtraction{root: root, indexerVersion: IndexerVersion, indexStartedAt: time.Now().UTC(), documents: map[string]extractedDocument{}, symbols: map[string]storage.Symbol{}, coverage: storage.CoverageSyntax, coverageDeterministic: true}
	selection := build.Default
	selection.GOOS, selection.GOARCH, selection.CgoEnabled, selection.BuildTags = root.Variant.GOOS, root.Variant.GOARCH, root.Variant.CGOEnabled == "1", root.Variant.BuildTags
	packagesByPath := map[string]*extractedPackage{}
	for _, file := range root.Files {
		content, err := syntaxAnalysisDocument(selection, file)
		if err != nil {
			return extraction, err
		}
		coverage := storage.CoverageExcluded
		if content.Excluded == "" {
			coverage = storage.CoverageSyntax
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			return extraction, err
		}
		extraction.documents[file.PathKey] = extractedDocument{inputHash: root.ContentSetHash, coverage: coverage, content: encoded, symbolCount: len(content.Symbols), occurrenceCount: len(content.Occurrences)}
		if packagesByPath[file.PackagePath] == nil {
			packagesByPath[file.PackagePath] = &extractedPackage{path: file.PackagePath, inputHash: root.ContentSetHash, coverage: storage.CoverageExcluded}
		}
		pkg := packagesByPath[file.PackagePath]
		pkg.fileCount++
		if coverage == storage.CoverageSyntax {
			pkg.coverage = storage.CoverageSyntax
		}
	}
	for _, path := range sortedKeys(packagesByPath) {
		extraction.packages = append(extraction.packages, *packagesByPath[path])
	}
	extraction.contextHash = contextHash(root.ConfigurationHash, map[string]string{"sources": root.ContentSetHash})
	var err error
	extraction.diagnostics, err = snapshotDiagnostics(extraction.packages)
	return extraction, err
}

func syntaxAnalysisDocument(selection build.Context, file discoveredFile) (storage.DocumentContent, error) {
	content := storage.DocumentContent{Version: storage.DocumentFormatVersion, PackagePath: file.PackagePath, Symbols: []storage.DocumentSymbol{}, Occurrences: []storage.DocumentOccurrence{}, Diagnostics: []storage.DocumentDiagnostic{},
		Excluded: fmt.Sprintf("build constraints exclude the file under GOOS=%s GOARCH=%s CGO_ENABLED=%t", selection.GOOS, selection.GOARCH, selection.CgoEnabled)}
	selected, err := selection.MatchFile(filepath.Dir(file.AbsolutePath), filepath.Base(file.AbsolutePath))
	if err != nil || !selected {
		return content, err
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), file.PathKey, file.Content, parser.ImportsOnly)
	if err != nil {
		return content, fmt.Errorf("parse imports in %s: %w", file.PathKey, err)
	}
	for _, imported := range parsed.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			return content, fmt.Errorf("parse import in %s: %w", file.PathKey, err)
		}
		if path == "C" && !selection.CgoEnabled {
			return content, nil
		}
	}
	indexed, err := syntax.Extract(file.PathKey, content.PackagePath, file.Content)
	if err != nil {
		return content, fmt.Errorf("parse %s: %w", file.PathKey, err)
	}
	return indexed.Document(), nil
}
