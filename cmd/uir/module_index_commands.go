package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/entity"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

type moduleAddOptions struct {
	Path                 string `args:"true"`
	IncludeTests         bool   `flag:"include-tests" help:"Index Go test files"`
	Force                bool   `flag:"force" help:"Reparse all sources and publish a new snapshot"`
	IncludeWorkspaceUses bool   `flag:"include-workspace-uses" help:"Also add go.work use paths outside the requested directory"`
	NoWorkspaceUses      bool   `flag:"no-workspace-uses" help:"Do not add go.work use paths outside the requested directory"`
}

type moduleReindexOptions struct {
	Path         string `args:"true"`
	All          bool   `flag:"all" help:"Reindex every registered checkout with no indexed head"`
	IncludeTests bool   `flag:"include-tests" help:"Index Go test files"`
	Force        bool   `flag:"force" help:"Reparse all sources and publish a new snapshot"`
}

// moduleIndexRun answers add and reindex. An HTTP request gets the run id with 202 Accepted while the
// run continues under the serve context; the CLI waits and gets every module's result as well.
type moduleIndexRun struct {
	RunID    string                 `json:"run_id"`
	Results  []indexer.ModuleResult `json:"results,omitempty"`
	accepted bool
}

func (run moduleIndexRun) ResponseStatus() int {
	if run.accepted {
		return http.StatusAccepted
	}
	return http.StatusOK
}

func registerModuleIndexCommands(root *cobra.Command) {
	registerAddCommand(root)
	registerReindexCommand(root)
	registerImportCommand(root)
	registerPruneCommand(root)
}

func registerAddCommand(root *cobra.Command) {
	var add *cobra.Command
	add = clicky.AddNamedCommandWithContext("add", root, moduleAddOptions{}, func(ctx context.Context, options moduleAddOptions) (moduleIndexRun, error) {
		path := options.Path
		if path == "" {
			path = "."
		}
		pathOptions := addPathOptions{Path: path, IncludeWorkspaceUses: options.IncludeWorkspaceUses, NoWorkspaceUses: options.NoWorkspaceUses}
		if entity.OperationSurfaceFromContext(ctx) == "cli" {
			pathOptions.Input, pathOptions.Prompt = add.InOrStdin(), add.ErrOrStderr()
		}
		paths, err := addPaths(pathOptions)
		if err != nil {
			return moduleIndexRun{}, err
		}
		requests := make([]indexer.ModuleOptions, 0, len(paths))
		for _, selected := range paths {
			requests = append(requests, indexer.ModuleOptions{Path: selected, IncludeTests: options.IncludeTests, Force: options.Force, Reason: storage.ReasonAdd})
		}
		return startModuleIndex(ctx, func(runCtx context.Context, engine *indexer.Indexer) (*indexer.ModuleIndexRun, error) {
			return indexer.StartModulesTask(runCtx, engine, requests)
		})
	})
	add.Use = "add [path]"
	add.Short = "Add Go modules below a directory and index them immediately"
	add.Args = cobra.MaximumNArgs(1)
	setModuleRoute(add, "modules/add")
}

func registerReindexCommand(root *cobra.Command) {
	reindex := clicky.AddNamedCommandWithContext("reindex", root, moduleReindexOptions{}, func(ctx context.Context, options moduleReindexOptions) (moduleIndexRun, error) {
		if options.All && options.Path != "" {
			return moduleIndexRun{}, errors.New("reindex --all does not accept a path")
		}
		if options.All && options.Force {
			return moduleIndexRun{}, errors.New("reindex --all does not accept --force")
		}
		path := options.Path
		if path == "" {
			path = "."
		}
		return startModuleIndex(ctx, func(runCtx context.Context, engine *indexer.Indexer) (*indexer.ModuleIndexRun, error) {
			if options.All {
				return indexer.StartMissingModulesTask(runCtx, engine, options.IncludeTests)
			}
			return indexer.StartModulesTask(runCtx, engine, []indexer.ModuleOptions{{
				Path: path, IncludeTests: options.IncludeTests, Force: options.Force, ExistingOnly: true, Reason: storage.ReasonReindex,
			}})
		})
	})
	reindex.Use = "reindex [path]"
	reindex.Short = "Incrementally reindex registered Go modules"
	reindex.Args = cobra.MaximumNArgs(1)
	setModuleRoute(reindex, "modules/reindex")
}

// startModuleIndex starts a run under runContext. An HTTP request is answered with the run id at
// once, the runtime keeping count of the run until it ends; any other caller waits for its results.
func startModuleIndex(ctx context.Context, start func(context.Context, *indexer.Indexer) (*indexer.ModuleIndexRun, error)) (moduleIndexRun, error) {
	runtime, err := runtimeFor(ctx)
	if err != nil {
		return moduleIndexRun{}, err
	}
	var database *gorm.DB
	if err := indexer.Phase(ctx, "db-open", func(ctx context.Context) (err error) {
		database, err = runtime.Database(ctx)
		return err
	}); err != nil {
		return moduleIndexRun{}, err
	}
	engine, err := indexer.New(database)
	if err != nil {
		return moduleIndexRun{}, err
	}
	runCtx, err := runContext(ctx)
	if err != nil {
		return moduleIndexRun{}, err
	}
	run, err := start(runCtx, engine)
	if err != nil {
		return moduleIndexRun{}, err
	}
	if detachedRequest(ctx) {
		runtime.detached.Go(func() {
			// The outcome is the run's own record, in the task list and task_runs; this wait only keeps
			// serve from closing the database under it.
			if _, err := run.Wait(context.Background()); err != nil {
				logger.Warnf("task run %s: %v", run.ID(), err)
			}
		})
		return moduleIndexRun{RunID: run.ID(), accepted: true}, nil
	}
	results, err := run.Wait(ctx)
	return moduleIndexRun{RunID: run.ID(), Results: results}, err
}

// addModules indexes the modules below path as the CLI does, for callers that need them indexed.
func addModules(ctx context.Context, database *gorm.DB, path string, force bool) ([]indexer.ModuleResult, error) {
	engine, err := indexer.New(database)
	if err != nil {
		return nil, err
	}
	run, err := indexer.StartModulesTask(ctx, engine, []indexer.ModuleOptions{{Path: path, Force: force, Reason: storage.ReasonAdd}})
	if err != nil {
		return nil, err
	}
	return run.Wait(ctx)
}

// reindexAllModules reindexes every registered checkout without a head, as `uir reindex --all` does.
func reindexAllModules(ctx context.Context, database *gorm.DB, includeTests bool) ([]indexer.ModuleResult, error) {
	engine, err := indexer.New(database)
	if err != nil {
		return nil, err
	}
	run, err := indexer.StartMissingModulesTask(ctx, engine, includeTests)
	if err != nil {
		return nil, err
	}
	return run.Wait(ctx)
}
