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
}
