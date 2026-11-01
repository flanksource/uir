package indexer

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// pendingDocument is a document publication will insert or, under force, verify against existing.
type pendingDocument struct {
	document  storage.Document
	extracted extractedDocument
	existing  *storage.Document
}

// documentPlan is what publishDocuments writes: the documents to insert or verify, and the path →
// document ordinal binding of every file whose document already exists.
type documentPlan struct {
	pending  []pendingDocument
	bindings map[string]int64
}

// publishDocuments binds every file to a document and returns the handle of every symbol the new
// documents name, with the snapshot's path → document ordinal membership. A document is keyed by its
// root, path, and content digest, so a file whose rendering an existing document already holds is bound
// to that document, with no insert and no postings, whatever its package's input hash; under force the
// fresh extraction must equal it instead: a different result needs a new indexer version. New
// documents get their symbol rows and handles first, then the next document ordinals, then their
// postings; postings of a document another writer inserted are never written twice. A document that
// fails its checks is degraded in publication's extraction before anything is written.
func publishDocuments(ctx context.Context, database *gorm.DB, publication *snapshotPublication, membership *snapshotMembership, result *ModuleResult) (map[string]int64, error) {
	plan, err := planDocuments(ctx, database, publication, membership, result)
	if err != nil {
		return nil, err
	}
	needed, stored, err := neededSymbols(ctx, database, publication.extraction, plan.pending)
	if err != nil {
		return nil, err
	}
	handles, err := ensureSymbols(ctx, database, publication.extraction.symbols, needed)
	if err != nil {
		return nil, err
	}
	maps.Copy(handles, stored)
	if err := assignDocumentOrdinals(ctx, database, plan.pending); err != nil {
		return nil, err
	}
	for _, next := range plan.pending {
		if plan.bindings[next.document.PathKey], err = publishDocument(ctx, database, next, publication.root.Ordinal, handles); err != nil {
			return nil, err
		}
		result.ParsedFiles++
	}
	membership.bindings = plan.bindings
	return handles, nil
}

// planDocuments validates and digests every rendered document against its revision and the database's
// kind registry, binds each file whose document exists, and returns the documents the publication must
// insert, or verify under force, counting the rest as reused. A rendered document that fails a check
// its writes would repeat is replaced by a degraded one first; the extraction's documents and packages
// are copied before, so a retried transaction starts from the extraction as it was.
func planDocuments(ctx context.Context, database *gorm.DB, publication *snapshotPublication, membership *snapshotMembership, result *ModuleResult) (documentPlan, error) {
	extraction := &publication.extraction
	if extraction.indexerVersion == "" {
		return documentPlan{}, fmt.Errorf("publish module %q: extraction names no indexer version", extraction.root.RootKey)
	}
	kinds, err := storage.LoadSymbolKinds(ctx, database)
	if err != nil {
		return documentPlan{}, err
	}
	extraction.documents, extraction.packages = maps.Clone(extraction.documents), slices.Clone(extraction.packages)
	plan := documentPlan{bindings: map[string]int64{}}
	var rendered []pendingDocument
	for _, file := range extraction.root.Files {
		extracted, found := extraction.documents[file.PathKey]
		if !found {
			return documentPlan{}, fmt.Errorf("no document was extracted for %q", file.PathKey)
		}
		revision := membership.current[file.PathKey]
		if extracted.reused {
			if err := requireReusedDocument(publication.force, revision, extracted, membership.base[file.PathKey]); err != nil {
				return documentPlan{}, err
			}
			plan.bindings[file.PathKey] = extracted.base.Ordinal
			result.ReusedFiles++
			continue
		}
		next, err := extraction.checkedDocument(file, revision, kinds)
		if err != nil {
			return documentPlan{}, err
		}
		rendered = append(rendered, next)
	}
	existing, err := existingDocuments(ctx, database, *publication, membership.base, rendered)
	if err != nil {
		return documentPlan{}, err
	}
	for _, next := range rendered {
		stored, found := existing[next.document.PathKey]
		switch {
		case !found:
			plan.pending = append(plan.pending, next)
		case publication.force:
			next.existing = &stored
			plan.pending = append(plan.pending, next)
		default:
			plan.bindings[next.document.PathKey] = stored.Ordinal
			result.ReusedFiles++
		}
	}
	return plan, nil
}

