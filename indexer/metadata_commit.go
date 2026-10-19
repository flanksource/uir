package indexer

import (
	"context"
	"fmt"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
)

func (indexer *Indexer) extractFromBase(ctx context.Context, options extractOptions) (moduleExtraction, error) {
	var extraction moduleExtraction
	var reused bool
	err := Phase(ctx, "extraction-reuse", func(ctx context.Context) (err error) {
		if extraction, reused, err = indexer.metadataExtraction(ctx, options); reused {
			PhaseCount(ctx, "documents", int64(len(extraction.documents)))
		}
		return err
	})
	if err != nil || reused {
		return extraction, err
	}
	return extractModule(ctx, options)
}

// metadataExtraction reuses a complete extraction when only commit metadata changed. Local modules
// require their recorded semantic context to be revalidated; this path admits only immutable module
// dependencies and no recorded workspace inputs.
func (indexer *Indexer) metadataExtraction(ctx context.Context, options extractOptions) (moduleExtraction, bool, error) {
	base, root := options.baseSnapshot, options.root
	if !sameExtractionInputs(base, root) || root.WorkFile != "" {
		return moduleExtraction{}, false, nil
	}
	modules, err := diskModules(root)
	if err != nil {
		return moduleExtraction{}, false, err
	}
	if len(modules) != 0 {
		return moduleExtraction{}, false, nil
	}
	var siblings int64
	if err := indexer.database.WithContext(ctx).Model(&storage.SnapshotSiblingShape{}).Where("snapshot_id = ?", base.ID).Count(&siblings).Error; err != nil {
		return moduleExtraction{}, false, fmt.Errorf("load workspace inputs of snapshot %s: %w", base.ID, err)
	}
	if siblings != 0 {
		return moduleExtraction{}, false, nil
	}
	extraction := moduleExtraction{
		root: root, baseSnapshotID: base.ID, indexerVersion: IndexerVersion, indexStartedAt: time.Now().UTC(),
		contextHash: base.ContextHash, coverage: base.Coverage, coverageDeterministic: true,
		diagnostics: base.Diagnostics, documents: map[string]extractedDocument{},
	}
	grouped := map[string][]discoveredFile{}
	for _, file := range root.Files {
		grouped[file.PackagePath] = append(grouped[file.PackagePath], file)
	}
	for _, path := range sortedKeys(grouped) {
		stored, found := options.base[path]
		if !found {
			return moduleExtraction{}, false, fmt.Errorf("base snapshot %s has no package %q", base.ID, path)
		}
		extraction.packages = append(extraction.packages, extractedPackage{
			path: path, inputHash: stored.inputHash, exportShapeHash: stored.exportShapeHash,
			coverage: stored.coverage, fileCount: len(grouped[path]), diagnostics: stored.diagnostics,
		})
		if err := reuseBaseDocuments(&extraction, stored, grouped[path]); err != nil {
			return moduleExtraction{}, false, err
		}
	}
	if extraction.implements, err = indexer.storedImplements(ctx, base.ID); err != nil {
		return moduleExtraction{}, false, err
	}
	return extraction, true, nil
}

func sameExtractionInputs(base storage.ModuleSnapshot, root discoveredRoot) bool {
	return base.ID != uuid.Nil && base.Coverage.Complete() && base.ContentSetHash == root.ContentSetHash &&
		base.ConfigurationHash == root.ConfigurationHash && base.DependencySetHash != nil &&
		*base.DependencySetHash == dependencyHash(root.Dependencies)
}

func (indexer *Indexer) storedImplements(ctx context.Context, snapshot uuid.UUID) ([]implementsFact, error) {
	pairs, err := storage.EffectiveImplements(ctx, indexer.database, snapshot, storage.ImplementsFilter{})
	if err != nil {
		return nil, err
	}
	wanted := map[int64]bool{}
	for _, pair := range pairs {
		wanted[pair.Type], wanted[pair.Interface] = true, true
	}
	handles := make([]int64, 0, len(wanted))
	for handle := range wanted {
		handles = append(handles, handle)
	}
	ids, err := storage.SymbolIDs(ctx, indexer.database, handles)
	if err != nil {
		return nil, err
	}
	found := map[implementsFact]bool{}
	for _, pair := range pairs {
		found[implementsFact{typeID: ids[pair.Type], interfaceID: ids[pair.Interface]}] = true
	}
	return sortedFacts(found), nil
}
