package indexer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RevisionOptions selects one commit of a registered checkout. Reason is historical for a commit
// indexed on demand, or versioned-dependency for a selected dependency version.
type RevisionOptions struct {
	RootKey          string
	Checkout         string
	Commit           string
	Version          string
	IncludeTests     bool
	Reason           storage.SnapshotReason
	ScratchDirectory string
}

// IndexRevision publishes a clean historical snapshot for an already registered module without
// moving its checkout head or changing the checkout's working tree. A complete stored snapshot of the
// commit under the same configuration and dependency targets is reused without extracting; otherwise
// the commit is extracted against its base, and a stored snapshot identical to the extraction,
// degraded or not, is reused instead of publishing a duplicate.
func (indexer *Indexer) IndexRevision(ctx context.Context, options RevisionOptions) (result ModuleResult, err error) {
	if options.Reason != storage.ReasonHistorical && options.Reason != storage.ReasonVersionedDependency {
		return result, fmt.Errorf("index revision %s of %q: reason %q is not historical or versioned-dependency", options.Commit, options.RootKey, options.Reason)
	}
	prepared, err := indexer.prepareRevision(ctx, options)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, prepared.cleanup()) }()
	return indexer.indexPreparedRevision(ctx, prepared, options)
}

// indexPreparedRevision is IndexRevision of the commit prepareRevision already checked out for
// options; the caller still owns the worktree's cleanup.
func (indexer *Indexer) indexPreparedRevision(ctx context.Context, prepared preparedRevision, options RevisionOptions) (result ModuleResult, err error) {
	root, location := prepared.root, prepared.location
	selectedRoots := []discoveredRoot{root}
	if err := Phase(ctx, "versioned-deps", func(ctx context.Context) error {
		PhaseCount(ctx, "dependencies", int64(len(root.Dependencies)))
		return indexer.indexVersionedDependencies(ctx, selectedRoots, options.IncludeTests)
	}); err != nil {
		return result, err
	}
	root = selectedRoots[0]
	if existing, found, err := indexer.reusableRevision(ctx, location, root); err != nil || found {
		return revisionResult(options, existing), err
	}
	var base storage.ModuleSnapshot
	var packages map[string]basePackage
	if err := Phase(ctx, "revision-base", func(ctx context.Context) (err error) {
		if base, err = indexer.revisionBase(ctx, location, root); err != nil {
			return err
		}
		packages, err = basePackages(ctx, indexer.database, base)
		return err
	}); err != nil {
		return result, err
	}
	extraction, err := extractModule(ctx, extractOptions{loadPackages: indexer.loadPackages, root: root, includeTests: options.IncludeTests, base: packages})
	if err != nil {
		return result, fmt.Errorf("extract historical module %q: %w", root.RootKey, err)
	}
	identical, found, err := indexer.storedRevision(ctx, location, root, &extraction)
	if err != nil || found {
		return revisionResult(options, identical), err
	}
	return indexer.publishRevision(ctx, revisionPublication{location: location, extraction: extraction, reason: options.Reason, baseID: base.ID})
}

