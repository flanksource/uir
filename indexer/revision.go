package indexer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
// moving its checkout head or changing the checkout's working tree. A stored snapshot of the commit
// under the same configuration and dependency targets whose coverage is complete, or degraded but
// deterministic, is reused without extracting; otherwise
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
	existing, found, err := indexer.reusableRevision(ctx, &prepared, options.IncludeTests, indexer.indexVersionedDependencies)
	if err != nil || found {
		return reusedRevision(options, existing, err)
	}
	root, location := prepared.root, prepared.location
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
		return reusedRevision(options, identical, err)
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
// found by checking the commit out, discovering its module and linking its versioned dependencies to
// the version snapshots already stored, which indexes and publishes nothing. A version IndexRevision
// would index first makes the dependency set differ, so the probe misses. A caller that finds one
// needs no index run.
func (indexer *Indexer) ReusableRevision(ctx context.Context, options RevisionOptions) (result ModuleResult, found bool, err error) {
	prepared, err := indexer.prepareRevision(ctx, options)
	if err != nil {
		return result, false, err
	}
	defer func() { err = errors.Join(err, prepared.cleanup()) }()
	existing, found, err := indexer.reusableRevision(ctx, &prepared, options.IncludeTests, indexer.storedVersions(newRunMemo()))
	if err != nil || !found {
		return result, false, err
	}
	result, err = revisionResult(options, existing)
	return result, err == nil, err
}

// versionResolver links roots' versioned dependencies to snapshots: indexVersionedDependencies, which
// indexes a version it needs, or linkStoredVersions, which only links stored ones.
type versionResolver func(ctx context.Context, roots []discoveredRoot, includeTests bool) error

