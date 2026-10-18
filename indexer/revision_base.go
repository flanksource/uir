package indexer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// revisionAncestorScan bounds how many commits the base search walks back from a requested one.
	revisionAncestorScan  = 1024
	revisionAncestorBatch = 128
)

// basePackage is one package of a base snapshot: the input hash its package coverage records, and its
// active documents by path, without content.
type basePackage struct {
	inputHash string
	documents map[string]storage.Document
}

// revisionBase is the snapshot a historical commit's snapshot is stored as a delta on: the newest
// snapshot of the nearest indexed ancestor (the commit itself included) under the same configuration
// and dependency set, else the location's head or the primary's, else none. Ancestors are walked in
// git rev-list order, at most revisionAncestorScan commits back.
func (indexer *Indexer) revisionBase(ctx context.Context, location storage.ModuleLocation, root discoveredRoot) (storage.ModuleSnapshot, error) {
	listed, err := revisionGit(ctx, root.LocalPath, "rev-list", fmt.Sprintf("--max-count=%d", revisionAncestorScan), "--end-of-options", root.GitCommit)
	if err != nil {
		return storage.ModuleSnapshot{}, fmt.Errorf("list ancestors of %s: %w", root.GitCommit, err)
	}
	ancestors := strings.Fields(listed)
	for start := 0; start < len(ancestors); start += revisionAncestorBatch {
		batch := ancestors[start:min(start+revisionAncestorBatch, len(ancestors))]
		snapshot, found, err := indexer.nearestIndexed(ctx, location.RootID, root, batch)
		if err != nil || found {
			return snapshot, err
		}
	}
	head, err := loadModuleBase(ctx, indexer.database, storage.ModuleRoot{ID: location.RootID}, location)
	return head.snapshot, err
}

// nearestIndexed is the newest clean snapshot of the earliest of commits, in their order, indexed for
// the root under its configuration and dependency set.
func (indexer *Indexer) nearestIndexed(ctx context.Context, rootID uuid.UUID, root discoveredRoot, commits []string) (storage.ModuleSnapshot, bool, error) {
	var candidates []storage.ModuleSnapshot
	if err := indexer.database.WithContext(ctx).
		Where("root_id = ? AND configuration_hash = ? AND dependency_set_hash = ? AND worktree_state = ? AND git_commit IN ?",
			rootID, root.ConfigurationHash, dependencyHash(root.Dependencies), storage.WorktreeClean, commits).
		Order("completed_at DESC").Find(&candidates).Error; err != nil {
		return storage.ModuleSnapshot{}, false, fmt.Errorf("load indexed ancestors of %s: %w", root.GitCommit, err)
	}
	for _, commit := range commits {
		if index := slices.IndexFunc(candidates, func(snapshot storage.ModuleSnapshot) bool { return snapshot.GitCommit == commit }); index >= 0 {
			return candidates[index], true, nil
		}
	}
	return storage.ModuleSnapshot{}, false, nil
}

// basePackages is the base snapshot's packages with their active documents; none without a base.
func basePackages(ctx context.Context, database *gorm.DB, base storage.ModuleSnapshot) (map[string]basePackage, error) {
	packages := map[string]basePackage{}
	if base.ID == uuid.Nil {
		return packages, nil
	}
	active, err := storage.ActiveDocuments(ctx, database, base.ID, storage.ActiveDocumentOptions{})
	if err != nil {
		return nil, err
	}
	var coverage []storage.PackageCoverage
	if err := database.WithContext(ctx).Select("package_path", "input_hash").Where("snapshot_id = ?", base.ID).Find(&coverage).Error; err != nil {
		return nil, fmt.Errorf("load package input hashes of base snapshot %s: %w", base.ID, err)
	}
	for _, row := range coverage {
		packages[row.PackagePath] = basePackage{inputHash: row.InputHash, documents: map[string]storage.Document{}}
	}
	for path, entry := range active {
		known, found := packages[entry.Source.PackagePath]
		if !found {
			return nil, fmt.Errorf("base snapshot %s has no package coverage for %q of %q", base.ID, entry.Source.PackagePath, path)
		}
		known.documents[path] = entry.Document
	}
	return packages, nil
}

// pinnedBase is the location's locked head, as loadModuleBase reads it inside the publishing
// transaction, with the base the extraction was made against: a historical snapshot never moves the
// head, so its storage base stays the one whose documents the extraction reused.
func pinnedBase(ctx context.Context, transaction *gorm.DB, root storage.ModuleRoot, location storage.ModuleLocation, baseID uuid.UUID) (moduleBase, error) {
	base, err := loadModuleBase(ctx, transaction, root, location)
	if err != nil {
		return moduleBase{}, err
	}
	base.snapshot = storage.ModuleSnapshot{}
	if baseID == uuid.Nil {
		return base, nil
	}
	err = transaction.WithContext(ctx).Where("id = ? AND root_id = ?", baseID, root.ID).Take(&base.snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return moduleBase{}, fmt.Errorf("base snapshot %s of root %s was removed: %w", baseID, root.ID, ErrIndexInputsChanged)
	}
	if err != nil {
		return moduleBase{}, fmt.Errorf("load base snapshot %s: %w", baseID, err)
	}
	return base, nil
}
