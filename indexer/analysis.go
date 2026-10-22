package indexer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	clickytask "github.com/flanksource/clicky/task"
	"github.com/flanksource/commons-db/git"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

type AnalysisOptions struct {
	Directory    string
	Mode         storage.AnalysisMode
	IncludeTests bool
	NoIndex      bool
	Source       *git.PreparedSource
}

func (indexer *Indexer) Analyze(ctx context.Context, options AnalysisOptions) (ModuleResult, error) {
	if indexer == nil || indexer.database == nil {
		return ModuleResult{}, errors.New("UIR analysis database is required")
	}
	if options.Mode != storage.AnalysisSyntax && options.Mode != storage.AnalysisTyped {
		return ModuleResult{}, fmt.Errorf("invalid analysis mode %q", options.Mode)
	}
	var result ModuleResult
	err := clickytask.Retry(ctx, indexRetryConfig, func(ctx context.Context, _ int) (err error) {
		result, err = indexer.analyze(ctx, options)
		return err
	})
	if errors.Is(err, ErrIndexInputsChanged) {
		return ModuleResult{}, fmt.Errorf("analysis of %q: %w", options.Directory, err)
	}
	return result, err
}

func (indexer *Indexer) analyze(ctx context.Context, options AnalysisOptions) (ModuleResult, error) {
	root, err := discoverAnalysis(ctx, options)
	if err != nil {
		return ModuleResult{}, err
	}
	head, snapshot, err := indexer.analysisBase(ctx, root)
	if err != nil {
		return ModuleResult{}, err
	}
	current, err := analysisCurrent(ctx, root, head, snapshot)
	if err != nil {
		return ModuleResult{}, err
	}
	if current && (snapshot.CoverageSettled() || options.NoIndex) {
		return ModuleResult{RootKey: root.RootKey, Location: root.LocalPath, SnapshotID: snapshot.ID.String(), Files: len(root.Files), ReusedFiles: len(root.Files), Unchanged: true}, nil
	}
	if options.NoIndex {
		return ModuleResult{}, fmt.Errorf("missing or stale %s lint analysis for %q; run uir lint without --no-index", options.Mode, root.LocalPath)
	}
	extraction, inputs, err := indexer.extractAnalysis(ctx, root, options.IncludeTests)
	if err != nil {
		return ModuleResult{}, err
	}
	return indexer.publishAnalysis(ctx, analysisPublication{options: options, extraction: extraction, inputs: inputs, head: head, previous: snapshot})
}

func discoverAnalysis(ctx context.Context, options AnalysisOptions) (discoveredRoot, error) {
	absolute, err := filepath.Abs(options.Directory)
	if err != nil {
		return discoveredRoot{}, err
	}
	path, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return discoveredRoot{}, err
	}
	directory, module, found, err := enclosingModule(path)
	if err != nil {
		return discoveredRoot{}, err
	}
	if !found {
		return discoveredRoot{}, fmt.Errorf("no enclosing Go module for %q", path)
	}
	root := discoveredRoot{RootKey: module, Name: filepath.Base(module), LocalPath: directory, Kind: "module", AnalysisMode: options.Mode}
	discovery := discoveryOptions{IncludeTests: options.IncludeTests}
	if options.Source != nil && options.Source.Info().Kind != git.SourceWorktree {
		info := options.Source.Info()
		if !pathWithin(directory, info.Dir) {
			return root, fmt.Errorf("analysis module is outside captured source")
		}
		discovery.Source = &info
	}
	if err := populateRoot(ctx, &root, []discoveredRoot{root}, discovery); err != nil {
		return root, err
	}
	root.ConfigurationHash = hashBytes([]byte("analysis-v1\x00" + string(options.Mode) + "\x00" + root.ConfigurationHash))
	return root, nil
}

type analysisPublication struct {
	options    AnalysisOptions
	extraction moduleExtraction
	inputs     analysisInputs
	head       storage.AnalysisHead
	previous   storage.ModuleSnapshot
}

func (indexer *Indexer) publishAnalysis(ctx context.Context, publication analysisPublication) (ModuleResult, error) {
	options, extraction, inputs := publication.options, publication.extraction, publication.inputs
	result := ModuleResult{RootKey: extraction.root.RootKey, Location: extraction.root.LocalPath, Files: len(extraction.root.Files)}
	err := storage.RetryAllocationConflicts(ctx, indexer.database, func(tx *gorm.DB) error {
		fresh, err := discoverAnalysis(ctx, options)
		if err != nil {
			return err
		}
		if fresh.ContentSetHash != extraction.root.ContentSetHash {
			return ErrIndexInputsChanged
		}
		if valid, err := inputs.current(); err != nil || !valid {
			if err != nil {
				return err
			}
			return ErrIndexInputsChanged
		}
		root := extraction.root
		if options.Source != nil && options.Source.Info().Kind != git.SourceWorktree {
			root.LocalPath = sourceLocation(options.Source.Info(), root.LocalPath)
		}
		storedRoot, location, err := ensureModuleLocation(ctx, tx, root, nil)
		if err != nil {
			return err
		}
		reason := storage.ReasonReindex
		if root.Historical {
			reason = storage.ReasonHistorical
		}
		published, err := publishSnapshot(ctx, tx, snapshotPublication{root: storedRoot, location: location, base: moduleBase{snapshot: publication.previous}, extraction: extraction, startedAt: time.Now().UTC(), reason: reason, preserveHead: true}, &result)
		if err != nil {
			return err
		}
		result.SnapshotID = published.ID.String()
		if root.Historical {
			return nil
		}
		return storeAnalysisHead(ctx, tx, publication.head, published, inputs)
	})
	return result, err
}
