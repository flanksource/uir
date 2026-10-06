package indexer

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
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

// headReasons are the reasons IndexModules publishes a snapshot that advances a Go checkout's head for;
// Publish advances an external location's head for import.
var headReasons = map[storage.SnapshotReason]bool{
	storage.ReasonAdd: true, storage.ReasonReindex: true, storage.ReasonRefactor: true, storage.ReasonLocalDependency: true,
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

// IndexModules indexes the modules options selects, and the local and versioned dependencies they
// need first. Every location it publishes or confirms is remembered in the run's memo, which it starts
// when ctx belongs to no run, so a later module of the run that depends on one reuses its snapshot.
func (indexer *Indexer) IndexModules(ctx context.Context, options ModuleOptions) ([]ModuleResult, error) {
	if indexer == nil || indexer.database == nil {
		return nil, errors.New("UIR index database is required")
	}
	if !headReasons[options.Reason] {
		return nil, fmt.Errorf("index modules under %q: reason %q is not one of add, reindex, refactor, local-dependency", options.Path, options.Reason)
	}
	ctx, memo := ensureRunMemo(ctx)
	results, err := indexer.indexModules(ctx, options)
	if err != nil {
		return nil, err
	}
	memo.rememberModules(results, options.IncludeTests)
	return results, nil
}

func (indexer *Indexer) indexModules(ctx context.Context, options ModuleOptions) ([]ModuleResult, error) {
	var roots []discoveredRoot
	if err := Phase(ctx, "discover", func(ctx context.Context) (err error) {
		roots, err = discoverRequestedModules(ctx, options)
		return err
	}); err != nil {
		return nil, err
	}
	if options.ExistingOnly {
		for _, root := range roots {
			if err := requireRegisteredLocation(ctx, indexer.database, root); err != nil {
				return nil, err
			}
		}
	}
	var graph localDependencyGraph
	var cyclic bool
	if err := Phase(ctx, "dependencies", func(ctx context.Context) (err error) {
		graph, cyclic, err = readModuleDependencies(ctx, roots, options.IncludeTests)
		return err
	}); err != nil {
		return nil, err
	}
	if cyclic {
		return indexer.indexLocalCycleGraph(ctx, graph, len(roots), options)
	}
	ctx = withActivePaths(ctx, roots)
	if err := Phase(ctx, "local-deps", func(ctx context.Context) error {
		return indexer.indexLocalDependencies(ctx, roots, options.IncludeTests)
	}); err != nil {
		return nil, err
	}
	if err := Phase(ctx, "versioned-deps", func(ctx context.Context) error {
		return indexer.indexVersionedDependencies(ctx, roots, options.IncludeTests)
	}); err != nil {
		return nil, err
	}
	extractions, err := indexer.extractModules(ctx, roots, options)
	if err != nil {
		return nil, err
	}
	return indexer.publishModules(ctx, roots, extractions, options)
}

// readModuleDependencies reads every root's dependencies and, unless an enclosing IndexModules is
// already indexing local dependencies, discovers their local dependency graph and whether it is cyclic.
func readModuleDependencies(ctx context.Context, roots []discoveredRoot, includeTests bool) (localDependencyGraph, bool, error) {
	var err error
	for i := range roots {
		roots[i].Dependencies, err = readDependencies(ctx, roots[i])
		if err != nil {
			return localDependencyGraph{}, false, fmt.Errorf("read dependencies of module %q: %w", roots[i].RootKey, err)
		}
	}
	if ctx.Value(activeModulePaths{}) != nil {
		return localDependencyGraph{}, false, nil
	}
	graph, err := discoverLocalDependencyGraph(ctx, roots, includeTests)
	if err != nil {
		return localDependencyGraph{}, false, err
	}
	return graph, graph.cyclic(), nil
}

// extractModules extracts every root whose head snapshot is not reusable.
func (indexer *Indexer) extractModules(ctx context.Context, roots []discoveredRoot, options ModuleOptions) ([]moduleExtraction, error) {
	extractions := make([]moduleExtraction, 0, len(roots))
	for _, root := range roots {
		var head uuid.UUID
		var reusable bool
		if err := Phase(ctx, "unchanged-check", func(ctx context.Context) (err error) {
			head, reusable, err = reusableHead(ctx, indexer.database, root, options.Force)
			return err
		}); err != nil {
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
	return extractions, nil
}

// publishModules publishes every extraction in one transaction, after verifying the inputs it was
// extracted from are still current.
func (indexer *Indexer) publishModules(ctx context.Context, roots []discoveredRoot, extractions []moduleExtraction, options ModuleOptions) ([]ModuleResult, error) {
	var results []ModuleResult
	err := storage.RetryAllocationConflicts(ctx, indexer.database, func(transaction *gorm.DB) error {
		if err := Phase(ctx, "verify-inputs", func(ctx context.Context) error {
			return verifyIndexInputs(ctx, transaction, roots, options.IncludeTests)
		}); err != nil {
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
