package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/entity"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/symboldiff"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

type diffOptions struct {
	Revisions    []string `args:"true"`
	Root         string   `flag:"root" help:"Module path of the revisions; defaults to the module of the current checkout"`
	Visibility   string   `flag:"visibility" help:"Rows to report: exported, internal, or all" default:"exported"`
	ChangeType   []string `flag:"change-type" help:"Row classes to report: added, removed, signature, structure, body, moved; * and ! patterns (repeatable or comma separated; default all)"`
	Untracked    bool     `flag:"untracked" help:"Compare untracked files that are not ignored as new files when a revision is compared against the worktree"`
	Stat         bool     `flag:"stat" help:"Add per-file and per-symbol line counts from hash-verified Git blobs, or an external root's stored blobs"`
	SnapshotFrom string   `flag:"snapshot-from" help:"Snapshot UUID to use for <from> instead of its newest clean snapshot"`
	SnapshotTo   string   `flag:"snapshot-to" help:"Snapshot UUID to use for <to> instead of its newest clean snapshot"`
	AutoIndex    bool     `flag:"auto-index" help:"Index missing clean commit snapshots without moving the checkout head"`
	IncludeTests bool     `flag:"include-tests" help:"Include Go test symbols when indexing historical commits"`
	Location     string   `flag:"location" help:"Registered checkout to resolve and index commits from; defaults to the current checkout, else the primary checkout"`
}

func registerDiffCommand(root *cobra.Command) {
	var command *cobra.Command
	command = clicky.AddNamedCommandWithContext("diff", root, diffOptions{}, func(ctx context.Context, options diffOptions) (symboldiff.Result, error) {
		visibility, err := symboldiff.ParseVisibility(options.Visibility)
		if err != nil {
			return symboldiff.Result{}, err
		}
		if len(options.Revisions) == 0 || slices.Equal(options.Revisions, []string{"-"}) {
			return diffPatch(ctx, options, visibility, command.InOrStdin())
		}
		return diffRevisions(ctx, options, visibility)
	})
	command.Use = "diff [<range> | <rev> | <from> <to> | -]"
	command.Short = "List symbols added, removed, and changed between two indexed commits, between a commit and the worktree, or in a git patch on stdin"
	command.Long = `Revisions are read as git diff reads them; pr:N names the head of pull request N.

  A..B, A.., ..B   from A to B (an empty side is HEAD)
  A B              from A to B
  A...B            from the merge base of A and B to B
  A^!, A^-n        from A's parent (the nth) to A
  A                from A to the worktree of the current checkout, without an index
  -, or nothing    a git patch read from stdin (git diff HEAD~3 | uir diff)`
	command.Args = cobra.MaximumNArgs(2)
	setModuleRoute(command, "modules/diff")
}

// diffRevisions resolves the revisions in the root's checkouts: two commits are compared from the
// index, and a single commit against the worktree of the checkout it resolved in.
func diffRevisions(ctx context.Context, options diffOptions, visibility symboldiff.Visibility) (symboldiff.Result, error) {
	database, err := databaseFor(ctx)
	if err != nil {
		return symboldiff.Result{}, err
	}
	if resolution, found, err := unregisteredWorktree(ctx, database, options); err != nil || found {
		if err != nil {
			return symboldiff.Result{}, err
		}
		return diffWorktree(ctx, options, visibility, resolution)
	}
	rootKey, location, err := defaultCheckout(ctx, database, checkoutDefaults{Root: options.Root, Location: options.Location, Required: true})
	if err != nil {
		return symboldiff.Result{}, err
	}
	request := symboldiff.Options{
		RootKey: rootKey, Revisions: options.Revisions, SnapshotFrom: options.SnapshotFrom, SnapshotTo: options.SnapshotTo,
		Visibility: visibility, ChangeTypes: options.ChangeType, Stat: options.Stat, AutoIndex: options.AutoIndex, IncludeTests: options.IncludeTests, Location: location,
	}
	resolution, err := symboldiff.ResolveRevisions(ctx, database, request)
	if err != nil {
		return symboldiff.Result{}, err
	}
	if resolution.Worktree {
		return diffWorktree(ctx, options, visibility, resolution)
	}
	if options.Untracked {
		return symboldiff.Result{}, fmt.Errorf("--untracked applies to a revision compared against the worktree, not the range %q", options.Revisions)
	}
	if options.AutoIndex {
		if request.TaskContext, err = runContext(ctx); err != nil {
			return symboldiff.Result{}, err
		}
	}
	request.Revisions = []string{resolution.From, resolution.To}
	return symboldiff.Diff(ctx, database, request)
}