// checkedDocument is the pending document of a rendered file once it passes renderedDocument's checks,
// or else the first of its degraded documents that does, which then replaces it in the extraction.
func (extraction *moduleExtraction) checkedDocument(file discoveredFile, revision storage.SourceRevision, kinds symbolhandle.Kinds) (pendingDocument, error) {
	extracted := extraction.documents[file.PathKey]
	document, err := renderedDocument(extraction.indexerVersion, file, revision, extracted, kinds)
	failure := asDocumentError(err)
	if failure == nil {
		return pendingDocument{document: document, extracted: extracted}, err
	}
	packageIndex := slices.IndexFunc(extraction.packages, func(extracted extractedPackage) bool { return extracted.path == file.PackagePath })
	if packageIndex < 0 {
		return pendingDocument{}, fmt.Errorf("degrade document for %q: no package %q was extracted", file.PathKey, file.PackagePath)
	}
	diagnostics := fileDiagnostics(extraction.packages[packageIndex].diagnostics, file.PathKey)
	degraded, err := degradeDocument(file, extracted, diagnostics, func(candidate extractedDocument) (err error) {
		document, err = renderedDocument(extraction.indexerVersion, file, revision, candidate, kinds)
		return err
	})
	if err != nil {
		return pendingDocument{}, fmt.Errorf("degrade document for %q after %v: %v", file.PathKey, failure, err)
	}
	extraction.documents[file.PathKey] = degraded.document
	extraction.packages[packageIndex].degrade(failure, degraded.note)
	if coverageStrength[extraction.coverage] > coverageStrength[storage.CoveragePartial] {
		extraction.coverage = storage.CoveragePartial
	}
	extraction.diagnostics, err = snapshotDiagnostics(extraction.packages)
	return pendingDocument{document: document, extracted: degraded.document}, err
}

// renderedDocument is the document row of a rendered file, validated against its revision, checked
// for the declaration_metrics rows its write derives, and keyed by its content digest. Each of these
// failures is the file's own, a DocumentError. The row gets its id once it passes, so a failure reads
// the same on every run.
func renderedDocument(indexerVersion string, file discoveredFile, revision storage.SourceRevision, extracted extractedDocument, kinds symbolhandle.Kinds) (storage.Document, error) {
	document := storage.Document{
		RootID: revision.RootID, PathKey: file.PathKey, SourceRevisionID: revision.ID,
		PackagePath: file.PackagePath, InputHash: extracted.inputHash, IndexerVersion: indexerVersion,
		Coverage: extracted.coverage, SymbolCount: extracted.symbolCount, OccurrenceCount: extracted.occurrenceCount,
		Content: extracted.content,
	}
	content, err := storage.DecodeDocument(document, revision, storage.WithKinds(kinds))
	if err != nil {
		return storage.Document{}, &DocumentError{Path: file.PathKey, Err: fmt.Errorf("validate extracted document: %w", err)}
	}
	if err := requireMetricKeys(content); err != nil {
		return storage.Document{}, &DocumentError{Path: file.PathKey, Err: err}
	}
	digest, err := storage.DocumentDigest(document)
	if err != nil {
		return storage.Document{}, &DocumentError{Path: file.PathKey, Err: err}
	}
	document.ID, document.ContentDigest = uuid.New(), &digest
	return document, nil
}

// requireReusedDocument checks that the document extraction reused from the base, rather than
// rendering it, is the one the base binds to the path at the path's current revision. Force re-verifies
// every document, so it cannot take one it did not render.
func requireReusedDocument(force bool, revision storage.SourceRevision, extracted extractedDocument, bound storage.ActiveDocument) error {
	if force {
		return fmt.Errorf("document for %q was reused from the base, but a forced publication re-verifies every document", revision.PathKey)
	}
	if bound.Document.ID != extracted.base.ID || bound.Source.ID != revision.ID {
		return fmt.Errorf("document %s for %q was reused from the base, but the base binds document %s at revision %s and the snapshot has revision %s",
			extracted.base.ID, revision.PathKey, bound.Document.ID, bound.Source.ID, revision.ID)
	}
	return nil
}

