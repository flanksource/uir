package main

import (
	"context"
	"errors"
	"os"

	"github.com/flanksource/clicky"
	"github.com/flanksource/gavel/linters"
	"github.com/flanksource/uir/lint"
	"github.com/spf13/cobra"
)

type lintOptions struct {
	Config   string   `flag:"config" help:"Path to .uir.yaml (default: nearest config up to Git root)"`
	Rules    []string `flag:"rule" help:"Run only these exact rule IDs"`
	Include  []string `flag:"include" help:"Report files matching project-relative globs (repeat or comma-separate)"`
	Exclude  []string `flag:"exclude" help:"Exclude exact rule IDs or project-relative file globs (repeat or comma-separate; exclusions win)"`
	NoIndex  bool     `flag:"no-index" help:"Require current stored facts without refreshing the index"`
	Snapshot string   `flag:"snapshot" help:"Lint an immutable historical snapshot without refreshing"`
	GroupBy  string   `flag:"group-by" help:"Group findings by symbol or file (default: file)"`
}

func registerLintCommand(root *cobra.Command, runtime *commandRuntime) {
	command := clicky.AddNamedCommandWithContext("lint", root, lintOptions{}, func(ctx context.Context, options lintOptions) ([]*lint.Result, error) {
		directory, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		path, err := lint.Discover(directory, options.Config)
		if err != nil {
			return lintFailure(err), err
		}
		config, err := lint.Load(path)
		if err != nil {
			return lintFailure(err), err
		}
		if _, err := config.Filter(lint.Options{Rules: options.Rules, Include: options.Include, Exclude: options.Exclude, GroupBy: options.GroupBy}); err != nil {
			return lintFailure(err), err
		}
		if options.NoIndex && options.Snapshot != "" {
			err := errors.New("--no-index and --snapshot cannot be combined")
			return lintFailure(err), err
		}
		database, err := runtime.Database(ctx)
		if err != nil {
			return lintFailure(err), err
		}
		results := []*lint.Result{lint.Run(ctx, database, config, lint.Options{Rules: options.Rules, Include: options.Include, Exclude: options.Exclude, NoIndex: options.NoIndex, Snapshot: options.Snapshot, GroupBy: options.GroupBy})}
		if linters.HasFailures([]*linters.LinterResult{results[0].LinterResult}) {
			return results, errors.New("UIR lint failed")
		}
		return results, nil
	})
	command.Short = "Run SQL architecture rules from .uir.yaml"
	command.Args = cobra.NoArgs
	clicky.MarkLocalOnly(command)
}

func lintFailure(err error) []*lint.Result {
	return []*lint.Result{{LinterResult: &linters.LinterResult{Linter: "uir", Error: err.Error()}}}
}
