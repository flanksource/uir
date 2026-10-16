package main

import (
	"context"
	"net/http"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type moduleBrowseOptions struct {
	RootKey    string `flag:"root" help:"Module path; defaults to the module of the current checkout"`
	Location   string `flag:"location" help:"Registered checkout path; defaults to the current checkout"`
	SnapshotID string `flag:"snapshot" help:"Immutable snapshot UUID"`
	allRootsOption
}

type moduleContentOptions struct {
	SnapshotID string `flag:"snapshot" help:"Immutable snapshot UUID" required:"true"`
	Path       string `flag:"path" help:"Module-relative source path" required:"true"`
}

type moduleDependenciesOptions struct {
	SnapshotID string `flag:"snapshot" help:"Immutable snapshot UUID" required:"true"`
}

func registerModuleBrowseCommands(root *cobra.Command) {
	dependencies := clicky.AddNamedCommandWithContext("dependencies", root, moduleDependenciesOptions{}, func(ctx context.Context, options moduleDependenciesOptions) (storage.SnapshotDependenciesResult, error) {
		id, err := uuid.Parse(options.SnapshotID)
		if err != nil {
			return storage.SnapshotDependenciesResult{}, err
		}
		database, err := databaseFor(ctx)
		if err != nil {
			return storage.SnapshotDependenciesResult{}, err
		}
		return storage.ModuleDependencies(ctx, database, id)
	})
	dependencies.Short = "List declared dependencies and target snapshots"
	setModuleRoute(dependencies, "modules/dependencies")
	dependencies.Annotations["clicky/operation-method"] = http.MethodGet

	heads := clicky.AddNamedCommandWithContext("heads", root, struct{}{}, func(ctx context.Context, _ struct{}) ([]query.ModuleHeadFiles, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return nil, err
		}
		pipeline, err := query.NewPipeline(database)
		if err != nil {
			return nil, err
		}
		return pipeline.BrowseModuleHeads(ctx)
	})
	heads.Short = "List files from every published module checkout head"
	setModuleRoute(heads, "modules/heads")
	heads.Annotations["clicky/operation-method"] = http.MethodGet

	browse := clicky.AddNamedCommandWithContext("browse", root, moduleBrowseOptions{}, func(ctx context.Context, options moduleBrowseOptions) (query.ModuleBrowseResult, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return query.ModuleBrowseResult{}, err
		}
		pipeline, err := query.NewPipeline(database)
		if err != nil {
			return query.ModuleBrowseResult{}, err
		}
		scope, err := moduleScope(ctx, database, checkoutDefaults{Root: options.RootKey, Location: options.Location, Snapshot: options.SnapshotID, All: options.All})
		if err != nil {
			return query.ModuleBrowseResult{}, err
		}
		return pipeline.BrowseModules(ctx, scope)
	})
	browse.Short = "Browse effective sources and symbols in one module snapshot"
	setModuleRoute(browse, "modules/browse")
	browse.Annotations["clicky/operation-method"] = http.MethodGet

	content := clicky.AddNamedCommandWithContext("content", root, moduleContentOptions{}, func(ctx context.Context, options moduleContentOptions) (query.ModuleSourceContent, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return query.ModuleSourceContent{}, err
		}
		pipeline, err := query.NewPipeline(database)
		if err != nil {
			return query.ModuleSourceContent{}, err
		}
		return pipeline.ReadModuleSource(ctx, options.SnapshotID, options.Path)
	})
	content.Short = "Read verified source content for an immutable module snapshot"
	setModuleRoute(content, "modules/content")
	content.Annotations["clicky/operation-method"] = http.MethodGet
}
