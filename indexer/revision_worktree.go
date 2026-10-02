package indexer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/uir/storage"
	"golang.org/x/mod/modfile"
)

type preparedRevision struct {
	root     discoveredRoot
	location storage.ModuleLocation
	cleanup  func() error
}

// prepareRevision checks out options.Commit of a registered checkout into a scratch worktree and
// discovers the module there as a clean historical root. The caller runs cleanup once it is done.
func (indexer *Indexer) prepareRevision(ctx context.Context, options RevisionOptions) (prepared preparedRevision, err error) {
	if indexer == nil || indexer.database == nil {
		return prepared, errors.New("UIR index database is required")
	}
	var location storage.ModuleLocation
	if err = indexer.database.WithContext(ctx).Table("locations AS location").Select("location.*").
		Joins("JOIN modules AS root ON root.id = location.root_id").
		Where("root.root_key = ? AND location.canonical_path = ?", options.RootKey, options.Checkout).Take(&location).Error; err != nil {
		return prepared, fmt.Errorf("load registered checkout %q of %q: %w", options.Checkout, options.RootKey, err)
	}
	top, err := revisionGit(ctx, options.Checkout, "rev-parse", "--show-toplevel")
	if err != nil {
		return prepared, err
	}
	commit, err := revisionGit(ctx, options.Checkout, "rev-parse", "--verify", "--quiet", "--end-of-options", options.Commit+"^{commit}")
	if err != nil {
		return prepared, err
	}
	relative, err := filepath.Rel(top, options.Checkout)
	if err != nil {
		return prepared, fmt.Errorf("locate module within checkout: %w", err)
	}
	scratch, err := revisionWorktree(ctx, top, commit)
	if err != nil {
		return prepared, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, scratch.cleanup())
		}
	}()
	modulePath, err := filepath.EvalSymlinks(filepath.Join(scratch.worktree, relative))
	if err != nil {
		return prepared, fmt.Errorf("canonicalize historical module at %q: %w", relative, err)
	}
	roots, err := discoverModules(ctx, modulePath, options.IncludeTests)
	if err != nil {
		return prepared, err
	}
	for _, root := range roots {
		if root.LocalPath == modulePath && root.RootKey == options.RootKey {
			if root, err = historicalRoot(ctx, root, options, scratch.manifests); err != nil {
				return prepared, err
			}
			return preparedRevision{root: root, location: location, cleanup: scratch.cleanup}, nil
		}
	}
	return prepared, fmt.Errorf("revision %s has no module %q at %q", commit, options.RootKey, relative)
}

// historicalRoot turns a root discovered in a historical worktree into a clean, workspace-free root
// whose go.mod drops local replacements, written to manifests.
func historicalRoot(ctx context.Context, root discoveredRoot, options RevisionOptions, manifests string) (discoveredRoot, error) {
	var err error
	root.WorktreeState, err = worktreeState(ctx, root.LocalPath, root.GitCommit, root.Files)
	if err != nil {
		return root, err
	}
	if root.WorktreeState != storage.WorktreeClean {
		return root, fmt.Errorf("historical worktree of %s is %s", root.GitCommit, root.WorktreeState)
	}
	root.Revision = root.GitCommit
	root.Historical = true
	root.ModuleVersion = options.Version
	environment, err := readGoEnvironmentWithWork(ctx, root.LocalPath, true)
	if err != nil {
		return root, err
	}
	root.Variant = environment.Variant
	root.Variant.GoWorkOff = true
	root.WorkFile = ""
	if root.ModFile, err = historicalModfile(root.LocalPath, manifests); err != nil {
		return root, err
	}
	manifestHashes, err := readManifests(root.LocalPath, "")
	if err != nil {
		return root, err
	}
	root.ContentSetHash = contentSetHash(root.Files, manifestHashes)
	root.ConfigurationHash = configurationHash(options.IncludeTests, root.Variant)
	root.Dependencies, err = readDependencies(ctx, root)
	return root, err
}

// historicalModfile writes the module's go.mod without local replacements, and its go.sum, into
// scratch, which lies outside the worktree, and returns the manifest path for -modfile.
func historicalModfile(directory, scratch string) (string, error) {
	path := filepath.Join(directory, "go.mod")
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read historical go.mod: %w", err)
	}
	manifest, err := modfile.Parse(path, content, nil)
	if err != nil {
		return "", fmt.Errorf("parse historical go.mod: %w", err)
	}
	for _, replacement := range append([]*modfile.Replace(nil), manifest.Replace...) {
		if replacement.New.Version == "" {
			if err := manifest.DropReplace(replacement.Old.Path, replacement.Old.Version); err != nil {
				return "", fmt.Errorf("remove historical local replacement %q: %w", replacement.Old.Path, err)
			}
		}
	}
	formatted, err := manifest.Format()
	if err != nil {
		return "", fmt.Errorf("format historical go.mod: %w", err)
	}
	alternate := filepath.Join(scratch, "uir-historical.mod")
	if err := os.WriteFile(alternate, formatted, 0o644); err != nil {
		return "", fmt.Errorf("write historical module manifest: %w", err)
	}
	if sums, err := os.ReadFile(filepath.Join(directory, "go.sum")); err == nil {
		if err := os.WriteFile(filepath.Join(scratch, "uir-historical.sum"), sums, 0o644); err != nil {
			return "", fmt.Errorf("write historical module sums: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read historical module sums: %w", err)
	}
	return alternate, nil
}

// revisionScratch is a temporary directory outside the checkout holding the detached worktree of one
// historical commit and, beside it, the manifests its load reads, so nothing is written into either
// the checkout or the worktree.
type revisionScratch struct {
	worktree  string
	manifests string
	cleanup   func() error
}

// revisionWorktree adds a detached worktree of commit in a new scratch directory. Its cleanup force
// removes the worktree, prunes the repository's worktree records, and deletes the scratch directory.
func revisionWorktree(ctx context.Context, top, commit string) (revisionScratch, error) {
	scratch, err := os.MkdirTemp("", "uir-revision-")
	if err != nil {
		return revisionScratch{}, fmt.Errorf("create historical snapshot scratch directory: %w", err)
	}
	prepared := revisionScratch{worktree: filepath.Join(scratch, "worktree"), manifests: filepath.Join(scratch, "manifests")}
	if err := os.Mkdir(prepared.manifests, 0o755); err != nil {
		return revisionScratch{}, errors.Join(fmt.Errorf("create historical manifest directory: %w", err), os.RemoveAll(scratch))
	}
	if _, err := revisionGit(ctx, top, "worktree", "add", "--detach", prepared.worktree, commit); err != nil {
		return revisionScratch{}, errors.Join(err, os.RemoveAll(scratch))
	}
	prepared.cleanup = func() error {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, removeErr := revisionGit(cleanupCtx, top, "worktree", "remove", "--force", prepared.worktree)
		_, pruneErr := revisionGit(cleanupCtx, top, "worktree", "prune")
		return errors.Join(removeErr, pruneErr, os.RemoveAll(scratch))
	}
	return prepared, nil
}

func revisionGit(ctx context.Context, directory string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, args...)...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s in %q: %w: %s", strings.Join(args, " "), directory, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(output)), nil
}