// reusableRevision links the prepared root's versioned dependencies through resolve, keeping the linked
// root in prepared, and is then the stored snapshot of the historical commit, of settled coverage,
// whose context is proven without extracting. A historical root loads with GOWORK=off and without
// local replacements, so its context is a function of the commit, the configuration and the module
// versions its dependencies select; the dependency set hash names every selected version and, for one
// linked to an indexed version, that version's target snapshot. A snapshot that recorded a workspace
// sibling package with a shape or source digest read something the commit does not pin, so it is
// proven only by extracting.
func (indexer *Indexer) reusableRevision(ctx context.Context, prepared *preparedRevision, includeTests bool, resolve versionResolver) (storage.ModuleSnapshot, bool, error) {
	root, location := prepared.root, prepared.location
	if !root.Historical {
		return storage.ModuleSnapshot{}, false, fmt.Errorf("reuse revision %s of %q: the root is not historical", root.GitCommit, root.RootKey)
	}
	roots := []discoveredRoot{root}
	if err := Phase(ctx, "versioned-deps", func(ctx context.Context) error {
		PhaseCount(ctx, "dependencies", int64(len(root.Dependencies)))
		return resolve(ctx, roots, includeTests)
	}); err != nil {
		return storage.ModuleSnapshot{}, false, err
	}
	prepared.root, root = roots[0], roots[0]
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

func reusedRevision(options RevisionOptions, snapshot storage.ModuleSnapshot, err error) (ModuleResult, error) {
	if err != nil {
		return ModuleResult{}, err
	}
	return revisionResult(options, snapshot)
}

// revisionResult is the result of reusing a stored snapshot: every one of its files is reused.
func revisionResult(options RevisionOptions, snapshot storage.ModuleSnapshot) (ModuleResult, error) {
	if snapshot.FileCount == nil {
		return ModuleResult{}, fmt.Errorf("reuse snapshot %s of %q: its file count was never recorded", snapshot.ID, options.RootKey)
	}
	files := int(*snapshot.FileCount)
	return ModuleResult{RootKey: options.RootKey, Location: options.Checkout, SnapshotID: snapshot.ID.String(), Files: files, ReusedFiles: files, Unchanged: true}, nil
}

// storedRevision is the newest clean snapshot of root's commit under its configuration and dependency
// set (and version, when it has one). Without an extraction only a complete snapshot, or a degraded one
// whose coverage is deterministic, qualifies, and a miss counts why; with one, the snapshot must have
// the extraction's context hash and coverage, and for a degraded coverage its determinism, so an
// unclassified degraded row never stands in for a classified extraction.
func (indexer *Indexer) storedRevision(ctx context.Context, location storage.ModuleLocation, root discoveredRoot, extraction *moduleExtraction) (existing storage.ModuleSnapshot, found bool, err error) {
	err = Phase(ctx, "reuse-check", func(ctx context.Context) (err error) {
		existing, found, err = indexer.loadStoredRevision(ctx, location, root, extraction)
		var reused int64
		if found {
			reused = 1
		}
		PhaseCount(ctx, "reused", reused)
		if err != nil || found || extraction != nil {
			return err
		}
		miss, err := indexer.revisionMiss(ctx, location, root)
		if err != nil {
			return err
		}
		PhaseCount(ctx, "miss-"+miss, 1)
		return nil
	})
	return existing, found, err
}

// storedRevisionQuery selects the clean snapshots of root's commit, and of its version when it has one.
func (indexer *Indexer) storedRevisionQuery(ctx context.Context, location storage.ModuleLocation, root discoveredRoot) *gorm.DB {
	query := indexer.database.WithContext(ctx).Where("root_id = ? AND revision = ? AND worktree_state = ?", location.RootID, root.GitCommit, storage.WorktreeClean)
	if root.ModuleVersion != "" {
		query = query.Where("module_version = ?", root.ModuleVersion)
	}
	return query
}

// revisionMiss is why no stored snapshot of root's commit is reusable without extracting, from the
// closest one: none when the commit has no clean snapshot, configuration or dependency-set when the
// closest differs in that, else coverage, a degraded coverage not known to be deterministic.
func (indexer *Indexer) revisionMiss(ctx context.Context, location storage.ModuleLocation, root discoveredRoot) (string, error) {
	dependencies := dependencyHash(root.Dependencies)
	var closest storage.ModuleSnapshot
	err := indexer.storedRevisionQuery(ctx, location, root).Select("configuration_hash", "dependency_set_hash").
		Order(clause.OrderBy{Expression: clause.Expr{
			SQL:  "CASE WHEN configuration_hash = ? THEN 0 ELSE 1 END, CASE WHEN dependency_set_hash = ? THEN 0 ELSE 1 END, completed_at DESC",
			Vars: []any{root.ConfigurationHash, dependencies},
		}}).Take(&closest).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "none", nil
	case err != nil:
		return "", fmt.Errorf("find why snapshots of %s are not reusable: %w", root.GitCommit, err)
	case closest.ConfigurationHash != root.ConfigurationHash:
		return "configuration", nil
	case closest.DependencySetHash == nil || *closest.DependencySetHash != dependencies:
		return "dependency-set", nil
	default:
		return "coverage", nil
	}
}

func (indexer *Indexer) loadStoredRevision(ctx context.Context, location storage.ModuleLocation, root discoveredRoot, extraction *moduleExtraction) (storage.ModuleSnapshot, bool, error) {
	query := indexer.storedRevisionQuery(ctx, location, root).
		Where("configuration_hash = ? AND dependency_set_hash = ?", root.ConfigurationHash, dependencyHash(root.Dependencies))
	switch {
	case extraction == nil:
		return newestSettled(query, root.GitCommit)
	case extraction.coverage.Complete():
		query = query.Where("context_hash = ? AND coverage = ?", extraction.contextHash, extraction.coverage)
	default:
		query = query.Where("context_hash = ? AND coverage = ? AND coverage_deterministic = ?", extraction.contextHash, extraction.coverage, extraction.coverageDeterministic)
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

// newestSettled is the newest snapshot query selects whose coverage is settled (CoverageSettled).
func newestSettled(query *gorm.DB, commit string) (storage.ModuleSnapshot, bool, error) {
	var candidates []storage.ModuleSnapshot
	if err := query.Order("completed_at DESC").Find(&candidates).Error; err != nil {
		return storage.ModuleSnapshot{}, false, fmt.Errorf("load clean snapshots of %s: %w", commit, err)
	}
	for _, candidate := range candidates {
		if candidate.CoverageSettled() {
			return candidate, true, nil
		}
	}
	return storage.ModuleSnapshot{}, false, nil
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
