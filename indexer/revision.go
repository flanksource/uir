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
	"gorm.io/gorm"
)

type RevisionOptions struct {
	RootKey      string
	Checkout     string
	Commit       string
	Version      string
	IncludeTests bool
}

// IndexRevision publishes a clean historical snapshot for an already registered module without
// moving its checkout head or changing the checkout's working tree.
func (indexer *Indexer) IndexRevision(ctx context.Context, options RevisionOptions) (result ModuleResult, err error) {
	if indexer == nil || indexer.database == nil {
		return result, errors.New("UIR index database is required")
	}
	var location storage.ModuleLocation
	if err = indexer.database.WithContext(ctx).Table("locations AS location").Select("location.*").
		Joins("JOIN modules AS root ON root.id = location.root_id").
		Where("root.root_key = ? AND location.canonical_path = ?", options.RootKey, options.Checkout).Take(&location).Error; err != nil {
		return result, fmt.Errorf("load registered checkout %q of %q: %w", options.Checkout, options.RootKey, err)
	}
	top, err := revisionGit(ctx, options.Checkout, "rev-parse", "--show-toplevel")
	if err != nil {
		return result, err
	}
	commit, err := revisionGit(ctx, options.Checkout, "rev-parse", "--verify", "--quiet", "--end-of-options", options.Commit+"^{commit}")
	if err != nil {
		return result, err
	}
	relative, err := filepath.Rel(top, options.Checkout)
	if err != nil {
		return result, fmt.Errorf("locate module within checkout: %w", err)
	}
	worktree, cleanup, err := revisionWorktree(ctx, top, commit)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, cleanup()) }()
	modulePath, err := filepath.EvalSymlinks(filepath.Join(worktree, relative))
	if err != nil {
		return result, fmt.Errorf("canonicalize historical module at %q: %w", relative, err)
	}
	roots, err := discoverModules(ctx, modulePath, options.IncludeTests)
	if err != nil {
		return result, err
	}
	for _, root := range roots {
		if root.LocalPath == modulePath && root.RootKey == options.RootKey {
			root.WorktreeState, err = worktreeState(ctx, root.LocalPath, root.GitCommit, root.Files)
			if err != nil {
				return result, err
			}
			if root.WorktreeState != storage.WorktreeClean {
				return result, fmt.Errorf("historical worktree of %s is %s", commit, root.WorktreeState)
			}
			root.Revision = root.GitCommit
			root.Historical = true
			root.ModuleVersion = options.Version
			environment, envErr := readGoEnvironmentWithWork(ctx, root.LocalPath, true)
			if envErr != nil {
				return result, envErr
			}
			root.Variant = environment.Variant
			root.Variant.GoWorkOff = true
			root.WorkFile = ""
			root.ModFile, err = historicalModfile(root.LocalPath)
			if err != nil {
				return result, err
			}
			manifests, manifestErr := readManifests(root.LocalPath, "")
			if manifestErr != nil {
				return result, manifestErr
			}
			root.ContentSetHash = contentSetHash(root.Files, manifests)
			root.ConfigurationHash = configurationHash(options.IncludeTests, root.Variant)
			root.Dependencies, err = readDependencies(ctx, root)
			if err != nil {
				return result, err
			}
			selectedRoots := []discoveredRoot{root}
			if err := indexer.indexVersionedDependencies(ctx, selectedRoots, options.IncludeTests); err != nil {
				return result, err
			}
			root = selectedRoots[0]
			if len(root.Dependencies) == 0 {
				var existing storage.ModuleSnapshot
				query := indexer.database.WithContext(ctx).Where("root_id = ? AND revision = ? AND configuration_hash = ? AND worktree_state = ? AND dependency_set_hash IS NOT NULL",
					location.RootID, commit, root.ConfigurationHash, storage.WorktreeClean)
				if options.Version != "" {
					query = query.Where("module_version = ?", options.Version)
				}
				err = query.Order("completed_at DESC").Take(&existing).Error
				if err == nil {
					return ModuleResult{RootKey: options.RootKey, Location: options.Checkout, SnapshotID: existing.ID.String(), Unchanged: true}, nil
				}
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return result, fmt.Errorf("load clean snapshot of %s: %w", commit, err)
				}
			}
			return indexer.publishRevision(ctx, root, location, options.IncludeTests)
		}
	}
	return result, fmt.Errorf("revision %s has no module %q at %q", commit, options.RootKey, relative)
}

func historicalModfile(directory string) (string, error) {
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
	scratch := filepath.Join(directory, ".tmp")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return "", fmt.Errorf("create historical module scratch: %w", err)
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

func (indexer *Indexer) publishRevision(ctx context.Context, root discoveredRoot, location storage.ModuleLocation, includeTests bool) (result ModuleResult, err error) {
	extraction, err := extractModule(ctx, indexer.loadPackages, root, includeTests)
	if err != nil {
		return result, fmt.Errorf("extract historical module %q: %w", root.RootKey, err)
	}
	err = storage.RetryAllocationConflicts(ctx, indexer.database, func(transaction *gorm.DB) error {
		var stored storage.ModuleRoot
		if loadErr := transaction.Where("id = ?", location.RootID).Take(&stored).Error; loadErr != nil {
			return fmt.Errorf("load root of historical module: %w", loadErr)
		}
		base, loadErr := loadModuleBase(ctx, transaction, stored, location)
		if loadErr != nil {
			return loadErr
		}
		result = ModuleResult{RootKey: root.RootKey, Location: location.CanonicalPath, Files: len(root.Files)}
		snapshot, publishErr := publishSnapshot(ctx, transaction, snapshotPublication{
			root: stored, location: location, base: base, extraction: extraction, startedAt: time.Now().UTC(), preserveHead: true,
		}, &result)
		if publishErr != nil {
			return publishErr
		}
		result.SnapshotID = snapshot.ID.String()
		return nil
	})
	return result, err
}

func revisionWorktree(ctx context.Context, top, commit string) (string, func() error, error) {
	scratch := filepath.Join(top, ".tmp")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return "", nil, fmt.Errorf("create historical snapshot scratch directory: %w", err)
	}
	path, err := os.MkdirTemp(scratch, "uir-revision-")
	if err != nil {
		return "", nil, fmt.Errorf("create historical snapshot worktree: %w", err)
	}
	if _, err := revisionGit(ctx, top, "worktree", "add", "--detach", path, commit); err != nil {
		return "", nil, errors.Join(err, os.Remove(path))
	}
	return path, func() error {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := revisionGit(cleanupCtx, top, "worktree", "remove", path)
		return err
	}, nil
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
