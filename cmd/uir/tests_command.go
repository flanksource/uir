package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/testruns"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type testsImportOptions struct {
	File         string `args:"true" required:"true"`
	Path         string `flag:"path" help:"Checkout the tests ran in; it must match its indexed head exactly" default:"."`
	CoverProfile string `flag:"coverprofile" help:"go test -coverprofile output to attach as the aggregate coverage of the import's one run"`
}

type testsFlakyOptions struct {
	Root    string        `flag:"root" help:"Registered Go module path; defaults to the module of the current checkout"`
	Since   time.Duration `flag:"since" help:"Only read runs that started within this duration; 0 reads every run"`
	MinRuns int           `flag:"min-runs" help:"Only report tests with at least this many runs" default:"1"`
}

func registerTestsCommand(root *cobra.Command) {
	command := &cobra.Command{Use: "tests", Short: "Record test runs against indexed snapshots and report flaky tests"}
	root.AddCommand(command)
	importCommand := clicky.AddNamedCommandWithContext("import", command, testsImportOptions{}, importTests)
	importCommand.Use = "import <results.json>"
	importCommand.Short = "Import a producer's test runs, bound to the checkout's current indexed snapshot"
	importCommand.Args = cobra.ExactArgs(1)
	clicky.MarkLocalOnly(importCommand)
	flaky := clicky.AddNamedCommandWithContext("flaky", command, testsFlakyOptions{}, func(ctx context.Context, options testsFlakyOptions) ([]testruns.FlakyTest, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return nil, err
		}
		rootKey, _, err := defaultCheckout(ctx, database, checkoutDefaults{Root: options.Root, Required: true})
		if err != nil {
			return nil, err
		}
		var module storage.ModuleRoot
		if err := database.WithContext(ctx).Where("root_key = ?", rootKey).Take(&module).Error; err != nil {
			return nil, fmt.Errorf("load root %q: %w", rootKey, err)
		}
		since := time.Time{}
		if options.Since > 0 {
			since = time.Now().Add(-options.Since)
		}
		return testruns.Flaky(ctx, database, testruns.FlakyOptions{RootID: module.ID, Since: since, MinRuns: options.MinRuns})
	})
	flaky.Short = "List tests whose outcome differed on identical code in one environment"
	clicky.MarkLocalOnly(flaky)
}

// importTests proves the checkout is exactly its indexed head, with the head's test setting, and
// records the import against that snapshot. There is no nearest-snapshot fallback.
func importTests(ctx context.Context, options testsImportOptions) (testruns.Result, error) {
	encoded, err := os.ReadFile(options.File)
	if err != nil {
		return testruns.Result{}, fmt.Errorf("read the test import %q: %w", options.File, err)
	}
	var imported testruns.Import
	if err := json.Unmarshal(encoded, &imported); err != nil {
		return testruns.Result{}, fmt.Errorf("%w: decode %q: %v", testruns.ErrInvalidImport, options.File, err)
	}
	database, err := databaseFor(ctx)
	if err != nil {
		return testruns.Result{}, err
	}
	engine, err := indexer.New(database)
	if err != nil {
		return testruns.Result{}, err
	}
	includeTests, err := engine.HeadIncludesTests(ctx, options.Path)
	if err != nil {
		return testruns.Result{}, err
	}
	if !includeTests {
		return testruns.Result{}, fmt.Errorf("the indexed head of %q excludes test files, so it cannot tell which test code ran; run uir reindex --include-tests", options.Path)
	}
	current, err := engine.CheckCurrent(ctx, options.Path, indexer.CheckOptions{IncludeTests: true, AllowIncomplete: true})
	if err != nil {
		return testruns.Result{}, withChangedPaths(ctx, options.Path, err, "the checkout differs from its indexed head (commit, gitignore, or move these, then reindex)")
	}
	// A head reindexed from a dirty checkout matches it, but its bytes are no commit's: refuse it, so a
	// run is never recorded against uncommitted code.
	var head storage.ModuleSnapshot
	if err := database.WithContext(ctx).Where("id = ?", current.SnapshotID).Take(&head).Error; err != nil {
		return testruns.Result{}, fmt.Errorf("load snapshot %s: %w", current.SnapshotID, err)
	}
	if head.WorktreeState == storage.WorktreeDirty {
		return testruns.Result{}, withChangedPaths(ctx, options.Path,
			fmt.Errorf("the indexed head of %q was indexed from a dirty worktree, so the tests ran against uncommitted changes", current.Location),
			"commit, stash or gitignore these, reindex, and rerun the tests")
	}
	if options.CoverProfile != "" {
		if len(imported.Runs) != 1 {
			return testruns.Result{}, errors.New("--coverprofile needs an import with exactly one run")
		}
		profile, err := os.Open(options.CoverProfile)
		if err != nil {
			return testruns.Result{}, fmt.Errorf("open the coverage profile: %w", err)
		}
		defer func() { _ = profile.Close() }()
		if imported.Runs[0].Coverage, err = testruns.ParseGoCoverProfile(profile, current.RootKey); err != nil {
			return testruns.Result{}, err
		}
	}
	snapshot, err := uuid.Parse(current.SnapshotID)
	if err != nil {
		return testruns.Result{}, fmt.Errorf("parse snapshot id %q: %w", current.SnapshotID, err)
	}
	return testruns.ImportRuns(ctx, database, snapshot, imported)
}

// withChangedPaths adds to a refused import the paths Git reports as changed or untracked in the
// checkout, after hint: any of them, Go source or not, makes the checkout differ from a clean commit,
// such as a producer's output directory that is not ignored.
func withChangedPaths(ctx context.Context, path string, err error, hint string) error {
	status, statusErr := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", path, "status", "--porcelain", "--", ".").Output()
	// Trim only the end: a porcelain line starts with its status columns, such as " M".
	trimmed := strings.TrimRight(string(status), "\n")
	if statusErr != nil || trimmed == "" {
		return err
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > 10 {
		lines = append(lines[:10], fmt.Sprintf("... and %d more", len(lines)-10))
	}
	return fmt.Errorf("%w; %s:\n%s", err, hint, strings.Join(lines, "\n"))
}