// revisionPublication is one historical snapshot to publish on the base its extraction reused.
type revisionPublication struct {
	location   storage.ModuleLocation
	extraction moduleExtraction
	reason     storage.SnapshotReason
	baseID     uuid.UUID
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

// reusableRevision is the complete stored snapshot of a historical commit whose context is proven
// without extracting. A historical root loads with GOWORK=off and without local replacements, so its
// context is a function of the commit, the configuration and the module versions its dependencies
// select; the dependency set hash names every selected version and, for one linked to an indexed
// version, that version's target snapshot. A snapshot that recorded a workspace sibling package with a
// shape or source digest read something the commit does not pin, so it is proven only by extracting.
func (indexer *Indexer) reusableRevision(ctx context.Context, location storage.ModuleLocation, root discoveredRoot) (storage.ModuleSnapshot, bool, error) {
	if !root.Historical {
		return storage.ModuleSnapshot{}, false, fmt.Errorf("reuse revision %s of %q: the root is not historical", root.GitCommit, root.RootKey)
	}
	existing, found, err := indexer.storedRevision(ctx, location, root, nil)
	if err != nil || !found {
		return storage.ModuleSnapshot{}, false, err
	}
	var siblings int64
	if err := indexer.database.WithContext(ctx).Model(&storage.SnapshotSiblingShape{}).
		Where("snapshot_id = ? AND (export_shape_hash IS NOT NULL OR source_digest IS NOT NULL)", existing.ID).Count(&siblings).Error; err != nil {
		return storage.ModuleSnapshot{}, false, fmt.Errorf("load sibling shapes of snapshot %s: %w", existing.ID, err)
	}
	if siblings != 0 {
		PhaseCount(ctx, "sibling-recorded", 1)
		return storage.ModuleSnapshot{}, false, nil
	}
	return existing, true, nil
}

func revisionResult(options RevisionOptions, snapshot storage.ModuleSnapshot) ModuleResult {
	return ModuleResult{RootKey: options.RootKey, Location: options.Checkout, SnapshotID: snapshot.ID.String(), Unchanged: true}
}

// storedRevision is the newest clean snapshot of root's commit under its configuration and dependency
// set (and version, when it has one). Without an extraction only a complete snapshot qualifies;
// with one, the snapshot must have the extraction's context hash and coverage.
func (indexer *Indexer) storedRevision(ctx context.Context, location storage.ModuleLocation, root discoveredRoot, extraction *moduleExtraction) (existing storage.ModuleSnapshot, found bool, err error) {
	err = Phase(ctx, "reuse-check", func(ctx context.Context) (err error) {
		existing, found, err = indexer.loadStoredRevision(ctx, location, root, extraction)
		var reused int64
		if found {
			reused = 1
		}
		PhaseCount(ctx, "reused", reused)
		return err
	})
	return existing, found, err
}

func (indexer *Indexer) loadStoredRevision(ctx context.Context, location storage.ModuleLocation, root discoveredRoot, extraction *moduleExtraction) (storage.ModuleSnapshot, bool, error) {
	query := indexer.database.WithContext(ctx).Where("root_id = ? AND revision = ? AND configuration_hash = ? AND worktree_state = ? AND dependency_set_hash = ?",
		location.RootID, root.GitCommit, root.ConfigurationHash, storage.WorktreeClean, dependencyHash(root.Dependencies))
	if root.ModuleVersion != "" {
		query = query.Where("module_version = ?", root.ModuleVersion)
	}
	if extraction == nil {
		query = query.Where("coverage IN ?", storage.CompleteCoverages)
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

func (indexer *Indexer) publishRevision(ctx context.Context, publication revisionPublication) (result ModuleResult, err error) {
	root, location := publication.extraction.root, publication.location
	err = Phase(ctx, "publish", func(ctx context.Context) error {
		return storage.RetryAllocationConflicts(ctx, indexer.database, func(transaction *gorm.DB) error {
			var stored storage.ModuleRoot
			if loadErr := transaction.Where("id = ?", location.RootID).Take(&stored).Error; loadErr != nil {
				return fmt.Errorf("load root of historical module: %w", loadErr)
			}
			base, loadErr := pinnedBase(ctx, transaction, stored, location, publication.baseID)
			if loadErr != nil {
				return loadErr
			}
			result = ModuleResult{RootKey: root.RootKey, Location: location.CanonicalPath, Files: len(root.Files)}
			snapshot, publishErr := publishSnapshot(ctx, transaction, snapshotPublication{
				root: stored, location: location, base: base, extraction: publication.extraction, startedAt: time.Now().UTC(), reason: publication.reason, preserveHead: true,
			}, &result)
			if publishErr != nil {
				return publishErr
			}
			result.SnapshotID = snapshot.ID.String()
			return nil
		})
	})
	return result, err
}
