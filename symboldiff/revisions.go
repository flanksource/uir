package symboldiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

// pullRequestToken is a pr:N revision inside a range spec; a colon cannot appear in a ref name.
var pullRequestToken = regexp.MustCompile(`\bpr:[1-9][0-9]*\b`)

// Resolution is the two commits a diff's revisions name or, with Worktree, the one commit that is
// compared against the worktree of Checkout, as git diff <rev> does.
type Resolution struct {
	From     string
	To       string
	Worktree bool
	Checkout string
}

// ResolveRevisions resolves options.Revisions as git diff reads them, in the root's registered
// checkouts with options.Location first. An external root's revisions are not Git's: they are taken
// literally, as <from>..<to> or two revisions.
func ResolveRevisions(ctx context.Context, database *gorm.DB, options Options) (Resolution, error) {
	scope, err := loadScope(ctx, database, options)
	if err != nil {
		return Resolution{}, err
	}
	return scope.resolveRevisions(ctx, options.Revisions)
}

// RevisionSnapshotOptions select the snapshot of one revision of a root.
type RevisionSnapshotOptions struct {
	RootKey string
	// Location is the registered checkout the revision resolves in first.
	Location string
	Revision string
	// Snapshot is an explicit snapshot UUID, which must record Revision, in place of its newest clean one.
	Snapshot string
	// SnapshotFlag names the flag that sets Snapshot, for the errors that ask for it.
	SnapshotFlag string
}

// RevisionSnapshot resolves one revision as uir diff resolves each side of a range, and selects the
// snapshot uir diff would compare for it.
func RevisionSnapshot(ctx context.Context, database *gorm.DB, options RevisionSnapshotOptions) (storage.ModuleSnapshot, error) {
	scope, err := loadScope(ctx, database, Options{RootKey: options.RootKey, Location: options.Location})
	if err != nil {
		return storage.ModuleSnapshot{}, err
	}
	if strings.TrimSpace(options.Revision) == "" {
		return storage.ModuleSnapshot{}, errors.New("a revision is required to select its snapshot")
	}
	commit, err := scope.resolveCommit(ctx, options.Revision)
	if err != nil {
		return storage.ModuleSnapshot{}, err
	}
	return scope.selectSnapshot(ctx, database, commit, options.Snapshot, options.SnapshotFlag)
}

// ResolveInRepository resolves one revision spec as uir diff does, in the git repository holding
// directory rather than a registered checkout: a single revision is compared against that repository's
// worktree. Pull request heads are fetched only through a registered root, so pr:N is refused.
func ResolveInRepository(ctx context.Context, directory, spec string) (Resolution, error) {
	switch {
	case strings.HasPrefix(spec, "-"):
		return Resolution{}, fmt.Errorf("revision %q is an option, not a revision", spec)
	case pullRequestToken.MatchString(spec):
		return Resolution{}, fmt.Errorf("revision %q names a pull request, which resolves only in a registered checkout (run uir add . or pass --root)", spec)
	}
	output, err := git(ctx, directory, "rev-parse", "--show-toplevel")
	if err != nil {
		return Resolution{}, fmt.Errorf("revision %q is resolved in the git repository holding %q: %w", spec, directory, err)
	}
	toplevel := strings.TrimSpace(string(output))
	output, err = git(ctx, toplevel, "rev-parse", "--revs-only", "--end-of-options", spec)
	if err != nil {
		return Resolution{}, err
	}
	commits := strings.Fields(string(output))
	if len(commits) == 0 {
		return Resolution{}, fmt.Errorf("revision %q does not resolve in %s", spec, toplevel)
	}
	return interpretRevisions(ctx, toplevel, spec, spec, commits)
}

// WorktreePatchOptions name the checkout whose worktree is compared and the commit it is compared with.
type WorktreePatchOptions struct {
	// Checkout is the top level of the worktree.
	Checkout string
	Commit   string
	// Untracked adds the worktree's untracked files that are not ignored, as new files.
	Untracked bool
}

// WorktreePatch is git diff --full-index --no-renames <commit> in the checkout: the patch from the
// commit to the worktree that DiffPatch reads, with blob ids its locator can resolve. A pure rename
// carries no blob ids, and symbols pair across files on their own. With Untracked, the diff runs
// against a temporary copy of the index that marks every untracked file intent-to-add, so they appear
// as new files while the checkout's own index is never written.
func WorktreePatch(ctx context.Context, options WorktreePatchOptions) (io.Reader, error) {
	run := gitRun{directory: options.Checkout}
	if options.Untracked {
		directory, err := os.MkdirTemp("", "uir-index-*")
		if err != nil {
			return nil, fmt.Errorf("create a temporary index for the untracked files of %s: %w", options.Checkout, err)
		}
		defer os.RemoveAll(directory) //nolint:errcheck // a leftover temporary index is harmless
		index := filepath.Join(directory, "index")
		if err := markUntracked(ctx, options.Checkout, index); err != nil {
			return nil, err
		}
		run.env = []string{"GIT_INDEX_FILE=" + index}
	}
	output, err := run.output(ctx, "diff", "--full-index", "--no-renames", "--end-of-options", options.Commit)
	if err != nil {
		return nil, fmt.Errorf("diff commit %s against the worktree of %s: %w", options.Commit, options.Checkout, err)
	}
	return bytes.NewReader(output), nil
}

