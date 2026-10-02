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

// ModuleOptions selects the modules to index. Reason is why, recorded on every snapshot published:
// add, reindex, or refactor from a caller, local-dependency for a dependency indexed on the way.
type ModuleOptions struct {
	Path          string
	ExactLocation string
	IncludeTests  bool
	Force         bool
	ExistingOnly  bool
	Reason        storage.SnapshotReason
}

// headReasons are the reasons a snapshot that advances a checkout's head is published for.
var headReasons = map[storage.SnapshotReason]bool{
	storage.ReasonAdd: true, storage.ReasonReindex: true, storage.ReasonRefactor: true, storage.ReasonLocalDependency: true,
}

// publicationReasons are every reason a snapshot is published for; unknown marks only older rows.
var publicationReasons = map[storage.SnapshotReason]bool{
	storage.ReasonAdd: true, storage.ReasonReindex: true, storage.ReasonRefactor: true, storage.ReasonLocalDependency: true,
	storage.ReasonVersionedDependency: true, storage.ReasonHistorical: true, storage.ReasonDependencyCycle: true,
}

type ModuleResult struct {
	RootKey     string `json:"root_key"`
	Location    string `json:"location"`
	SnapshotID  string `json:"snapshot_id"`
	HeadVersion int64  `json:"head_version"`
	Files       int    `json:"files"`
	ParsedFiles int    `json:"parsed_files"`
	ReusedFiles int    `json:"reused_files"`
	Unchanged   bool   `json:"unchanged"`
	// Error is why a module of a task run failed; its other counts are then zero.
	Error string `json:"error,omitempty"`
}

