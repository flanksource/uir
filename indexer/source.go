package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/commons-db/git"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

type SourceOptions struct {
	Source       *git.PreparedSource
	Directory    string
	IncludeTests bool
}

// IndexSource indexes the full prepared context as historical snapshots at the original locations.
// The caller owns the source and must keep it alive until this method returns.
func (indexer *Indexer) IndexSource(ctx context.Context, options SourceOptions) (results []ModuleResult, err error) {
	if indexer == nil || indexer.database == nil || options.Source == nil {
		return nil, errors.New("an index database and prepared source are required")
	}
	info := options.Source.Info()
	if info.Kind != git.SourceCommit && info.Kind != git.SourceStaged {
		return nil, errors.New("source indexing requires a commit or staged tree")
	}
	if !filepath.IsAbs(options.Directory) || !pathWithin(options.Directory, info.Dir) {
		return nil, fmt.Errorf("source directory %q is outside %q", options.Directory, info.Dir)
	}
	roots, err := discoverModules(ctx, options.Directory, discoveryOptions{IncludeTests: options.IncludeTests, Source: &info})
	if err != nil {
		return nil, err
	}
	manifests, err := os.MkdirTemp(filepath.Dir(info.Dir), "manifests-")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(manifests)) }()
	for i := range roots {
		directory := filepath.Join(manifests, fmt.Sprint(i))
		if err := os.Mkdir(directory, 0o700); err != nil {
			return nil, err
		}
		if roots[i], err = isolateRoot(ctx, roots[i], options.IncludeTests, directory); err != nil {
			return nil, err
		}
	}
	if err := indexer.indexVersionedDependencies(ctx, roots, options.IncludeTests); err != nil {
		return nil, err
	}
	extractions := make([]moduleExtraction, len(roots))
	for i, root := range roots {
		extractions[i], err = indexer.extractFromBase(ctx, extractOptions{loadPackages: indexer.loadPackages, root: root, includeTests: options.IncludeTests})
		if err != nil {
			return nil, err
		}
		extractions[i].root.LocalPath = sourceLocation(info, root.LocalPath)
		if root.ParentRootKey != "" {
			extractions[i].root.ParentRootKey = sourceLocation(info, root.ParentRootKey)
		}
	}
	return indexer.publishSource(ctx, extractions)
}

func (indexer *Indexer) publishSource(ctx context.Context, extractions []moduleExtraction) ([]ModuleResult, error) {
	var results []ModuleResult
	err := storage.RetryAllocationConflicts(ctx, indexer.database, func(transaction *gorm.DB) error {
		results = nil
		locations := map[string]storage.ModuleLocation{}
		for _, extraction := range extractions {
			result, location, err := indexModule(ctx, transaction, extraction, locations, ModuleOptions{Reason: storage.ReasonHistorical})
			if err != nil {
				return err
			}
			locations[result.Location] = location
			results = append(results, result)
		}
		return nil
	})
	return results, err
}

func sourceLocation(info git.SourceInfo, path string) string {
	relative, err := filepath.Rel(info.Dir, path)
	if err != nil || !filepath.IsLocal(relative) {
		panic(fmt.Sprintf("source path %q is outside %q", path, info.Dir))
	}
	return filepath.Join(info.RepositoryRoot, relative)
}
