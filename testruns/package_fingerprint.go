package testruns

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/uir/internal/canonicalhash"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type sourceDocument struct {
	path        string
	contentHash string
	imports     []storage.DocumentImport
}

type packageFingerprintResult struct {
	fingerprint string
	complete    bool
}

type packageFingerprints struct {
	packages map[string][]sourceDocument
	cache    map[string]packageFingerprintResult
}

// newPackageFingerprints decodes one snapshot's active documents once; of then derives and memoizes
// the distinct import closure used by each test package in that snapshot.
func newPackageFingerprints(ctx context.Context, database *gorm.DB, snapshot uuid.UUID) (*packageFingerprints, error) {
	active, err := storage.ActiveDocuments(ctx, database, snapshot, storage.ActiveDocumentOptions{Content: true})
	if err != nil {
		return nil, err
	}
	packages := map[string][]sourceDocument{}
	for path, document := range active {
		var content storage.DocumentContent
		if err := json.Unmarshal(document.Document.Content, &content); err != nil {
			return nil, fmt.Errorf("decode setup document %q: %w", path, err)
		}
		packages[content.PackagePath] = append(packages[content.PackagePath], sourceDocument{
			path: path, contentHash: document.Source.ContentHash, imports: content.Imports,
		})
	}
	return &packageFingerprints{packages: packages, cache: map[string]packageFingerprintResult{}}, nil
}

// of covers the exact source and import graph of every in-module package linked into the test binary.
// This guards init, TestMain, package globals and blank imports that need not appear in the test graph.
func (fingerprints *packageFingerprints) of(packagePath string) packageFingerprintResult {
	if cached, found := fingerprints.cache[packagePath]; found {
		return cached
	}
	if len(fingerprints.packages[packagePath]) == 0 {
		return packageFingerprintResult{}
	}

	included := map[string]bool{}
	pending := []string{packagePath, packagePath + "_test"}
	for len(pending) > 0 {
		current := pending[0]
		pending = pending[1:]
		if included[current] || len(fingerprints.packages[current]) == 0 {
			continue
		}
		included[current] = true
		for _, document := range fingerprints.packages[current] {
			for _, imported := range document.imports {
				if !included[imported.Path] && len(fingerprints.packages[imported.Path]) > 0 {
					pending = append(pending, imported.Path)
				}
			}
		}
	}

	digest := canonicalhash.New("uir-test-package-v2")
	paths := slices.Sorted(maps.Keys(included))
	digest.Count(len(paths))
	for _, current := range paths {
		digest.Text(current)
		documents := slices.Clone(fingerprints.packages[current])
		slices.SortFunc(documents, func(left, right sourceDocument) int { return strings.Compare(left.path, right.path) })
		digest.Count(len(documents))
		for _, document := range documents {
			digest.Text(document.path)
			digest.Text(document.contentHash)
			digest.Count(len(document.imports))
			for _, imported := range document.imports {
				digest.Text(imported.Alias)
				digest.Text(imported.Path)
			}
		}
	}
	result := packageFingerprintResult{fingerprint: digest.Sum(), complete: true}
	fingerprints.cache[packagePath] = result
	return result
}
