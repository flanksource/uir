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

type ModuleOptions struct {
	Path         string
	IncludeTests bool
	Force        bool
	ExistingOnly bool
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
}

func (indexer *Indexer) IndexModules(ctx context.Context, options ModuleOptions) ([]ModuleResult, error) {
	if indexer == nil || indexer.database == nil {
		return nil, errors.New("UIR index database is required")
	}
	roots, err := discoverModules(ctx, options.Path, options.IncludeTests)
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
	results := make([]ModuleResult, 0, len(roots))
	err = indexer.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
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
		base.snapshot.ConfigurationHash == discovered.ConfigurationHash && base.snapshot.ContextHash == extraction.contextHash
	if extraction.reusedHead != uuid.Nil {
		if !base.hasHead || base.snapshot.ID != extraction.reusedHead {
			return ModuleResult{}, storage.ModuleLocation{}, fmt.Errorf("location %s head moved from snapshot %s during indexing", location.ID, extraction.reusedHead)
		}
		unchanged = true
	}
	if unchanged {
		result.SnapshotID, result.HeadVersion, result.ReusedFiles, result.Unchanged = base.snapshot.ID.String(), base.head.Version, len(discovered.Files), true
		return result, location, nil
	}
	snapshot, err := publishSnapshot(ctx, database, snapshotPublication{
		root: root, location: location, base: base, extraction: extraction, force: options.Force, startedAt: startedAt,
	}, &result)
	if err != nil {
		return ModuleResult{}, storage.ModuleLocation{}, err
	}
	result.SnapshotID = snapshot.ID.String()
	return result, location, nil
}

type snapshotPublication struct {
	root       storage.ModuleRoot
	location   storage.ModuleLocation
	base       moduleBase
	extraction moduleExtraction
	force      bool
	startedAt  time.Time
}

// publishSnapshot writes source revisions, symbols, documents, and postings, then the snapshot row
// with its package coverage and source deltas, and finally advances the location head with a
// compare-and-swap; the caller's transaction makes the whole publication atomic. Extraction is
// already complete, so the transaction only writes rows.
func publishSnapshot(ctx context.Context, database *gorm.DB, publication snapshotPublication, result *ModuleResult) (storage.ModuleSnapshot, error) {
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
	if err := publishDocuments(ctx, database, publication.extraction, current, publication.force, result); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	snapshot := publication.snapshot()
	if err := database.WithContext(ctx).Create(&snapshot).Error; err != nil {
		return storage.ModuleSnapshot{}, fmt.Errorf("create module snapshot: %w", err)
	}
	if err := createSourceDeltas(ctx, database, snapshot, previous, current); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	if err := createPackageCoverage(ctx, database, snapshot, publication.extraction.packages); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	if result.HeadVersion, err = advanceHead(ctx, database, publication.base, snapshot); err != nil {
		return storage.ModuleSnapshot{}, err
	}
	return snapshot, nil
}

func (publication snapshotPublication) snapshot() storage.ModuleSnapshot {
	discovered := publication.extraction.root
	snapshot := storage.ModuleSnapshot{
		ID: uuid.New(), RootID: publication.root.ID, LocationID: publication.location.ID,
		Revision: discovered.Revision, WorktreeState: discovered.WorktreeState,
		ContentSetHash: discovered.ContentSetHash, ConfigurationHash: discovered.ConfigurationHash,
		ContextHash: publication.extraction.contextHash, Coverage: publication.extraction.coverage,
		PackageCount: len(publication.extraction.packages), Diagnostics: publication.extraction.diagnostics,
		StartedAt: publication.startedAt, CompletedAt: time.Now().UTC(),
	}
	if publication.base.snapshot.ID != uuid.Nil {
		baseID := publication.base.snapshot.ID
		snapshot.BaseSnapshotID = &baseID
	}
	return snapshot
}
