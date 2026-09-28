package main

import (
	"context"
	"net/http"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir/symboldiff"
	"github.com/spf13/cobra"
)

type historyOptions struct {
	Root     string `flag:"root" help:"Registered Go module path" required:"true"`
	Location string `flag:"location" help:"Registered checkout path; defaults to the primary checkout"`
	Limit    int    `flag:"limit" help:"Number of recent commits, from 1 through 200" default:"50"`
}

type historyShowOptions struct {
	Commit       string `args:"true" required:"true"`
	Root         string `flag:"root" help:"Registered Go module path" required:"true"`
	Visibility   string `flag:"visibility" help:"Rows to report: exported, internal, or all" default:"all"`
	Stat         bool   `flag:"stat" help:"Add line counts from hash-verified Git blobs" default:"true"`
	IncludeTests bool   `flag:"include-tests" help:"Include Go test symbols when indexing historical commits"`
}

func registerHistoryCommand(root *cobra.Command) {
	command := clicky.AddNamedCommandWithContext("history", root, historyOptions{}, func(ctx context.Context, options historyOptions) (symboldiff.History, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return symboldiff.History{}, err
		}
		return symboldiff.ListHistory(ctx, database, options.Root, options.Location, options.Limit)
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
		return symboldiff.DiffCommit(ctx, database, symboldiff.CommitOptions{
			RootKey: options.Root, Commit: options.Commit, Visibility: visibility, Stat: options.Stat, IncludeTests: options.IncludeTests,
		})
	})
	show.Use = "show <commit>"
	show.Short = "Show a Git commit's logical changes against its first parent"
	show.Args = cobra.ExactArgs(1)
	clicky.MarkLocalOnly(show)
}
