package query

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/flanksource/uir/storage"
)

// ModuleSourcesOptions selects the sources ReadModuleSources reads: module-relative paths of one snapshot.
type ModuleSourcesOptions struct {
	SnapshotID string
	Paths      []string
}

// ReadModuleSources reads the indexed bytes of every path of one snapshot, in the order asked: the
// snapshot's scope and effective sources are resolved once, and the sources Git must recover are read
// through one git cat-file process. It fails, naming the path, on the first source not in the snapshot
// or whose indexed bytes cannot be recovered.
func (pipeline *Pipeline) ReadModuleSources(ctx context.Context, options ModuleSourcesOptions) ([]ModuleSourceContent, error) {
	if pipeline == nil || pipeline.database == nil {
		return nil, errors.New("UIR query database is required")
	}
	if len(options.Paths) == 0 {
		return nil, fmt.Errorf("reading the sources of snapshot %s requires at least one source path", options.SnapshotID)
	}
	for _, path := range options.Paths {
		if !filepath.IsLocal(path) {
			return nil, fmt.Errorf("source path %q must be local to its module root", path)
		}
	}
	selection, err := pipeline.moduleScopes(ctx, ModuleScopeOptions{SnapshotID: options.SnapshotID}, false)
	if err != nil {
		return nil, err
	}
	scope := selection.scopes[0]
	paths := make([]string, len(options.Paths))
	for index, path := range options.Paths {
		paths[index] = filepath.ToSlash(path)
	}
	revisions, err := storage.EffectiveSources(ctx, pipeline.database, storage.EffectiveSourcesOptions{SnapshotID: scope.snapshot.ID, Paths: paths})
	if err != nil {
		return nil, err
	}
	sources := make([]verifiedSource, 0, len(options.Paths))
	for _, path := range options.Paths {
		revision, found := revisions[filepath.ToSlash(path)]
		if !found {
			return nil, fmt.Errorf("source %q is not in snapshot %s", path, options.SnapshotID)
		}
		sources = append(sources, verifiedSource{path: path, hash: revision.ContentHash, snapshot: options.SnapshotID, revision: &revision})
	}
	contents := make([]ModuleSourceContent, 0, len(sources))
	for index, read := range readVerifiedSources(ctx, pipeline.database, scope, sources, pipeline.options.gitReader) {
		if read.err != nil {
			return nil, read.err
		}
		contents = append(contents, ModuleSourceContent{
			Path: options.Paths[index], Content: string(read.content), Origin: read.origin,
			Revision: scope.snapshot.Revision, SnapshotID: options.SnapshotID,
		})
	}
	return contents, nil
}
