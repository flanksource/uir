package indexer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

// RevisionOptions selects one commit of a registered checkout. Reason is historical for a commit
// indexed on demand, or versioned-dependency for a selected dependency version.
type RevisionOptions struct {
	RootKey      string
	Checkout     string
	Commit       string
	Version      string
	IncludeTests bool
	Reason       storage.SnapshotReason
}

// IndexRevision publishes a clean historical snapshot for an already registered module without
// moving its checkout head or changing the checkout's working tree. A stored, fully indexed snapshot
// of a commit without dependencies is reused without extracting; otherwise the commit is extracted,
// and a stored snapshot identical to the extraction, degraded or not, is reused instead of publishing
// a duplicate.
func (indexer *Indexer) IndexRevision(ctx context.Context, options RevisionOptions) (result ModuleResult, err error) {
	if options.Reason != storage.ReasonHistorical && options.Reason != storage.ReasonVersionedDependency {
		return result, fmt.Errorf("index revision %s of %q: reason %q is not historical or versioned-dependency", options.Commit, options.RootKey, options.Reason)
	}
	prepared, err := indexer.prepareRevision(ctx, options)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, prepared.cleanup()) }()
	root, location := prepared.root, prepared.location
	selectedRoots := []discoveredRoot{root}
	if err := indexer.indexVersionedDependencies(ctx, selectedRoots, options.IncludeTests); err != nil {
		return result, err
	}
	root = selectedRoots[0]
	if existing, found, err := indexer.reusableRevision(ctx, location, root); err != nil || found {
		return revisionResult(options, existing), err
	}
	extraction, err := extractModule(ctx, indexer.loadPackages, root, options.IncludeTests)
	if err != nil {
		return result, fmt.Errorf("extract historical module %q: %w", root.RootKey, err)
	}
	identical, found, err := indexer.storedRevision(ctx, location, root, &extraction)
	if err != nil || found {
		return revisionResult(options, identical), err
	}
	return indexer.publishRevision(ctx, location, extraction, options.Reason)
}

// ReusableRevision is the stored snapshot IndexRevision would reuse for options without extracting,
// found by checking the commit out and discovering its module, which indexes nothing. A caller that
// finds one needs no index run.
func (indexer *Indexer) ReusableRevision(ctx context.Context, options RevisionOptions) (result ModuleResult, found bool, err error) {
	prepared, err := indexer.prepareRevision(ctx, options)
	if err != nil {
		return result, false, err
	}
	defer func() { err = errors.Join(err, prepared.cleanup()) }()
	existing, found, err := indexer.reusableRevision(ctx, prepared.location, prepared.root)
	if err != nil || !found {
		return result, false, err
	}
	return revisionResult(options, existing), true, nil
}

// reusableRevision is the fully indexed stored snapshot of a commit without dependencies. A commit
// with dependencies is never reused without extracting.
func (indexer *Indexer) reusableRevision(ctx context.Context, location storage.ModuleLocation, root discoveredRoot) (storage.ModuleSnapshot, bool, error) {
	if len(root.Dependencies) != 0 {
		return storage.ModuleSnapshot{}, false, nil
	}
	return indexer.storedRevision(ctx, location, root, nil)
}

func revisionResult(options RevisionOptions, snapshot storage.ModuleSnapshot) ModuleResult {
	return ModuleResult{RootKey: options.RootKey, Location: options.Checkout, SnapshotID: snapshot.ID.String(), Unchanged: true}
}

// storedRevision is the newest clean snapshot of root's commit under its configuration and dependency
// set (and version, when it has one). Without an extraction only a fully indexed snapshot qualifies;
// with one, the snapshot must have the extraction's context hash and coverage.
func (indexer *Indexer) storedRevision(ctx context.Context, location storage.ModuleLocation, root discoveredRoot, extraction *moduleExtraction) (storage.ModuleSnapshot, bool, error) {
	query := indexer.database.WithContext(ctx).Where("root_id = ? AND revision = ? AND configuration_hash = ? AND worktree_state = ? AND dependency_set_hash = ?",
		location.RootID, root.GitCommit, root.ConfigurationHash, storage.WorktreeClean, dependencyHash(root.Dependencies))
	if root.ModuleVersion != "" {
		query = query.Where("module_version = ?", root.ModuleVersion)
	}
	if extraction == nil {
		query = query.Where("coverage = ?", storage.CoverageIndexed)
	} else {
		query = query.Where("context_hash = ? AND coverage = ?", extraction.contextHash, extraction.coverage)
	}
	var existing storage.ModuleSnapshot
	err := query.Order("completed_at DESC").Take(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return storage.ModuleSnapshot{}, false, nil
	}
	if err != nil {
		return storage.ModuleSnapshot{}, false, fmt.Errorf("load clean snapshot of %s: %w", root.GitCommit, err)
	}
	return existing, true, nil
}

func (indexer *Indexer) publishRevision(ctx context.Context, location storage.ModuleLocation, extraction moduleExtraction, reason storage.SnapshotReason) (result ModuleResult, err error) {
	root := extraction.root
	err = storage.RetryAllocationConflicts(ctx, indexer.database, func(transaction *gorm.DB) error {
		var stored storage.ModuleRoot
		if loadErr := transaction.Where("id = ?", location.RootID).Take(&stored).Error; loadErr != nil {
			return fmt.Errorf("load root of historical module: %w", loadErr)
		}
		base, loadErr := loadModuleBase(ctx, transaction, stored, location)
		if loadErr != nil {
			return loadErr
		}
		result = ModuleResult{RootKey: root.RootKey, Location: location.CanonicalPath, Files: len(root.Files)}
		snapshot, publishErr := publishSnapshot(ctx, transaction, snapshotPublication{
			root: stored, location: location, base: base, extraction: extraction, startedAt: time.Now().UTC(), reason: reason, preserveHead: true,
		}, &result)
		if publishErr != nil {
			return publishErr
		}
		result.SnapshotID = snapshot.ID.String()
		return nil
	})
	return result, err
}
