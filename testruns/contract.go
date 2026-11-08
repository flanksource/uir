// Package testruns records test runs, their results and coverage against the immutable snapshot whose
// bytes they executed, and answers which tests are flaky: whose outcome differs on identical code.
//
// A producer (gavel) reports what a test runner ran in the producer-neutral Import contract; uir never
// parses test runner output or finds tests in source. Import binds a run to the snapshot of the code the
// run recorded it ran on, links each test to the declaration it is when that is provable, attributes coverage
// blocks to declarations, and fingerprints each result's closure.
package testruns

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/flanksource/uir/storage"
)

// ContractVersion is the newest Import version this package reads.
const ContractVersion = 2

// fullObject is a full SHA-1 or SHA-256 Git object name.
var fullObject = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// ErrInvalidImport marks an import the contract rejects.
var ErrInvalidImport = errors.New("invalid test import")

// Import is the JSON a producer writes for `uir tests import`.
type Import struct {
	Version int   `json:"version"`
	Runs    []Run `json:"runs"`
}

// Run is one execution of a root's tests. Environment is what makes two runs comparable (GOOS, GOARCH,
// toolchain, -race, build tags, -count); its digest groups results for flakiness.
//
// GitCommit, Dirty and GitTree say which code the tests ran on: the full SHA of HEAD, whether the
// worktree had uncommitted changes to indexed inputs, and, required when dirty, the Git tree hash of the
// repository's working tree with uncommitted and untracked, non-ignored files and without the producer's
// export file (GIT_INDEX_FILE=<tmp> git read-tree HEAD && git add -A && git write-tree, at the top level).
type Run struct {
	GitCommit   string            `json:"git_commit"`
	Dirty       bool              `json:"dirty,omitempty"`
	GitTree     string            `json:"git_tree,omitempty"`
	Producer    string            `json:"producer"`
	Environment map[string]string `json:"environment"`
	StartedAt   time.Time         `json:"started_at"`
	FinishedAt  time.Time         `json:"finished_at"`
	TaskRunID   string            `json:"task_run_id,omitempty"`
	// InputsDigest is the producer's digest of the inputs uir does not index, such as embedded SQL,
	// testdata and configuration files. Every closure fingerprint of the run folds it in; without it a
	// change to those files is invisible to flakiness.
	InputsDigest string `json:"inputs_digest,omitempty"`
	// SelectionMode is full when the producer executed its complete current inventory and partial
	// when it applied a plan. Only a complete, identity-verified full run can be a future baseline.
	SelectionMode    string `json:"selection_mode,omitempty"`
	InventoryDigest  string `json:"inventory_digest,omitempty"`
	Complete         bool   `json:"complete,omitempty"`
	IdentityVerified bool   `json:"identity_verified,omitempty"`
	PlanID           string `json:"plan_id,omitempty"`
	Tests            []Test `json:"tests"`
	// Coverage is the run's aggregate coverage profile.
	Coverage []Block `json:"coverage,omitempty"`
}

// Test is one test the runner reported, in gavel's terms: framework ("go test", "ginkgo", "jest", ...),
// package, suite path, name, and the module-relative file and line it is declared at.
type Test struct {
	Framework string    `json:"framework"`
	Package   string    `json:"package"`
	Suite     []string  `json:"suite,omitempty"`
	Name      string    `json:"name"`
	File      string    `json:"file,omitempty"`
	Line      int       `json:"line,omitempty"`
	Attempts  []Attempt `json:"attempts"`
	// Coverage is this test's own coverage profile, from a run of this test alone.
	Coverage []Block `json:"coverage,omitempty"`
}

// Attempt is one execution of a test within a run; retries are later attempts.
type Attempt struct {
	Outcome    storage.TestOutcome `json:"outcome"`
	DurationMS int64               `json:"duration_ms"`
	Failure    string              `json:"failure,omitempty"`
}

// Block is a coverage block in a module-relative file: one-based lines and columns, the statements it
// holds, and how many times it ran.
type Block struct {
	Path       string `json:"path"`
	StartLine  int    `json:"start_line"`
	StartCol   int    `json:"start_col"`
	EndLine    int    `json:"end_line"`
	EndCol     int    `json:"end_col"`
	Statements int    `json:"statements"`
	Count      int64  `json:"count"`
}

func invalid(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidImport, fmt.Sprintf(format, arguments...))
}

