package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

func (publication Publication) deltaExtraction(ctx context.Context, database *gorm.DB, extraction moduleExtraction) (moduleExtraction, error) {
	base, err := publication.deltaBase(ctx, database, extraction)
	if err != nil {
		return moduleExtraction{}, err
	}
	if len(publication.Documents) == 0 && len(publication.Delta.DeletedPaths) == 0 {
		return carriedExtraction(extraction, base)
	}
	active, err := storage.ActiveDocuments(ctx, database, base.ID, storage.ActiveDocumentOptions{})
	if err != nil {
		return moduleExtraction{}, err
	}
	extraction.baseActive, extraction.baseActiveOf = active, base.ID
	removed, affected, err := publication.deltaPaths(active)
	if err != nil {
		return moduleExtraction{}, err
	}
	for name, document := range active {
		if removed[name] {
			continue
		}
		if _, replaced := extraction.documents[name]; replaced {
			continue
		}
		if affected[document.Source.PackagePath] {
			return moduleExtraction{}, fmt.Errorf("delta package %q omits %q; provide the complete package or delete the path", document.Source.PackagePath, name)
		}
		extraction.root.Files = append(extraction.root.Files, discoveredFile{retained: true, PathKey: name, PackagePath: document.Source.PackagePath,
			ContentHash: document.Source.ContentHash, SizeBytes: document.Source.SizeBytes, GitPath: document.Source.GitPath})
		extraction.documents[name] = extractedDocument{reused: true, base: document.Document, inputHash: document.Document.InputHash,
			coverage: document.Document.Coverage, symbolCount: document.Document.SymbolCount, occurrenceCount: document.Document.OccurrenceCount}
	}
	if extraction.packages, err = retainedPackages(ctx, database, base, affected, extraction.packages); err != nil {
		return moduleExtraction{}, err
	}
	sort.Slice(extraction.root.Files, func(i, j int) bool { return extraction.root.Files[i].PathKey < extraction.root.Files[j].PathKey })
	sort.Slice(extraction.packages, func(i, j int) bool { return extraction.packages[i].path < extraction.packages[j].path })
	extraction.root.ContentSetHash = contentSetHash(extraction.root.Files, nil)
	hashes := make(map[string]string, len(extraction.packages))
	for _, pkg := range extraction.packages {
		hashes[pkg.path] = pkg.inputHash
	}
	extraction.contextHash = contextHash(extraction.root.ConfigurationHash, hashes)
	extraction.diagnostics, err = snapshotDiagnostics(extraction.packages)
	return extraction, err
}

// retainedPackages adds to packages the base's coverage of every package the delta leaves unaffected.
func retainedPackages(ctx context.Context, database *gorm.DB, base storage.ModuleSnapshot, affected map[string]bool, packages []extractedPackage) ([]extractedPackage, error) {
	var coverage []storage.PackageCoverage
	if err := database.WithContext(ctx).Where("snapshot_id = ?", base.ID).Find(&coverage).Error; err != nil {
		return nil, err
	}
	for _, row := range coverage {
		if affected[row.PackagePath] {
			continue
		}
		var diagnostics []packageDiagnostic
		if err := json.Unmarshal(row.Diagnostics, &diagnostics); err != nil {
			return nil, fmt.Errorf("decode base package %q diagnostics: %w", row.PackagePath, err)
		}
		packages = append(packages, extractedPackage{path: row.PackagePath, inputHash: row.InputHash,
			exportShapeHash: row.ExportShapeHash, coverage: row.Coverage, fileCount: row.FileCount, diagnostics: diagnostics})
	}
	return packages, nil
}

// carriedExtraction is the extraction of a delta that changes nothing: it serves exactly the base's
// sources and documents under the base's content set, context, and diagnostics, so publication
// carries the base forward instead of binding every file again.
func carriedExtraction(extraction moduleExtraction, base storage.ModuleSnapshot) (moduleExtraction, error) {
	stats, err := base.Stats()
	if err != nil {
		return moduleExtraction{}, fmt.Errorf("carry delta base forward: %w", err)
	}
	extraction.carried = &carriedBase{snapshot: base, stats: stats}
	extraction.root.ContentSetHash, extraction.contextHash, extraction.diagnostics = base.ContentSetHash, base.ContextHash, base.Diagnostics
	return extraction, nil
}

func (publication Publication) deltaBase(ctx context.Context, database *gorm.DB, extraction moduleExtraction) (storage.ModuleSnapshot, error) {
	var base storage.ModuleSnapshot
	if err := database.WithContext(ctx).Where("id = ?", extraction.requestedBase).First(&base).Error; err != nil {
		return base, fmt.Errorf("load delta base %s: %w", extraction.requestedBase, err)
	}
	var root storage.ModuleRoot
	if err := database.WithContext(ctx).Where("id = ?", base.RootID).First(&root).Error; err != nil {
		return base, err
	}
	if root.RootKey != publication.RootKey || base.ConfigurationHash != extraction.root.ConfigurationHash || base.Coverage != storage.CoverageIndexed {
		return base, fmt.Errorf("delta base %s is incompatible with root %q, producer configuration %s and indexed coverage", base.ID, publication.RootKey, extraction.root.ConfigurationHash)
	}
	var location storage.ModuleLocation
	if err := database.WithContext(ctx).Where("id = ?", base.LocationID).First(&location).Error; err != nil {
		return base, err
	}
	repository := ""
	if location.RepositoryURI != nil {
		repository = *location.RepositoryURI
	}
	if location.Kind != storage.LocationExternal || repository != publication.Location.RepositoryURI {
		return base, fmt.Errorf("delta base %s has a different source origin", base.ID)
	}
	return base, nil
}

func (publication Publication) deltaPaths(active map[string]storage.ActiveDocument) (removed, affected map[string]bool, err error) {
	removed, affected = map[string]bool{}, map[string]bool{}
	for _, name := range publication.Delta.DeletedPaths {
		if !fs.ValidPath(name) || name == "." || strings.ContainsRune(name, 0) || removed[name] {
			return nil, nil, fmt.Errorf("invalid or duplicate delta deletion %q", name)
		}
		prior, found := active[name]
		if !found {
			return nil, nil, fmt.Errorf("delta deletes unknown path %q", name)
		}
		removed[name], affected[prior.Source.PackagePath] = true, true
	}
	for _, document := range publication.Documents {
		if removed[document.PathKey] {
			return nil, nil, fmt.Errorf("delta both replaces and deletes %q", document.PathKey)
		}
		affected[document.PackagePath] = true
		if prior, found := active[document.PathKey]; found {
			affected[prior.Source.PackagePath] = true
		}
	}
	return removed, affected, nil
}