// unregisteredWorktree resolves a single revision in the working directory's git repository when that
// directory is outside every registered checkout and neither --root nor --location names one: a
// worktree diff reads only git, not the index. A range there still needs a registered checkout,
// because it compares indexed snapshots.
func unregisteredWorktree(ctx context.Context, database *gorm.DB, options diffOptions) (symboldiff.Resolution, bool, error) {
	if detachedRequest(ctx) || options.Root != "" || options.Location != "" || len(options.Revisions) != 1 {
		return symboldiff.Resolution{}, false, nil
	}
	directory, err := os.Getwd()
	if err != nil {
		return symboldiff.Resolution{}, false, fmt.Errorf("resolve the working directory for the default --root: %w", err)
	}
	if _, found, err := indexer.LocateCheckout(ctx, database, directory); err != nil || found {
		return symboldiff.Resolution{}, false, err
	}
	resolution, err := symboldiff.ResolveInRepository(ctx, directory, options.Revisions[0])
	if err != nil {
		return symboldiff.Resolution{}, false, fmt.Errorf("--root is required: %s is not inside a registered checkout (run uir add . or pass --root), and %w", directory, err)
	}
	if !resolution.Worktree {
		return symboldiff.Resolution{}, false, fmt.Errorf("--root is required for the range %q, which compares indexed snapshots: %s is not inside a registered checkout (run uir add . or pass --root)", options.Revisions[0], directory)
	}
	return resolution, true, nil
}

// diffWorktree compares a commit with the worktree of its checkout through git diff's patch, as
// git diff <rev> does. Over HTTP the server's worktree is not the caller's.
func diffWorktree(ctx context.Context, options diffOptions, visibility symboldiff.Visibility, resolution symboldiff.Resolution) (symboldiff.Result, error) {
	if detachedRequest(ctx) {
		return symboldiff.Result{}, entity.NewStatusError(http.StatusBadRequest, "worktree_diff", fmt.Sprintf("revision %q compares against a worktree, which an HTTP diff cannot read; pass two revisions", options.Revisions[0]))
	}
	if flags := setFlags(map[string]bool{
		"--snapshot-from": options.SnapshotFrom != "", "--snapshot-to": options.SnapshotTo != "",
		"--auto-index": options.AutoIndex, "--include-tests": options.IncludeTests,
	}); len(flags) > 0 {
		return symboldiff.Result{}, fmt.Errorf("%s applies to a range of two commits, not revision %q compared against the worktree", strings.Join(flags, ", "), options.Revisions[0])
	}
	patch, err := symboldiff.WorktreePatch(ctx, symboldiff.WorktreePatchOptions{Checkout: resolution.Checkout, Commit: resolution.From, Untracked: options.Untracked})
	if err != nil {
		return symboldiff.Result{}, err
	}
	locator, err := symboldiff.NewGitLocator(ctx, resolution.Checkout)
	if err != nil {
		return symboldiff.Result{}, err
	}
	return symboldiff.DiffPatch(ctx, symboldiff.PatchOptions{
		Patch: patch, From: resolution.From, Locator: locator, Visibility: visibility, ChangeTypes: options.ChangeType, Stat: options.Stat,
	})
}

// diffPatch compares a git patch read from stdin against the git repository of the working directory.
// Over HTTP there is no stdin, and the server's working directory is not the caller's.
func diffPatch(ctx context.Context, options diffOptions, visibility symboldiff.Visibility, stdin io.Reader) (symboldiff.Result, error) {
	if detachedRequest(ctx) {
		return symboldiff.Result{}, entity.NewStatusError(http.StatusBadRequest, "missing_range", "an HTTP diff requires revisions in args; a patch is read from stdin on the CLI")
	}
	if flags := setFlags(map[string]bool{
		"--root": options.Root != "", "--snapshot-from": options.SnapshotFrom != "", "--snapshot-to": options.SnapshotTo != "",
		"--auto-index": options.AutoIndex, "--include-tests": options.IncludeTests, "--location": options.Location != "",
		"--untracked": options.Untracked,
	}); len(flags) > 0 {
		return symboldiff.Result{}, fmt.Errorf("%s applies to a commit range, not a patch on stdin", strings.Join(flags, ", "))
	}
	directory, err := os.Getwd()
	if err != nil {
		return symboldiff.Result{}, fmt.Errorf("a patch is read against the working directory's repository: %w", err)
	}
	locator, err := symboldiff.NewGitLocator(ctx, directory)
	if err != nil {
		return symboldiff.Result{}, err
	}
	return symboldiff.DiffPatch(ctx, symboldiff.PatchOptions{Patch: stdin, Locator: locator, Visibility: visibility, ChangeTypes: options.ChangeType, Stat: options.Stat})
}

// setFlags is the sorted names of the flags that are set.
func setFlags(flags map[string]bool) []string {
	var set []string
	for flag, isSet := range flags {
		if isSet {
			set = append(set, flag)
		}
	}
	slices.Sort(set)
	return set
}
