package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/symboldiff"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

type historyOptions struct {
	Root     string `flag:"root" help:"Registered Go module path; defaults to the module of the current checkout"`
	Location string `flag:"location" help:"Registered checkout path; defaults to the current checkout, else the primary checkout"`
	Limit    int    `flag:"limit" help:"Number of recent commits, from 1 through 200" default:"50"`
}

type historyShowOptions struct {
	Commit       string   `args:"true" required:"true"`
	Root         string   `flag:"root" help:"Registered Go module path; defaults to the module of the current checkout"`
	Visibility   string   `flag:"visibility" help:"Rows to report: exported, internal, or all" default:"all"`
	ChangeType   []string `flag:"change-type" help:"Row classes to report: added, removed, signature, structure, body, format, moved, import; * and ! patterns (repeatable or comma separated; default all)"`
	Stat         bool     `flag:"stat" help:"Add line counts from hash-verified Git blobs" default:"true"`
	IncludeTests bool     `flag:"include-tests" help:"Include Go test symbols when indexing historical commits"`
}

type historySymbolOptions struct {
	Selector string `args:"true" required:"true"`
	Root     string `flag:"root" help:"Registered Go module path; defaults to the module of the current checkout"`
	Location string `flag:"location" help:"Registered checkout path; defaults to the current checkout, else the primary checkout"`
}

func registerHistoryCommand(root *cobra.Command) {
	command := clicky.AddNamedCommandWithContext("history", root, historyOptions{}, func(ctx context.Context, options historyOptions) (symboldiff.History, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return symboldiff.History{}, err
		}
		rootKey, location, err := defaultCheckout(ctx, database, checkoutDefaults{Root: options.Root, Location: options.Location, Required: true})
		if err != nil {
			return symboldiff.History{}, err
		}
		return symboldiff.ListHistory(ctx, database, rootKey, location, options.Limit)
	})
	command.Short = "List branch, commit, and pull request revisions of an indexed module"
	setModuleRoute(command, "modules/history")
	command.Annotations["clicky/operation-method"] = http.MethodGet
	show := clicky.AddNamedCommandWithContext("show", command, historyShowOptions{}, func(ctx context.Context, options historyShowOptions) (symboldiff.Result, error) {
		visibility, err := symboldiff.ParseVisibility(options.Visibility)
		if err != nil {
			return symboldiff.Result{}, err
		}
		database, err := databaseFor(ctx)
		if err != nil {
			return symboldiff.Result{}, err
		}
		rootKey, _, err := defaultCheckout(ctx, database, checkoutDefaults{Root: options.Root, Required: true})
		if err != nil {
			return symboldiff.Result{}, err
		}
		taskContext, err := runContext(ctx)
		if err != nil {
			return symboldiff.Result{}, err
		}
		return symboldiff.DiffCommit(ctx, database, symboldiff.CommitOptions{
			RootKey: rootKey, Commit: options.Commit, Visibility: visibility, ChangeTypes: options.ChangeType, Stat: options.Stat, IncludeTests: options.IncludeTests,
			TaskContext: taskContext,
		})
	})
	show.Use = "show <commit>"
	show.Short = "Show a Git commit's logical changes against its first parent"
	show.Args = cobra.ExactArgs(1)
	clicky.MarkLocalOnly(show)
	symbol := clicky.AddNamedCommandWithContext("symbol", command, historySymbolOptions{}, func(ctx context.Context, options historySymbolOptions) ([]storage.SymbolHistoryEntry, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return nil, err
		}
		rootKey, location, err := defaultCheckout(ctx, database, checkoutDefaults{Root: options.Root, Location: options.Location, Required: true})
		if err != nil {
			return nil, err
		}
		return symbolHistory(ctx, database, options.Selector, query.ModuleScopeOptions{RootKey: rootKey, Location: location})
	})
	symbol.Use = "symbol <selector>"
	symbol.Short = "List the snapshots and commits at which one symbol's implementation changed"
	symbol.Example = "uir history symbol 'func:Add'"
	symbol.Args = cobra.ExactArgs(1)
	clicky.MarkLocalOnly(symbol)
}

// symbolHistory resolves selector to exactly one declaration in scope and lists the snapshots along its
// snapshot's base chain at which the symbol's content generation advanced.
func symbolHistory(ctx context.Context, database *gorm.DB, selector string, scope query.ModuleScopeOptions) ([]storage.SymbolHistoryEntry, error) {
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return nil, err
	}
	result, err := pipeline.RunModules(ctx, selector, scope)
	if err != nil {
		return nil, err
	}
	declarations := map[[2]string]bool{}
	for _, match := range append(result.Declarations, result.Matches...) {
		if match.SymbolID != "" && match.SnapshotID != "" && (match.Kind == "definition" || match.Kind == "symbol") {
			declarations[[2]string{match.SymbolID, match.SnapshotID}] = true
		}
	}
	if len(declarations) != 1 {
		return nil, fmt.Errorf("selector %q matches %d declarations in scope; narrow it to one symbol in one checkout", selector, len(declarations))
	}
	var declaration [2]string
	for declaration = range declarations {
	}
	handles, err := storage.SymbolHandles(ctx, database, []string{declaration[0]})
	if err != nil {
		return nil, err
	}
	snapshot, err := uuid.Parse(declaration[1])
	if err != nil {
		return nil, fmt.Errorf("parse snapshot id %q: %w", declaration[1], err)
	}
	return storage.SymbolHistory(ctx, database, snapshot, handles[declaration[0]])
}
