package symboldiff

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

type GitRef struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

// GitCommit is one commit of the listed checkout. The snapshot fields are set when the commit has a
// snapshot in that checkout: the newest clean one, else the newest.
type GitCommit struct {
	Commit              string     `json:"commit"`
	Parents             []string   `json:"parents"`
	Subject             string     `json:"subject"`
	AuthoredAt          string     `json:"authored_at"`
	SnapshotID          string     `json:"snapshot_id,omitempty"`
	SnapshotReason      string     `json:"snapshot_reason,omitempty"`
	SnapshotCompletedAt *time.Time `json:"snapshot_completed_at,omitempty"`
}

type PullRequestRef struct {
	Number int    `json:"number"`
	Commit string `json:"commit"`
}

type History struct {
	RootKey          string           `json:"root_key"`
	Location         string           `json:"location"`
	Branches         []GitRef         `json:"branches"`
	Commits          []GitCommit      `json:"commits"`
	PullRequests     []PullRequestRef `json:"pull_requests"`
	PullRequestError string           `json:"pull_request_error,omitempty"`
}

type CommitOptions struct {
	RootKey      string
	Commit       string
	Visibility   Visibility
	Stat         bool
	IncludeTests bool
	// TaskContext bounds the module-index runs for the commit and its parent; see Options.
	TaskContext context.Context
}

// DiffCommit compares a commit with its first parent and indexes missing clean snapshots.
func DiffCommit(ctx context.Context, database *gorm.DB, options CommitOptions) (Result, error) {
	scope, err := loadRootScope(ctx, database, options.RootKey)
	if err != nil {
		return Result{}, err
	}
	commit, err := scope.resolveCommit(ctx, options.Commit)
	if err != nil {
		return Result{}, err
	}
	checkout, err := scope.checkoutFor(ctx, commit)
	if err != nil {
		return Result{}, err
	}
	output, err := git(ctx, checkout, "rev-list", "--parents", "-n", "1", commit)
	if err != nil {
		return Result{}, err
	}
	parents := strings.Fields(string(output))
	if len(parents) == 0 || parents[0] != commit {
		return Result{}, fmt.Errorf("git returned no parent record for commit %s", commit)
	}
	if len(parents) == 1 {
		return Result{}, fmt.Errorf("commit %s has no parent to compare", commit)
	}
	return Diff(ctx, database, Options{RootKey: options.RootKey, From: parents[1], To: commit,
		Visibility: options.Visibility, Stat: options.Stat, AutoIndex: true, IncludeTests: options.IncludeTests, TaskContext: options.TaskContext})
}

func ListHistory(ctx context.Context, database *gorm.DB, rootKey, location string, limit int) (History, error) {
	if limit < 1 || limit > 200 {
		return History{}, fmt.Errorf("history limit %d must be from 1 through 200", limit)
	}
	scope, err := loadRootScope(ctx, database, rootKey)
	if err != nil {
		return History{}, err
	}
	if len(scope.locations) == 0 {
		return History{}, fmt.Errorf("root %q has no registered checkout", rootKey)
	}
	if location != "" {
		if scope, err = scope.preferring(location); err != nil {
			return History{}, err
		}
	}
	checkout := scope.locations[0]
	result := History{RootKey: rootKey, Location: checkout.CanonicalPath, Branches: []GitRef{}, Commits: []GitCommit{}, PullRequests: []PullRequestRef{}}
	if result.Branches, err = listBranches(ctx, checkout.CanonicalPath); err != nil {
		return History{}, err
	}
	if result.Commits, err = listCommits(ctx, checkout.CanonicalPath, limit); err != nil {
		return History{}, err
	}
	if err := markIndexedCommits(ctx, database, checkout, result.Commits); err != nil {
		return History{}, err
	}
	result.PullRequests, result.PullRequestError = listPullRequests(ctx, checkout.CanonicalPath)
	return result, nil
}

