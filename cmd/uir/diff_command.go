package main

import (
	"context"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir/symboldiff"
	"github.com/spf13/cobra"
)

type diffOptions struct {
	Range        string `args:"true" required:"true"`
	Root         string `flag:"root" help:"Module path" required:"true"`
	Visibility   string `flag:"visibility" help:"Rows to report: exported, internal, or all" default:"exported"`
	Stat         bool   `flag:"stat" help:"Add per-file and per-symbol line counts from hash-verified Git blobs"`
	SnapshotFrom string `flag:"snapshot-from" help:"Snapshot UUID to use for <from> instead of its newest clean snapshot"`
	SnapshotTo   string `flag:"snapshot-to" help:"Snapshot UUID to use for <to> instead of its newest clean snapshot"`
}

func registerDiffCommand(root *cobra.Command) {
	command := clicky.AddNamedCommandWithContext("diff", root, diffOptions{}, func(ctx context.Context, options diffOptions) (symboldiff.Result, error) {
		from, to, err := symboldiff.ParseRange(options.Range)
		if err != nil {
			return symboldiff.Result{}, err
		}
		visibility, err := symboldiff.ParseVisibility(options.Visibility)
		if err != nil {
			return symboldiff.Result{}, err
		}
		database, err := databaseFor(ctx)
		if err != nil {
			return symboldiff.Result{}, err
		}
		return symboldiff.Diff(ctx, database, symboldiff.Options{
			RootKey: options.Root, From: from, To: to, SnapshotFrom: options.SnapshotFrom, SnapshotTo: options.SnapshotTo,
			Visibility: visibility, Stat: options.Stat,
		})
	})
	command.Use = "diff <from>..<to>"
	command.Short = "List symbols added, removed, and changed between two indexed commits"
	command.Args = cobra.ExactArgs(1)
	setModuleRoute(command, "modules/diff")
}