func (indexer *Indexer) IndexModules(ctx context.Context, options ModuleOptions) ([]ModuleResult, error) {
	if indexer == nil || indexer.database == nil {
		return nil, errors.New("UIR index database is required")
	}
	if !headReasons[options.Reason] {
		return nil, fmt.Errorf("index modules under %q: reason %q is not one of add, reindex, refactor, local-dependency", options.Path, options.Reason)
	}
	roots, err := discoverRequestedModules(ctx, options)
	if err != nil {
		return nil, err
	}
	if options.ExistingOnly {
		for _, root := range roots {
			if err := requireRegisteredLocation(ctx, indexer.database, root); err != nil {
				return nil, err
			}
		}
	}
	for i := range roots {
		roots[i].Dependencies, err = readDependencies(ctx, roots[i])
		if err != nil {
			return nil, fmt.Errorf("read dependencies of module %q: %w", roots[i].RootKey, err)
		}
	}
	if ctx.Value(activeModulePaths{}) == nil {
		graph, err := discoverLocalDependencyGraph(ctx, roots, options.IncludeTests)
		if err != nil {
			return nil, err
		}
		if graph.cyclic() {
			return indexer.indexLocalCycleGraph(ctx, graph, len(roots), options)
		}
	}
	ctx = withActivePaths(ctx, roots)
	if err := indexer.indexLocalDependencies(ctx, roots, options.IncludeTests); err != nil {
		return nil, err
	}
	if err := indexer.indexVersionedDependencies(ctx, roots, options.IncludeTests); err != nil {
		return nil, err
	}
	extractions := make([]moduleExtraction, 0, len(roots))
	for _, root := range roots {
		head, reusable, err := reusableHead(ctx, indexer.database, root, options.Force)
		if err != nil {
			return nil, fmt.Errorf("index module %q at %q: %w", root.RootKey, root.LocalPath, err)
		}
		if reusable {
			extractions = append(extractions, moduleExtraction{root: root, reusedHead: head})
			continue
		}
		extraction, err := extractModule(ctx, indexer.loadPackages, root, options.IncludeTests)
		if err != nil {
			return nil, fmt.Errorf("index module %q at %q: %w", root.RootKey, root.LocalPath, err)
		}
		extractions = append(extractions, extraction)
	}
	var results []ModuleResult
	err = storage.RetryAllocationConflicts(ctx, indexer.database, func(transaction *gorm.DB) error {
		if err := verifyIndexInputs(ctx, transaction, roots, options.IncludeTests); err != nil {
			return err
		}
		results = make([]ModuleResult, 0, len(roots))
		locations := make(map[string]storage.ModuleLocation, len(roots))
		for _, extraction := range extractions {
			result, location, indexErr := indexModule(ctx, transaction, extraction, locations, options)
			if indexErr != nil {
				return fmt.Errorf("index module %q at %q: %w", extraction.root.RootKey, extraction.root.LocalPath, indexErr)
			}
			locations[extraction.root.LocalPath] = location
			results = append(results, result)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

func discoverRequestedModules(ctx context.Context, options ModuleOptions) ([]discoveredRoot, error) {
	if options.ExactLocation == "" {
		return discoverModules(ctx, options.Path, options.IncludeTests)
	}
	root, err := discoverModule(ctx, options.Path, options.ExactLocation, options.IncludeTests)
	if err != nil {
		return nil, err
	}
	return []discoveredRoot{root}, nil
}

// indexModule publishes one extracted root inside the caller's transaction, or reports it unchanged
// when its revision, content set, configuration, and context all equal the base snapshot's. A root
// that was not extracted because its head was reusable must still find that head in the transaction.
func indexModule(ctx context.Context, database *gorm.DB, extraction moduleExtraction, locations map[string]storage.ModuleLocation, options ModuleOptions) (ModuleResult, storage.ModuleLocation, error) {
	startedAt := time.Now().UTC()
	discovered := extraction.root
	root, location, err := ensureModuleLocation(ctx, database, discovered, locations)
	if err != nil {
		return ModuleResult{}, storage.ModuleLocation{}, err
	}
	result := ModuleResult{RootKey: root.RootKey, Location: location.CanonicalPath, Files: len(discovered.Files)}
	base, err := loadModuleBase(ctx, database, root, location)
	if err != nil {
		return ModuleResult{}, storage.ModuleLocation{}, err
	}
	unchanged := base.hasHead && !options.Force && base.snapshot.Revision == discovered.Revision &&
		base.snapshot.ContentSetHash == discovered.ContentSetHash &&
		base.snapshot.ConfigurationHash == discovered.ConfigurationHash && base.snapshot.ContextHash == extraction.contextHash &&
		base.snapshot.DependencySetHash != nil && *base.snapshot.DependencySetHash == dependencyHash(discovered.Dependencies)
	if extraction.reusedHead != uuid.Nil {
		if !base.hasHead || base.snapshot.ID != extraction.reusedHead {
			return ModuleResult{}, storage.ModuleLocation{}, fmt.Errorf("location %s head moved from snapshot %s: %w", location.ID, extraction.reusedHead, ErrIndexInputsChanged)
		}
		unchanged = true
	}
	if unchanged {
		result.SnapshotID, result.HeadVersion, result.ReusedFiles, result.Unchanged = base.snapshot.ID.String(), base.head.Version, len(discovered.Files), true
		return result, location, nil
	}
	snapshot, err := publishSnapshot(ctx, database, snapshotPublication{
		root: root, location: location, base: base, extraction: extraction, force: options.Force, startedAt: startedAt, reason: options.Reason,
	}, &result)
	if err != nil {
		return ModuleResult{}, storage.ModuleLocation{}, err
	}
	result.SnapshotID = snapshot.ID.String()
	return result, location, nil
}

type snapshotPublication struct {
	root         storage.ModuleRoot
	location     storage.ModuleLocation
	base         moduleBase
	extraction   moduleExtraction
	force        bool
	startedAt    time.Time
	reason       storage.SnapshotReason
	preserveHead bool
	snapshotID   uuid.UUID
	pendingEdges *[]storage.SnapshotDependency
}

// publishSnapshot writes source revisions, symbols, documents, and postings, then the snapshot row
// with its source deltas, package coverage, and symbol deltas, and finally advances the location head
// with a compare-and-swap; the caller's transaction makes the whole publication atomic. Extraction is
// already complete, so the transaction only writes rows.
func publishSnapshot(ctx context.Context, database *gorm.DB, publication snapshotPublication, result *ModuleResult) (storage.ModuleSnapshot, error) {
	if !publicationReasons[publication.reason] {
		return storage.ModuleSnapshot{}, fmt.Errorf("publish module %q: reason %q is not a publication reason", publication.root.RootKey, publication.reason)
	}
	if publication.extraction.indexStartedAt.IsZero() {
		return storage.ModuleSnapshot{}, fmt.Errorf("publish module %q: extraction has no start time", publication.root.RootKey)
	}
	previous := map[string]storage.SourceRevision{}
	if publication.base.snapshot.ID != uuid.Nil {
		var err error
		if previous, err = storage.EffectiveSources(ctx, database, publication.base.snapshot.ID); err != nil {
			return storage.ModuleSnapshot{}, err
		}
	}
	current, err := sourceRevisions(ctx, database, publication.root.ID, publication.extraction.root.Files, previous)
	if err != nil {
		return storage.ModuleSnapshot{}, err
	}
	handles, err := publishDocuments(ctx, database, publication, current, result)
	if err != nil {
		return storage.ModuleSnapshot{}, err
	}
	snapshot := publication.snapshot()
	snapshot.TaskRunID = taskRunID(ctx)
	if err := storage.CreateSnapshot(ctx, database, &snapshot); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	if err := publishBlobsAndEdges(ctx, database, publication, snapshot); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	if err := createSourceDeltas(ctx, database, snapshot, previous, current); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	if err := createPackageCoverage(ctx, database, snapshot, publication.extraction.packages); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	if err := createSymbolDeltas(ctx, database, snapshot, publication, previous, handles); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	if _, err := storage.RecordSnapshotStats(ctx, database, snapshot.ID); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	if publication.preserveHead {
		result.HeadVersion = publication.base.head.Version
	} else {
		if result.HeadVersion, err = advanceHead(ctx, database, publication.base, snapshot); err != nil {
			return storage.ModuleSnapshot{}, err
		}
	}
	return snapshot, nil
}

// publishBlobsAndEdges stores the bytes of a snapshot that is not a clean commit, and its dependency
// edges, or queues the edges when the caller publishes them once every snapshot of a cycle exists.
func publishBlobsAndEdges(ctx context.Context, database *gorm.DB, publication snapshotPublication, snapshot storage.ModuleSnapshot) error {
	if snapshot.WorktreeState != storage.WorktreeClean {
		for _, file := range publication.extraction.root.Files {
			blob := storage.SourceBlob{ContentHash: file.ContentHash, Content: file.Content}
			if err := database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&blob).Error; err != nil {
				return fmt.Errorf("store source %q of snapshot %s: %w", file.PathKey, snapshot.ID, err)
			}
		}
	}
	for _, dependency := range publication.extraction.root.Dependencies {
		edge := dependency.Edge
		edge.SnapshotID = snapshot.ID
		if publication.pendingEdges != nil {
			*publication.pendingEdges = append(*publication.pendingEdges, edge)
			continue
		}
		if err := database.WithContext(ctx).Create(&edge).Error; err != nil {
			return fmt.Errorf("publish dependency %q of snapshot %s: %w", edge.ModulePath, snapshot.ID, err)
		}
	}
	return nil
}

func (publication snapshotPublication) snapshot() storage.ModuleSnapshot {
	discovered := publication.extraction.root
	snapshot := storage.ModuleSnapshot{
		ID: publication.snapshotID, RootID: publication.root.ID, LocationID: publication.location.ID,
		Revision: discovered.Revision, GitCommit: discovered.GitCommit, ModuleVersion: discovered.ModuleVersion, WorktreeState: discovered.WorktreeState,
		ContentSetHash: discovered.ContentSetHash, ConfigurationHash: discovered.ConfigurationHash,
		ContextHash: publication.extraction.contextHash, Coverage: publication.extraction.coverage,
		PackageCount: len(publication.extraction.packages), Diagnostics: publication.extraction.diagnostics,
		StartedAt: publication.startedAt, CompletedAt: time.Now().UTC(), Reason: publication.reason,
	}
	indexStartedAt := publication.extraction.indexStartedAt
	snapshot.IndexStartedAt = &indexStartedAt
	if snapshot.ID == uuid.Nil {
		snapshot.ID = uuid.New()
	}
	if discovered.WorktreeState != storage.WorktreeClean {
		modified := discovered.LastModifiedAt
		snapshot.LastModifiedAt = &modified
	}
	hash := dependencyHash(discovered.Dependencies)
	snapshot.DependencySetHash = &hash
	if publication.base.snapshot.ID != uuid.Nil {
		baseID := publication.base.snapshot.ID
		snapshot.BaseSnapshotID = &baseID
	}
	return snapshot
}