// Validate checks the import against the contract before anything is read or written.
func (imported Import) Validate() error {
	if imported.Version != 1 && imported.Version != ContractVersion {
		return invalid("version %d is neither 1 nor %d", imported.Version, ContractVersion)
	}
	if len(imported.Runs) == 0 {
		return invalid("no runs")
	}
	for i, run := range imported.Runs {
		if err := run.validate(); err != nil {
			return fmt.Errorf("run %d: %w", i, err)
		}
	}
	return nil
}

func (run Run) validate() error {
	switch {
	case strings.TrimSpace(run.Producer) == "":
		return invalid("producer is required")
	case run.StartedAt.IsZero() || run.FinishedAt.Before(run.StartedAt):
		return invalid("started_at is required and finished_at may not precede it")
	case run.GitCommit == "":
		return invalid("git_commit is required: without it the code the tests ran on is unknown")
	case !fullObject.MatchString(run.GitCommit):
		return invalid("git_commit %q is not a full lowercase commit SHA", run.GitCommit)
	case run.Dirty && run.GitTree == "":
		return invalid("the tests ran on uncommitted changes to %s but report no git_tree, so the code they ran on cannot be identified", run.GitCommit)
	case run.GitTree != "" && !fullObject.MatchString(run.GitTree):
		return invalid("git_tree %q is not a full lowercase tree hash", run.GitTree)
	case run.SelectionMode != "" && run.SelectionMode != "full" && run.SelectionMode != "partial":
		return invalid("selection_mode %q is not full or partial", run.SelectionMode)
	case run.SelectionMode != "" && len(run.InventoryDigest) != 64:
		return invalid("an affected-test run requires a 64-character inventory_digest")
	case run.Complete && run.SelectionMode == "":
		return invalid("complete requires selection_mode")
	case run.IdentityVerified && run.SelectionMode == "":
		return invalid("identity_verified requires selection_mode")
	}
	seen := map[string]bool{}
	for i, test := range run.Tests {
		key := test.namePath()
		if err := test.validate(); err != nil {
			return fmt.Errorf("test %d (%s): %w", i, test.display(), err)
		}
		if seen[test.Framework+"\x00"+test.Package+"\x00"+key] {
			return invalid("test %s is reported twice", test.display())
		}
		seen[test.Framework+"\x00"+test.Package+"\x00"+key] = true
	}
	return validateBlocks(run.Coverage)
}

func (test Test) validate() error {
	switch {
	case test.Framework == "" || test.Name == "":
		return invalid("framework and name are required")
	case len(test.Attempts) == 0:
		return invalid("at least one attempt is required")
	case test.File != "" && !validPath(test.File):
		return invalid("file %q is not a module-relative slash path", test.File)
	}
	for i, attempt := range test.Attempts {
		if !slices.Contains([]storage.TestOutcome{storage.TestPass, storage.TestFail, storage.TestSkip, storage.TestTimeout, storage.TestAborted}, attempt.Outcome) {
			return invalid("attempt %d has outcome %q, not pass, fail, skip, timeout or aborted", i+1, attempt.Outcome)
		}
		if attempt.DurationMS < 0 {
			return invalid("attempt %d has a negative duration", i+1)
		}
	}
	return validateBlocks(test.Coverage)
}

func validateBlocks(blocks []Block) error {
	for _, block := range blocks {
		switch {
		case !validPath(block.Path):
			return invalid("coverage path %q is not a module-relative slash path", block.Path)
		case block.StartLine < 1 || block.EndLine < block.StartLine || block.Statements < 0 || block.Count < 0:
			return invalid("coverage block %s is malformed", block)
		}
	}
	return nil
}

func validPath(name string) bool {
	return name != "" && !strings.HasPrefix(name, "/") && !strings.Contains(name, `\`) && path.Clean(name) == name && !strings.HasPrefix(name, "../") && name != ".."
}

func (block Block) String() string {
	return fmt.Sprintf("%s:%d.%d,%d.%d", block.Path, block.StartLine, block.StartCol, block.EndLine, block.EndCol)
}

// namePath is the length-delimited suite path and name, the test's identity within its package.
func (test Test) namePath() string {
	var encoded strings.Builder
	for _, segment := range append(slices.Clone(test.Suite), test.Name) {
		fmt.Fprintf(&encoded, "%d:%s,", len(segment), segment)
	}
	return encoded.String()
}

func (test Test) display() string {
	return strings.Join(append(slices.Clone(test.Suite), test.Name), " > ")
}
