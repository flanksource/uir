package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	Location     string `flag:"location" help:"Registered checkout to index a run's code from first; the root's other checkouts follow"`
	CoverProfile string `flag:"coverprofile" help:"go test -coverprofile output to attach as the aggregate coverage of the import's one run"`
}

type testsFlakyOptions struct {
	Root    string        `flag:"root" help:"Registered Go module path; defaults to the module of the current checkout"`
	Since   time.Duration `flag:"since" help:"Only read runs that started within this duration; 0 reads every run"`
	MinRuns int           `flag:"min-runs" help:"Only report tests with at least this many runs" default:"1"`
}

type testsPlanOptions struct {
	File     string `args:"true" required:"true"`
	Root     string `flag:"root" help:"Registered Go module path; defaults to the module of the current checkout"`
	Location string `flag:"location" help:"Registered checkout to ensure current; defaults to the current checkout"`
}

func registerTestsCommand(root *cobra.Command) {
	command := &cobra.Command{Use: "tests", Short: "Record test runs against indexed snapshots and report flaky tests"}
	root.AddCommand(command)
	importCommand := clicky.AddNamedCommandWithContext("import", command, testsImportOptions{}, importTests)
	importCommand.Use = "import <results.json>"
	importCommand.Short = "Import a producer's test runs, each bound to the snapshot of the commit it recorded"
	importCommand.Args = cobra.ExactArgs(1)
	clicky.MarkLocalOnly(importCommand)
	planCommand := clicky.AddNamedCommandWithContext("plan", command, testsPlanOptions{}, planTests)
	planCommand.Use = "plan <request.json>"
	planCommand.Short = "Ensure the exact current test snapshot and prove inventory tests skippable"
	planCommand.Args = cobra.ExactArgs(1)
	clicky.MarkLocalOnly(planCommand)
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

func planTests(ctx context.Context, options testsPlanOptions) (testruns.PlanResponse, error) {
	encoded, err := os.ReadFile(options.File)
	if err != nil {
		return testruns.PlanResponse{}, fmt.Errorf("read test plan request %q: %w", options.File, err)
	}
	var request testruns.PlanRequest
	if err := json.Unmarshal(encoded, &request); err != nil {
		return testruns.PlanResponse{}, fmt.Errorf("decode test plan request %q: %w", options.File, err)
	}
	if err := request.Validate(); err != nil {
		return testruns.PlanResponse{}, err
	}
	goEnvironment := exec.CommandContext(ctx, "go", "env", "-json", "GOOS", "GOARCH", "GOVERSION", "CGO_ENABLED", "GOEXPERIMENT")
	goEnvironment.Dir = options.Location
	environmentOutput, err := goEnvironment.Output()
	if err != nil {
		return testruns.PlanResponse{}, fmt.Errorf("read current Go build environment: %w", err)
	}
	var currentEnvironment map[string]string
	if err := json.Unmarshal(environmentOutput, &currentEnvironment); err != nil {
		return testruns.PlanResponse{}, fmt.Errorf("decode current Go build environment: %w", err)
	}
	for requestKey, currentKey := range map[string]string{
		"GOOS": "GOOS", "GOARCH": "GOARCH", "go": "GOVERSION", "CGO_ENABLED": "CGO_ENABLED", "GOEXPERIMENT": "GOEXPERIMENT",
	} {
		if request.Environment[requestKey] != currentEnvironment[currentKey] {
			return testruns.PlanResponse{}, fmt.Errorf("requested %s=%q does not match the current build environment %q", requestKey, request.Environment[requestKey], currentEnvironment[currentKey])
		}
	}
	database, err := databaseFor(ctx)
	if err != nil {
		return testruns.PlanResponse{}, err
	}
	rootKey, location, err := defaultCheckout(ctx, database, checkoutDefaults{Root: options.Root, Location: options.Location, Required: true})
	if err != nil {
		return testruns.PlanResponse{}, err
	}
	var root storage.ModuleRoot
	if err := database.WithContext(ctx).Where("root_key = ?", rootKey).Take(&root).Error; err != nil {
		return testruns.PlanResponse{}, fmt.Errorf("load root %q: %w", rootKey, err)
	}
	requestFile, err := filepath.Abs(options.File)
	if err != nil {
		return testruns.PlanResponse{}, err
	}
	identity := testruns.Run{GitCommit: request.GitCommit, Dirty: request.Dirty, GitTree: request.GitTree}
	if mismatch, err := ranOn(ctx, location, requestFile, identity); err != nil || mismatch != "" {
		return testruns.PlanResponse{}, errors.Join(err, fmt.Errorf("test plan request does not match %s: %s", location, mismatch))
	}
	engine, err := indexer.New(database)
	if err != nil {
		return testruns.PlanResponse{}, err
	}
	started, err := indexer.StartModulesTask(ctx, engine, []indexer.ModuleOptions{{
		Path: location, ExactLocation: location, IncludeTests: true, ExistingOnly: true, Reason: storage.ReasonReindex,
	}})
	if err != nil {
		return testruns.PlanResponse{}, err
	}
	results, err := started.Wait(ctx)
	if err != nil {
		return testruns.PlanResponse{}, fmt.Errorf("ensure current test index: %w", err)
	}
	if mismatch, err := ranOn(ctx, location, requestFile, identity); err != nil || mismatch != "" {
		return testruns.PlanResponse{}, errors.Join(err, fmt.Errorf("%s changed while planning (%s); rerun the plan", location, mismatch))
	}
	var snapshotID uuid.UUID
	for _, result := range results {
		if result.RootKey == root.RootKey {
			snapshotID, err = uuid.Parse(result.SnapshotID)
			break
		}
	}
	if err != nil || snapshotID == uuid.Nil {
		return testruns.PlanResponse{}, errors.Join(err, fmt.Errorf("ensure current published no snapshot of root %q", root.RootKey))
	}
	var snapshot storage.ModuleSnapshot
	if err := database.WithContext(ctx).Where("id = ?", snapshotID).Take(&snapshot).Error; err != nil {
		return testruns.PlanResponse{}, fmt.Errorf("load current snapshot %s: %w", snapshotID, err)
	}
	return testruns.Plan(ctx, database, root, snapshot, request)
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
	resultsFile, err := filepath.Abs(options.File)
	if err != nil {
		return testruns.Result{}, fmt.Errorf("resolve the test import %q: %w", options.File, err)
	}
	if canonical, err := filepath.EvalSymlinks(resultsFile); err == nil {
		resultsFile = canonical
	}
	snapshots := make([]uuid.UUID, len(imported.Runs))
	for i, run := range imported.Runs {
		if snapshots[i], err = runSnapshot(ctx, database, root, location, resultsFile, run); err != nil {
			return testruns.Result{}, fmt.Errorf("run %d: %w", i, err)
		}
	}
	return testruns.ImportRuns(ctx, database, imported, snapshots)
}

// runSnapshot is the snapshot holding the bytes a run executed. A clean run's is its commit's newest
// clean snapshot with test files, or the commit indexed with them in a temporary worktree. A dirty run's
// bytes exist only in a checkout that still holds them, so it is indexed from the checkout whose HEAD is
// the run's commit and whose working tree hashes to the run's git_tree.
func runSnapshot(ctx context.Context, database *gorm.DB, root storage.ModuleRoot, location, resultsFile string, run testruns.Run) (uuid.UUID, error) {
	if run.Dirty {
		return indexDirtyRun(ctx, database, root, location, resultsFile, run)
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

// indexDirtyRun reindexes, with test files, the first registered checkout of root (location first) that
// is exactly the code a dirty run ran on: HEAD is its git_commit and the working tree hashes to its
// git_tree, before and after indexing, so an edit made meanwhile is not bound to the run.
func indexDirtyRun(ctx context.Context, database *gorm.DB, root storage.ModuleRoot, location, resultsFile string, run testruns.Run) (uuid.UUID, error) {
	paths, err := checkouts(ctx, database, root, location)
	if err != nil {
		return uuid.Nil, err
	}
	var mismatches []string
	for _, checkout := range paths {
		if mismatch, err := ranOn(ctx, checkout, resultsFile, run); err != nil || mismatch != "" {
			if err != nil {
				return uuid.Nil, err
			}
			mismatches = append(mismatches, checkout+": "+mismatch)
			continue
		}
		engine, err := indexer.New(database)
		if err != nil {
			return uuid.Nil, err
		}
		started, err := indexer.StartModulesTask(ctx, engine, []indexer.ModuleOptions{{
			Path: checkout, ExactLocation: checkout, IncludeTests: true, ExistingOnly: true, Reason: storage.ReasonReindex,
		}})
		if err != nil {
			return uuid.Nil, err
		}
		results, err := started.Wait(ctx)
		if err != nil {
			return uuid.Nil, fmt.Errorf("index %s: %w", checkout, err)
		}
		if mismatch, err := ranOn(ctx, checkout, resultsFile, run); err != nil || mismatch != "" {
			return uuid.Nil, errors.Join(err, fmt.Errorf("%s changed while it was indexed (%s); rerun the import", checkout, mismatch))
		}
		for _, result := range results {
			if result.RootKey == root.RootKey {
				return uuid.Parse(result.SnapshotID)
			}
		}
		return uuid.Nil, fmt.Errorf("indexing %s published no snapshot of root %q", checkout, root.RootKey)
	}
	return uuid.Nil, fmt.Errorf("the tests ran on uncommitted changes to %s, and no registered checkout of root %q still holds them, so the code changed since the tests ran; rerun the tests and import their results (%s)",
		run.GitCommit, root.RootKey, strings.Join(mismatches, "; "))
}

// ranOn says why checkout is not the code run ran on, or "" when it is: its HEAD is run.GitCommit and
// its working tree hashes to run.GitTree as the producer hashed it, at the repository's top level with
// uncommitted and untracked, non-ignored files and without resultsFile. The hash is written through a
// temporary index, so the checkout's own index and working tree are untouched.
func ranOn(ctx context.Context, checkout, resultsFile string, run testruns.Run) (string, error) {
	git := func(environment []string, args ...string) (string, error) {
		command := exec.CommandContext(ctx, "git", append([]string{"-C", checkout}, args...)...)
		command.Env = append(os.Environ(), environment...)
		var stderr strings.Builder
		command.Stderr = &stderr
		output, err := command.Output()
		if err != nil {
			return "", fmt.Errorf("git %s in %s: %w: %s", strings.Join(args, " "), checkout, err, strings.TrimSpace(stderr.String()))
		}
		return strings.TrimSpace(string(output)), nil
	}
	head, err := git(nil, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", err
	}
	if head != run.GitCommit {
		return fmt.Sprintf("HEAD is %s, not %s", head, run.GitCommit), nil
	}
	top, err := git(nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	scratch, err := os.MkdirTemp("", "uir-tree-")
	if err != nil {
		return "", fmt.Errorf("create a temporary index: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	index := []string{"GIT_INDEX_FILE=" + filepath.Join(scratch, "index")}
	pathspec := []string{"--", ":/"}
	if relative, err := filepath.Rel(top, resultsFile); err == nil && filepath.IsLocal(relative) {
		pathspec = append(pathspec, ":(top,exclude,literal)"+filepath.ToSlash(relative))
	}
	if _, err := git(index, "read-tree", "HEAD"); err != nil {
		return "", err
	}
	if _, err := git(index, append([]string{"add", "-A"}, pathspec...)...); err != nil {
		return "", err
	}
	tree, err := git(index, "write-tree")
	if err != nil {
		return "", err
	}
	expected := run.GitTree
	if !run.Dirty {
		if expected, err = git(nil, "rev-parse", "HEAD^{tree}"); err != nil {
			return "", err
		}
	}
	if tree != expected {
		if run.Dirty {
			return fmt.Sprintf("its working tree is %s, not git_tree %s", tree, expected), nil
		}
		return fmt.Sprintf("its working tree is dirty (%s, not HEAD tree %s)", tree, expected), nil
	}
	return "", nil
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
	paths, err := checkouts(ctx, database, root, location)
	if err != nil {
		return uuid.Nil, err
	}
	for _, checkout := range paths {
		if exec.CommandContext(ctx, "git", "-C", checkout, "cat-file", "-e", commit+"^{commit}").Run() != nil {
			continue
		}
		engine, err := indexer.New(database)
		if err != nil {
			return uuid.Nil, err
		}
		run, err := indexer.StartRevisionTask(ctx, engine, indexer.RevisionOptions{
			RootKey: root.RootKey, Checkout: checkout, Commit: commit, IncludeTests: true, Reason: storage.ReasonHistorical,
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

// checkouts are the registered checkouts of root, location first.
func checkouts(ctx context.Context, database *gorm.DB, root storage.ModuleRoot, location string) ([]string, error) {
	var locations []storage.ModuleLocation
	if err := database.WithContext(ctx).Where("root_id = ?", root.ID).Order("canonical_path").Find(&locations).Error; err != nil {
		return nil, fmt.Errorf("load the checkouts of root %q: %w", root.RootKey, err)
	}
	sort.SliceStable(locations, func(i, j int) bool {
		return locations[i].CanonicalPath == location && locations[j].CanonicalPath != location
	})
	paths := make([]string, 0, len(locations))
	for _, candidate := range locations {
		paths = append(paths, candidate.CanonicalPath)
	}
	return paths, nil
}
