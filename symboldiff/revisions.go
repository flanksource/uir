package symboldiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

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

// WorktreePatch is git diff --full-index <commit> in checkout: the patch from commit to the
// worktree that DiffPatch reads, with blob ids its locator can resolve.
func WorktreePatch(ctx context.Context, checkout, commit string) (io.Reader, error) {
	output, err := git(ctx, checkout, "diff", "--full-index", "--end-of-options", commit)
	if err != nil {
		return nil, fmt.Errorf("diff commit %s against the worktree of %s: %w", commit, checkout, err)
	}
	return bytes.NewReader(output), nil
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
