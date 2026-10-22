package main

import (
	"context"
	"errors"
	"os"

	"github.com/flanksource/clicky"
	"github.com/flanksource/gavel/linters"
	"github.com/flanksource/uir/lint"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

type lintOptions struct {
	Config   string   `flag:"config" help:"Use a single config file (default: merge home and ancestor .uir.yaml files)"`
	Rules    []string `flag:"rule" help:"Run only these exact rule IDs"`
	Include  []string `flag:"include" help:"Report file globs relative to the project, or each module with --all (repeat or comma-separate)"`
	Exclude  []string `flag:"exclude" help:"Exclude exact rule IDs or file globs (project-relative, or module-relative with --all; exclusions win)"`
	NoIndex  bool     `flag:"no-index" help:"Require fresh stored analysis without refreshing"`
	Snapshot string   `flag:"snapshot" help:"Lint an immutable historical snapshot without refreshing"`
	GroupBy  string   `flag:"group-by" help:"Group findings by symbol or file (default: file)"`
	Changed  bool     `flag:"changed" help:"Report changed files; an optional revision or range selects an immutable commit tree"`
	Staged   bool     `flag:"staged" help:"Lint the exact staged tree and report staged files"`
	Revision string   `args:"true" help:"Commit, A..B or A...B (requires --changed)"`
	All      bool     `flag:"all" help:"Analyze every registered module's primary location in one combined query scope"`
}

func registerLintCommand(root *cobra.Command, runtime *commandRuntime) {
	command := clicky.AddNamedCommandWithContext("lint", root, lintOptions{}, func(ctx context.Context, options lintOptions) (results []*lint.Result, err error) {
		directory, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		request, err := lint.Prepare(ctx, directory, options.Config, lint.Options{All: options.All, Rules: options.Rules, Include: options.Include, Exclude: options.Exclude, NoIndex: options.NoIndex, Snapshot: options.Snapshot, GroupBy: options.GroupBy, Changed: options.Changed, Staged: options.Staged, Revision: options.Revision})
		if err != nil {
			return lintFailure(err), err
		}
		defer func() {
			if cleanup := request.Release(); cleanup != nil {
				err = errors.Join(err, cleanup)
				if len(results) == 0 {
					results = lintFailure(err)
				} else {
					results[0].Success = false
					if results[0].Error == "" {
						results[0].Error = cleanup.Error()
					} else {
						results[0].Error += "; " + cleanup.Error()
					}
				}
			}
		}()
		var database *gorm.DB
		if !request.Empty() {
			database, err = runtime.Database(ctx)
			if err != nil {
				return lintFailure(err), err
			}
		}
		results = []*lint.Result{request.Run(ctx, database)}
		if linters.HasFailures([]*linters.LinterResult{results[0].LinterResult}) {
			return results, errors.New("UIR lint failed")
		}
		return results, nil
	})
	command.Short = "Run SQL architecture rules from .uir.yaml"
	command.Args = cobra.MaximumNArgs(1)
	command.Use = "lint [revision|A..B|A...B]"
	clicky.MarkLocalOnly(command)
}

func lintFailure(err error) []*lint.Result {
	return []*lint.Result{{LinterResult: &linters.LinterResult{Linter: "uir", Error: err.Error()}}}
}
