package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/testruns"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

type testsImportOptions struct {
	File         string `args:"true" required:"true"`
	Root         string `flag:"root" help:"Registered Go module path the runs tested; defaults to the module of the current checkout"`
	Location     string `flag:"location" help:"Registered checkout to index an unindexed commit from first; other checkouts of the root that have the commit follow"`
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
	importCommand.Short = "Import a producer's test runs, each bound to the snapshot of the commit it recorded"
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

// importTests binds each run to the snapshot of the code it recorded it ran on and records it there.
// The state of any checkout plays no part: a checkout is only where an unindexed commit is read from.
func importTests(ctx context.Context, options testsImportOptions) (testruns.Result, error) {
	encoded, err := os.ReadFile(options.File)
	if err != nil {
		return testruns.Result{}, fmt.Errorf("read the test import %q: %w", options.File, err)
	}
	var imported testruns.Import
	if err := json.Unmarshal(encoded, &imported); err != nil {
		return testruns.Result{}, fmt.Errorf("%w: decode %q: %v", testruns.ErrInvalidImport, options.File, err)
	}
	if err := imported.Validate(); err != nil {
		return testruns.Result{}, err
	}
	database, err := databaseFor(ctx)
	if err != nil {
		return testruns.Result{}, err
	}
	rootKey, location, err := defaultCheckout(ctx, database, checkoutDefaults{Root: options.Root, Location: options.Location, Required: true})
	if err != nil {
		return testruns.Result{}, err
	}
	var root storage.ModuleRoot
	if err := database.WithContext(ctx).Where("root_key = ?", rootKey).Take(&root).Error; err != nil {
		return testruns.Result{}, fmt.Errorf("load root %q: %w", rootKey, err)
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
		if imported.Runs[0].Coverage, err = testruns.ParseGoCoverProfile(profile, root.RootKey); err != nil {
			return testruns.Result{}, err
		}
	}
	snapshots := make([]uuid.UUID, len(imported.Runs))
	for i, run := range imported.Runs {
		if snapshots[i], err = runSnapshot(ctx, database, root, location, run); err != nil {
			return testruns.Result{}, fmt.Errorf("run %d: %w", i, err)
		}
	}
	return testruns.ImportRuns(ctx, database, imported, snapshots)
}

// runSnapshot is the snapshot holding the bytes a run executed. A reported snapshot_id must be of the
// root and the run's commit and include test files. Without one the run is clean, and its commit's
// newest clean snapshot with test files is used, or the commit is indexed with them in a temporary
// worktree.
func runSnapshot(ctx context.Context, database *gorm.DB, root storage.ModuleRoot, location string, run testruns.Run) (uuid.UUID, error) {
	if run.SnapshotID != "" {
		id := uuid.MustParse(run.SnapshotID)
		var snapshot storage.ModuleSnapshot
		err := database.WithContext(ctx).Where("id = ?", id).Take(&snapshot).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return uuid.Nil, fmt.Errorf("snapshot_id %s is not in the index", id)
		case err != nil:
			return uuid.Nil, fmt.Errorf("load snapshot %s: %w", id, err)
		case snapshot.RootID != root.ID:
			return uuid.Nil, fmt.Errorf("snapshot_id %s is not a snapshot of root %q", id, root.RootKey)
		case snapshot.GitCommit != run.GitCommit:
			return uuid.Nil, fmt.Errorf("snapshot_id %s was indexed at commit %q, but the tests ran at git_commit %s", id, snapshot.GitCommit, run.GitCommit)
		}
		if includes, err := includesTests(ctx, database, id); err != nil || !includes {
			return uuid.Nil, errors.Join(err, fmt.Errorf("snapshot_id %s has no test files, so it cannot tell which test code ran; index with uir reindex --include-tests", id))
		}
		return id, nil
	}
	var snapshots []storage.ModuleSnapshot
	if err := database.WithContext(ctx).Where("root_id = ? AND git_commit = ? AND worktree_state = ?", root.ID, run.GitCommit, storage.WorktreeClean).
		Order("completed_at DESC").Order("started_at DESC").Order("id").Find(&snapshots).Error; err != nil {
		return uuid.Nil, fmt.Errorf("load the snapshots of commit %s: %w", run.GitCommit, err)
	}
	for _, snapshot := range snapshots {
		if includes, err := includesTests(ctx, database, snapshot.ID); err != nil || includes {
			return snapshot.ID, err
		}
	}
	return indexCommit(ctx, database, root, location, run.GitCommit)
}

// includesTests reports whether a snapshot serves a _test.go file. A snapshot indexed without test
// files serves none, so this proves only the setting of a root that has test files.
func includesTests(ctx context.Context, database *gorm.DB, snapshot uuid.UUID) (bool, error) {
	tests, err := storage.EffectiveSources(ctx, database, storage.EffectiveSourcesOptions{SnapshotID: snapshot, Suffix: "_test.go"})
	return len(tests) > 0, err
}

// indexCommit publishes a clean historical snapshot of commit with its test files, from location when
// it has the commit, else from the first other registered checkout of the root that does, without
// touching any checkout's worktree or head.
func indexCommit(ctx context.Context, database *gorm.DB, root storage.ModuleRoot, location, commit string) (uuid.UUID, error) {
	var locations []storage.ModuleLocation
	if err := database.WithContext(ctx).Where("root_id = ?", root.ID).Order("canonical_path").Find(&locations).Error; err != nil {
		return uuid.Nil, fmt.Errorf("load the checkouts of root %q: %w", root.RootKey, err)
	}
	sort.SliceStable(locations, func(i, j int) bool {
		return locations[i].CanonicalPath == location && locations[j].CanonicalPath != location
	})
	paths := make([]string, 0, len(locations))
	for _, candidate := range locations {
		paths = append(paths, candidate.CanonicalPath)
		if exec.CommandContext(ctx, "git", "-C", candidate.CanonicalPath, "cat-file", "-e", commit+"^{commit}").Run() != nil {
			continue
		}
		engine, err := indexer.New(database)
		if err != nil {
			return uuid.Nil, err
		}
		run, err := indexer.StartRevisionTask(ctx, engine, indexer.RevisionOptions{
			RootKey: root.RootKey, Checkout: candidate.CanonicalPath, Commit: commit, IncludeTests: true, Reason: storage.ReasonHistorical,
		})
		if err != nil {
			return uuid.Nil, err
		}
		indexed, err := run.Wait(ctx)
		if err != nil {
			return uuid.Nil, fmt.Errorf("index commit %s of %q: %w", commit, root.RootKey, err)
		}
		return uuid.Parse(indexed.SnapshotID)
	}
	return uuid.Nil, fmt.Errorf("commit %s has no clean snapshot with test files, and no registered checkout of root %q contains it to index (tried %s)", commit, root.RootKey, strings.Join(paths, ", "))
}