// existingDocuments finds, by path, the stored document under each rendered document's key: the
// base's bound document when its digest matches, else a paired-key lookup, with content under force.
// Under force a rendered document must also equal the base's for a package whose input hash is the base's.
func existingDocuments(ctx context.Context, database *gorm.DB, publication snapshotPublication, base map[string]storage.ActiveDocument, rendered []pendingDocument) (map[string]storage.Document, error) {
	if publication.force {
		if err := requireDeterministic(ctx, database, publication.base.snapshot, base, rendered); err != nil {
			return nil, err
		}
	}
	existing := map[string]storage.Document{}
	var keys []storage.DocumentKey
	for _, next := range rendered {
		key := storage.DocumentKey{PathKey: next.document.PathKey, Digest: *next.document.ContentDigest}
		if bound, found := base[key.PathKey]; found && !publication.force && bound.Document.ContentDigest != nil && *bound.Document.ContentDigest == key.Digest {
			existing[key.PathKey] = bound.Document
			continue
		}
		keys = append(keys, key)
	}
	stored, err := storage.DocumentsByKey(ctx, database, publication.root.ID, keys, publication.force)
	if err != nil {
		return nil, err
	}
	for _, next := range rendered {
		key := storage.DocumentKey{PathKey: next.document.PathKey, Digest: *next.document.ContentDigest}
		if document, found := stored[key]; found {
			if document.SourceRevisionID != next.document.SourceRevisionID {
				return nil, fmt.Errorf("document %s for %q describes revision %s, expected %s", document.ID, key.PathKey, document.SourceRevisionID, next.document.SourceRevisionID)
			}
			existing[key.PathKey] = document
		}
	}
	return existing, nil
}

// requireDeterministic fails when a forced re-extraction renders a file differently from the document
// its base binds although the file's package has the base's input hash: the extractor's output changed
// without an indexer version change.
func requireDeterministic(ctx context.Context, database *gorm.DB, baseSnapshot storage.ModuleSnapshot, base map[string]storage.ActiveDocument, rendered []pendingDocument) error {
	if baseSnapshot.ID == uuid.Nil {
		return nil
	}
	var coverage []storage.PackageCoverage
	if err := database.WithContext(ctx).Select("package_path", "input_hash").Where("snapshot_id = ?", baseSnapshot.ID).Find(&coverage).Error; err != nil {
		return fmt.Errorf("load package input hashes of base snapshot %s: %w", baseSnapshot.ID, err)
	}
	hashes := make(map[string]string, len(coverage))
	for _, row := range coverage {
		hashes[row.PackagePath] = row.InputHash
	}
	for _, next := range rendered {
		document := next.document
		bound, found := base[document.PathKey]
		if !found || bound.Document.ContentDigest == nil || hashes[document.PackagePath] != document.InputHash || *bound.Document.ContentDigest == *document.ContentDigest {
			continue
		}
		return fmt.Errorf("document for %q under input hash %s changed without an indexer version change", document.PathKey, document.InputHash)
	}
	return nil
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

// publishDocument inserts a new document with its postings, or verifies the stored one under its key
// (a forced publication's, or one a concurrent publisher inserted first), and returns the ordinal of the
// document the path binds.
func publishDocument(ctx context.Context, database *gorm.DB, next pendingDocument, rootOrdinal int32, handles map[string]int64) (int64, error) {
	document := next.document
	if next.existing == nil {
		inserted, err := storage.CreateDocument(ctx, database, &document)
		if err != nil {
			return 0, err
		}
		if inserted {
			if err := createPostings(ctx, database, document, rootOrdinal, next.extracted.postings, handles); err != nil {
				return 0, err
			}
			return document.Ordinal, publishDeclarationMetrics(ctx, database, metricPublication{document: document, handles: handles})
		}
		key := storage.DocumentKey{PathKey: document.PathKey, Digest: *document.ContentDigest}
		stored, err := storage.DocumentsByKey(ctx, database, document.RootID, []storage.DocumentKey{key}, true)
		if err != nil {
			return 0, err
		}
		found, ok := stored[key]
		if !ok {
			return 0, fmt.Errorf("load the document another publisher stored for %q: not found under digest %s", document.PathKey, key.Digest)
		}
		next.existing = &found
	}
	same, err := sameJSON(next.existing.Content, document.Content)
	if err != nil {
		return 0, fmt.Errorf("compare document for %q: %w", document.PathKey, err)
	}
	if !same || next.existing.Coverage != document.Coverage {
		return 0, fmt.Errorf("document for %q under input hash %s changed without an indexer version change", document.PathKey, document.InputHash)
	}
	return next.existing.Ordinal, publishDeclarationMetrics(ctx, database, metricPublication{document: *next.existing, handles: handles})
}