// markIndexedCommits sets the snapshot of each commit that has one in the checkout, matched by
// git_commit: the newest clean snapshot, else the newest.
func markIndexedCommits(ctx context.Context, database *gorm.DB, checkout storage.ModuleLocation, commits []GitCommit) error {
	if len(commits) == 0 {
		return nil
	}
	hashes := make([]string, 0, len(commits))
	for _, commit := range commits {
		hashes = append(hashes, commit.Commit)
	}
	var snapshots []storage.ModuleSnapshot
	if err := database.WithContext(ctx).Select("id", "git_commit", "reason", "completed_at", "worktree_state").
		Where("location_id = ? AND git_commit IN ?", checkout.ID, hashes).
		Order(fmt.Sprintf("CASE WHEN worktree_state = '%s' THEN 0 ELSE 1 END, completed_at DESC, ordinal DESC", storage.WorktreeClean)).
		Find(&snapshots).Error; err != nil {
		return fmt.Errorf("load snapshots of the commits of %q: %w", checkout.CanonicalPath, err)
	}
	newest := make(map[string]storage.ModuleSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		if _, seen := newest[snapshot.GitCommit]; !seen {
			newest[snapshot.GitCommit] = snapshot
		}
	}
	for index := range commits {
		snapshot, indexed := newest[commits[index].Commit]
		if !indexed {
			continue
		}
		completed := snapshot.CompletedAt
		commits[index].SnapshotID, commits[index].SnapshotReason, commits[index].SnapshotCompletedAt = snapshot.ID.String(), string(snapshot.Reason), &completed
	}
	return nil
}

func listBranches(ctx context.Context, checkout string) ([]GitRef, error) {
	output, err := git(ctx, checkout, "for-each-ref", "--format=%(refname:short)%00%(objectname)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	branches := []GitRef{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		name, commit, found := strings.Cut(line, "\x00")
		if !found {
			return nil, fmt.Errorf("invalid git branch row %q", line)
		}
		if !strings.HasSuffix(name, "/HEAD") {
			branches = append(branches, GitRef{Name: name, Commit: commit})
		}
	}
	return branches, nil
}

func listCommits(ctx context.Context, checkout string, limit int) ([]GitCommit, error) {
	output, err := git(ctx, checkout, "log", "-n", strconv.Itoa(limit), "--format=%H%x00%P%x00%aI%x00%s")
	if err != nil {
		return nil, err
	}
	commits := []GitCommit{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) != 4 {
			return nil, fmt.Errorf("invalid git log row %q", line)
		}
		parents := strings.Fields(parts[1])
		if parents == nil {
			parents = []string{}
		}
		commits = append(commits, GitCommit{Commit: parts[0], Parents: parents, AuthoredAt: parts[2], Subject: parts[3]})
	}
	return commits, nil
}

func listPullRequests(ctx context.Context, checkout string) ([]PullRequestRef, string) {
	requests := []PullRequestRef{}
	if _, err := git(ctx, checkout, "remote", "get-url", "origin"); err != nil {
		return requests, "origin remote is not configured"
	}
	remoteCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := git(remoteCtx, checkout, "ls-remote", "--refs", "origin", "refs/pull/*/head")
	if err != nil {
		return requests, err.Error()
	}
	for _, line := range strings.FieldsFunc(string(output), func(r rune) bool { return r == '\n' }) {
		commit, ref, found := strings.Cut(line, "\t")
		if !found {
			return nil, fmt.Sprintf("invalid pull request ref %q", line)
		}
		number, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(ref, "refs/pull/"), "/head"))
		if err != nil {
			return nil, fmt.Sprintf("invalid pull request ref %q: %v", ref, err)
		}
		requests = append(requests, PullRequestRef{Number: number, Commit: commit})
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].Number > requests[j].Number })
	if len(requests) > 50 {
		requests = requests[:50]
	}
	return requests, ""
}
