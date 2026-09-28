package symboldiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var fullCommitHash = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
var pullRequestRevision = regexp.MustCompile(`^pr:([1-9][0-9]*)$`)
var errCleanSnapshotMissing = errors.New("clean snapshot is missing")

// rootScope is the root being diffed and its registered checkouts, primary first.
type rootScope struct {
	root      storage.ModuleRoot
	locations []storage.ModuleLocation
}

func loadRootScope(ctx context.Context, database *gorm.DB, rootKey string) (rootScope, error) {
	var root storage.ModuleRoot
	if err := database.WithContext(ctx).Where("root_key = ?", rootKey).First(&root).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return rootScope{}, fmt.Errorf("root %q was not found", rootKey)
		}
		return rootScope{}, fmt.Errorf("load root %q: %w", rootKey, err)
	}
	var primary storage.ModulePrimary
	if err := database.WithContext(ctx).Where("root_id = ?", root.ID).First(&primary).Error; err != nil {
		return rootScope{}, fmt.Errorf("load primary location of root %q: %w", rootKey, err)
	}
	var locations []storage.ModuleLocation
	if err := database.WithContext(ctx).Where("root_id = ?", root.ID).Order("canonical_path").Find(&locations).Error; err != nil {
		return rootScope{}, fmt.Errorf("load locations of root %q: %w", rootKey, err)
	}
	sort.SliceStable(locations, func(i, j int) bool {
		return locations[i].ID == primary.LocationID && locations[j].ID != primary.LocationID
	})
	return rootScope{root: root, locations: locations}, nil
}

func (scope rootScope) checkoutPaths() string {
	paths := make([]string, 0, len(scope.locations))
	for _, location := range scope.locations {
		paths = append(paths, location.CanonicalPath)
	}
	return strings.Join(paths, ", ")
}

// resolveCommit returns a full commit hash as is and resolves any other revision through the first
// registered checkout that knows it.
func (scope rootScope) resolveCommit(ctx context.Context, revision string) (string, error) {
	if match := pullRequestRevision.FindStringSubmatch(revision); match != nil {
		number, err := strconv.Atoi(match[1])
		if err != nil {
			return "", fmt.Errorf("invalid pull request %q: %w", revision, err)
		}
		fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		var failures []error
		for _, location := range scope.locations {
			ref := fmt.Sprintf("refs/uir/pr/%d", number)
			if _, err := git(fetchCtx, location.CanonicalPath, "fetch", "--no-tags", "origin", fmt.Sprintf("refs/pull/%d/head:%s", number, ref)); err == nil {
				return verifyCommit(fetchCtx, location.CanonicalPath, ref)
			} else {
				failures = append(failures, err)
			}
		}
		return "", fmt.Errorf("fetch pull request #%d for %q: %w", number, scope.root.RootKey, errors.Join(failures...))
	}
	if fullCommitHash.MatchString(revision) {
		return revision, nil
	}
	for _, location := range scope.locations {
		if resolved, err := verifyCommit(ctx, location.CanonicalPath, revision); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("revision %q does not resolve in any registered checkout of root %q (%s); pass the full commit hash", revision, scope.root.RootKey, scope.checkoutPaths())
}

// checkoutFor returns the first registered checkout, primary first, that contains the commit.
func (scope rootScope) checkoutFor(ctx context.Context, commit string) (string, error) {
	for _, location := range scope.locations {
		if _, err := verifyCommit(ctx, location.CanonicalPath, commit); err == nil {
			return location.CanonicalPath, nil
		}
	}
	return "", fmt.Errorf("no registered checkout of root %q contains commit %s (tried %s)", scope.root.RootKey, commit, scope.checkoutPaths())
}

// selectSnapshot picks the newest clean snapshot of the commit, or the explicit override, which
// must belong to the root and record the commit.
func (scope rootScope) selectSnapshot(ctx context.Context, database *gorm.DB, commit, override, flag string) (storage.ModuleSnapshot, error) {
	if override != "" {
		return scope.overrideSnapshot(ctx, database, commit, override, flag)
	}
	var snapshots []storage.ModuleSnapshot
	if err := database.WithContext(ctx).Where("root_id = ? AND (git_commit = ? OR revision = ?)", scope.root.ID, commit, commit).
		Order("completed_at DESC").Order("started_at DESC").Order("id").Find(&snapshots).Error; err != nil {
		return storage.ModuleSnapshot{}, fmt.Errorf("load snapshots of commit %s: %w", commit, err)
	}
	if len(snapshots) == 0 {
		return storage.ModuleSnapshot{}, fmt.Errorf("%w: commit %s of root %q has no snapshot; check it out in a registered location and index it first", errCleanSnapshotMissing, commit, scope.root.RootKey)
	}
	dirty := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.WorktreeState == storage.WorktreeClean {
			return snapshot, nil
		}
		dirty = append(dirty, snapshot.ID.String())
	}
	return storage.ModuleSnapshot{}, fmt.Errorf("%w: commit %s of root %q has only dirty snapshots (%s), whose bytes may not be the commit's bytes; select one explicitly with %s",
		errCleanSnapshotMissing,
		commit, scope.root.RootKey, strings.Join(dirty, ", "), flag)
}

func (scope rootScope) overrideSnapshot(ctx context.Context, database *gorm.DB, commit, override, flag string) (storage.ModuleSnapshot, error) {
	id, err := uuid.Parse(override)
	if err != nil {
		return storage.ModuleSnapshot{}, fmt.Errorf("%s %q is not a snapshot UUID: %w", flag, override, err)
	}
	var snapshot storage.ModuleSnapshot
	if err := database.WithContext(ctx).Where("id = ?", id).First(&snapshot).Error; err != nil {
		return storage.ModuleSnapshot{}, fmt.Errorf("load %s snapshot %s: %w", flag, id, err)
	}
	switch {
	case snapshot.RootID != scope.root.ID:
		return storage.ModuleSnapshot{}, fmt.Errorf("%s snapshot %s does not belong to root %q", flag, id, scope.root.RootKey)
	case snapshot.GitCommit != commit && snapshot.Revision != commit:
		return storage.ModuleSnapshot{}, fmt.Errorf("%s snapshot %s records revision %s, not %s", flag, id, snapshot.Revision, commit)
	}
	return snapshot, nil
}

func verifyCommit(ctx context.Context, directory, revision string) (string, error) {
	output, err := git(ctx, directory, "rev-parse", "--verify", "--quiet", revision+"^{commit}")
	return strings.TrimSpace(string(output)), err
}

func git(ctx context.Context, directory string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, args...)...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s in %q: %w: %s", strings.Join(args, " "), directory, err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}
