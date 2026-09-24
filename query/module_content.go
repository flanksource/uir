package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/flanksource/uir/storage"
)

type ModuleSourceContent struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	Origin     string `json:"origin"`
	Revision   string `json:"revision"`
	SnapshotID string `json:"snapshot_id"`
}

var pinnedGitRevision = regexp.MustCompile(`^[a-fA-F0-9]{40,64}$`)

func (pipeline *Pipeline) ReadModuleSource(ctx context.Context, snapshotID, path string) (ModuleSourceContent, error) {
	if pipeline == nil || pipeline.database == nil {
		return ModuleSourceContent{}, errors.New("UIR query database is required")
	}
	if !filepath.IsLocal(path) {
		return ModuleSourceContent{}, fmt.Errorf("source path %q must be local to its module root", path)
	}
	scopes, err := pipeline.moduleScopes(ctx, ModuleScopeOptions{SnapshotID: snapshotID}, false)
	if err != nil {
		return ModuleSourceContent{}, err
	}
	scope := scopes[0]
	sources, err := storage.EffectiveSources(ctx, pipeline.database, scope.snapshot.ID)
	if err != nil {
		return ModuleSourceContent{}, err
	}
	source, ok := sources[filepath.ToSlash(path)]
	if !ok {
		return ModuleSourceContent{}, fmt.Errorf("source %q is not in snapshot %s", path, snapshotID)
	}
	modulePath := filepath.Join(scope.location.CanonicalPath, filepath.FromSlash(path))
	resolvedRoot, err := filepath.EvalSymlinks(scope.location.CanonicalPath)
	if err != nil {
		return ModuleSourceContent{}, fmt.Errorf("resolve module location %q: %w", scope.location.CanonicalPath, err)
	}
	resolvedFile, fileErr := filepath.EvalSymlinks(modulePath)
	if fileErr == nil {
		relative, relErr := filepath.Rel(resolvedRoot, resolvedFile)
		if relErr != nil || !filepath.IsLocal(relative) {
			return ModuleSourceContent{}, fmt.Errorf("source %q escapes module location %q", path, resolvedRoot)
		}
		content, readErr := os.ReadFile(resolvedFile)
		if readErr != nil {
			return ModuleSourceContent{}, fmt.Errorf("read source %q: %w", path, readErr)
		}
		if contentHash(content) == source.ContentHash {
			return ModuleSourceContent{Path: path, Content: string(content), Origin: "local", Revision: scope.snapshot.Revision, SnapshotID: snapshotID}, nil
		}
	} else if !errors.Is(fileErr, os.ErrNotExist) {
		return ModuleSourceContent{}, fmt.Errorf("resolve source %q: %w", path, fileErr)
	}
	if !pinnedGitRevision.MatchString(scope.snapshot.Revision) {
		return ModuleSourceContent{}, fmt.Errorf("source %q has changed since snapshot %s and no pinned Git revision can recover it", path, snapshotID)
	}
	command := exec.CommandContext(ctx, "git", "-C", resolvedRoot, "rev-parse", "--show-toplevel")
	repoTop, err := command.Output()
	if err != nil {
		return ModuleSourceContent{}, fmt.Errorf("locate Git repository for source %q: %w", path, err)
	}
	repoRoot, err := filepath.EvalSymlinks(strings.TrimSpace(string(repoTop)))
	if err != nil {
		return ModuleSourceContent{}, fmt.Errorf("resolve Git repository for source %q: %w", path, err)
	}
	relative, err := filepath.Rel(repoRoot, modulePath)
	if err != nil || !filepath.IsLocal(relative) {
		return ModuleSourceContent{}, fmt.Errorf("source %q escapes Git repository %q", path, repoRoot)
	}
	command = exec.CommandContext(ctx, "git", "-C", repoRoot, "cat-file", "blob", scope.snapshot.Revision+":"+filepath.ToSlash(relative))
	content, err := command.Output()
	if err != nil {
		return ModuleSourceContent{}, fmt.Errorf("read Git source %q at %s: %w", path, scope.snapshot.Revision, err)
	}
	if contentHash(content) != source.ContentHash {
		return ModuleSourceContent{}, fmt.Errorf("git source %q at %s does not match indexed content hash", path, scope.snapshot.Revision)
	}
	return ModuleSourceContent{Path: path, Content: string(content), Origin: "git", Revision: scope.snapshot.Revision, SnapshotID: snapshotID}, nil
}

func contentHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