// markUntracked writes index as a copy of the checkout's index with its untracked, non-ignored files
// added intent-to-add.
func markUntracked(ctx context.Context, checkout, index string) error {
	output, err := git(ctx, checkout, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return err
	}
	content, err := os.ReadFile(strings.TrimSpace(string(output)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("read the index of %s: %w", checkout, err)
	default:
		if err := os.WriteFile(index, content, 0o600); err != nil {
			return fmt.Errorf("copy the index of %s: %w", checkout, err)
		}
	}
	run := gitRun{directory: checkout, env: []string{"GIT_INDEX_FILE=" + index}}
	untracked, err := run.output(ctx, "ls-files", "-z", "--others", "--exclude-standard")
	if err == nil && len(untracked) > 0 {
		run.stdin = string(untracked)
		_, err = run.output(ctx, "add", "--intent-to-add", "--pathspec-from-file=-", "--pathspec-file-nul")
	}
	if err != nil {
		return fmt.Errorf("mark the untracked files of %s intent-to-add: %w", checkout, err)
	}
	return nil
}

// loadScope is the root's scope with options.Location, when set, as the preferred checkout.
func loadScope(ctx context.Context, database *gorm.DB, options Options) (rootScope, error) {
	if database == nil {
		return rootScope{}, errors.New("diff: database is required")
	}
	scope, err := loadRootScope(ctx, database, options.RootKey)
	if err != nil || options.Location == "" {
		return scope, err
	}
	return scope.preferring(options.Location)
}

func (scope rootScope) resolveRevisions(ctx context.Context, revisions []string) (Resolution, error) {
	for _, revision := range revisions {
		if strings.TrimSpace(revision) == "" {
			return Resolution{}, fmt.Errorf("diff revisions %q include an empty revision", revisions)
		}
	}
	switch {
	case len(revisions) == 2:
		from, err := scope.resolveCommit(ctx, revisions[0])
		if err != nil {
			return Resolution{}, err
		}
		to, err := scope.resolveCommit(ctx, revisions[1])
		return Resolution{From: from, To: to}, err
	case len(revisions) == 1 && scope.external:
		return scope.literalRange(revisions[0])
	case len(revisions) == 1:
		return scope.resolveSpec(ctx, revisions[0])
	}
	return Resolution{}, fmt.Errorf("uir diff takes one revision spec or two revisions, not %d (%q)", len(revisions), revisions)
}

// literalRange splits an external root's <from>..<to>; its revisions have no Git range syntax.
func (scope rootScope) literalRange(spec string) (Resolution, error) {
	from, to, found := strings.Cut(spec, "..")
	if !found || strings.HasPrefix(to, ".") || from == "" || to == "" {
		return Resolution{}, scope.errExternal(fmt.Sprintf("%q must be <from>..<to> or two revisions", spec))
	}
	return Resolution{From: from, To: to}, nil
}

// resolveSpec expands one revision spec with git rev-parse in the first registered checkout that
// knows it, after replacing its pr:N revisions with the fetched pull request heads.
func (scope rootScope) resolveSpec(ctx context.Context, spec string) (Resolution, error) {
	if strings.HasPrefix(spec, "-") {
		return Resolution{}, fmt.Errorf("revision %q is an option, not a revision", spec)
	}
	expanded, err := scope.substitutePullRequests(ctx, spec)
	if err != nil {
		return Resolution{}, err
	}
	var failures []error
	for _, location := range scope.locations {
		output, err := git(ctx, location.CanonicalPath, "rev-parse", "--revs-only", "--end-of-options", expanded)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if commits := strings.Fields(string(output)); len(commits) > 0 {
			return interpretRevisions(ctx, location.CanonicalPath, spec, expanded, commits)
		}
	}
	return Resolution{}, fmt.Errorf("revision %q does not resolve in any registered checkout of root %q (%s): %w",
		spec, scope.root.RootKey, scope.checkoutPaths(), errors.Join(failures...))
}

func (scope rootScope) substitutePullRequests(ctx context.Context, spec string) (string, error) {
	var failure error
	expanded := pullRequestToken.ReplaceAllStringFunc(spec, func(token string) string {
		if failure != nil {
			return token
		}
		commit, err := scope.resolveCommit(ctx, token)
		failure = err
		return commit
	})
	return expanded, failure
}

// interpretRevisions reads git rev-parse's expansion of spec: one positive and one negative commit is
// a range; two positives and one negative is A...B, compared from the merge base to B; a lone
// positive is a single revision when spec itself names one commit, compared against the worktree.
func interpretRevisions(ctx context.Context, checkout, spec, expanded string, commits []string) (Resolution, error) {
	var positive, negative []string
	for _, commit := range commits {
		if hash, found := strings.CutPrefix(commit, "^"); found {
			negative = append(negative, hash)
		} else {
			positive = append(positive, commit)
		}
	}
	unsupported := fmt.Errorf("uir diff compares two commits, or one commit with the worktree; %q names %s", spec, strings.Join(commits, " "))
	switch {
	case len(positive) == 1 && len(negative) == 0:
		commit, err := verifyCommit(ctx, checkout, expanded)
		if err != nil {
			return Resolution{}, unsupported
		}
		return Resolution{From: commit, Worktree: true, Checkout: checkout}, nil
	case len(negative) == 1 && (len(positive) == 1 || len(positive) == 2):
		from, err := verifyCommit(ctx, checkout, negative[0])
		if err != nil {
			return Resolution{}, errors.Join(unsupported, err)
		}
		to, err := verifyCommit(ctx, checkout, positive[0])
		return Resolution{From: from, To: to}, err
	}
	return Resolution{}, unsupported
}
