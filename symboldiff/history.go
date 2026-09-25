package symboldiff

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

type GitRef struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

type GitCommit struct {
	Commit     string   `json:"commit"`
	Parents    []string `json:"parents"`
	Subject    string   `json:"subject"`
	AuthoredAt string   `json:"authored_at"`
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
	checkout := scope.locations[0].CanonicalPath
	if location != "" {
		checkout = ""
		for _, candidate := range scope.locations {
			if candidate.CanonicalPath == location {
				checkout = location
				break
			}
		}
		if checkout == "" {
			return History{}, fmt.Errorf("checkout %q is not registered for root %q", location, rootKey)
		}
	}
	result := History{RootKey: rootKey, Location: checkout, Branches: []GitRef{}, Commits: []GitCommit{}, PullRequests: []PullRequestRef{}}
	if result.Branches, err = listBranches(ctx, checkout); err != nil {
		return History{}, err
	}
	if result.Commits, err = listCommits(ctx, checkout, limit); err != nil {
		return History{}, err
	}
	result.PullRequests, result.PullRequestError = listPullRequests(ctx, checkout)
	return result, nil
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
