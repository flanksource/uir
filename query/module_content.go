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
	"gorm.io/gorm"
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
	selection, err := pipeline.moduleScopes(ctx, ModuleScopeOptions{SnapshotID: snapshotID}, false)
	scopes := selection.scopes
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
	content, origin, err := readVerifiedSource(ctx, pipeline.database, scope, verifiedSource{path: path, hash: source.ContentHash, snapshot: snapshotID})
	if err != nil {
		return ModuleSourceContent{}, err
	}
	return ModuleSourceContent{Path: path, Content: string(content), Origin: origin, Revision: scope.snapshot.Revision, SnapshotID: snapshotID}, nil
}

// verifiedSource names the bytes readVerifiedSource must find: a module-relative path whose content
// has the SHA-256 the snapshot indexed. snapshot is how the caller spelled the snapshot, for errors.
type verifiedSource struct {
	path     string
	hash     string
	snapshot string
}

// readVerifiedSource returns the indexed bytes of a source and where they came from: the local file
// while it still has the indexed hash, else the stored blob of a dirty or non-Git snapshot, else the
// pinned Git blob. An external location is a producer's URI, not a checkout, so its sources are only
// the blobs its publication stored. Bytes with any other hash are never returned.
func readVerifiedSource(ctx context.Context, database *gorm.DB, scope moduleScope, source verifiedSource) ([]byte, string, error) {
	if scope.location.Kind == storage.LocationExternal {
		content, found, err := readStoredSource(ctx, database, source)
		if err == nil && !found {
			err = fmt.Errorf("source %q of external snapshot %s has no stored bytes", source.path, source.snapshot)
		}
		return content, "snapshot", err
	}
	modulePath := filepath.Join(scope.location.CanonicalPath, filepath.FromSlash(source.path))
	resolvedRoot, err := filepath.EvalSymlinks(scope.location.CanonicalPath)
	if err != nil {
		return nil, "", fmt.Errorf("resolve module location %q: %w", scope.location.CanonicalPath, err)
	}
	content, found, err := readLocalSource(resolvedRoot, modulePath, source)
	if err != nil || found {
		return content, "local", err
	}
	if scope.snapshot.WorktreeState != storage.WorktreeClean {
		content, found, err = readStoredSource(ctx, database, source)
		if err != nil || found {
			return content, "snapshot", err
		}
	}
	content, err = readGitSource(ctx, scope, resolvedRoot, modulePath, source)
	return content, "git", err
}

// readLocalSource reads the checkout's file and reports whether it still has the indexed hash.
func readLocalSource(resolvedRoot, modulePath string, source verifiedSource) ([]byte, bool, error) {
	resolvedFile, err := filepath.EvalSymlinks(modulePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("resolve source %q: %w", source.path, err)
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedFile)
	if err != nil || !filepath.IsLocal(relative) {
		return nil, false, fmt.Errorf("source %q escapes module location %q", source.path, resolvedRoot)
	}
	content, err := os.ReadFile(resolvedFile)
	if err != nil {
		return nil, false, fmt.Errorf("read source %q: %w", source.path, err)
	}
	return content, contentHash(content) == source.hash, nil
}

// readStoredSource reads the blob stored under the indexed hash and reports whether there is one.
func readStoredSource(ctx context.Context, database *gorm.DB, source verifiedSource) ([]byte, bool, error) {
	var blob storage.SourceBlob
	err := database.WithContext(ctx).Where("content_hash = ?", source.hash).Take(&blob).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load stored source %q: %w", source.path, err)
	}
	if contentHash(blob.Content) != source.hash {
		return nil, false, fmt.Errorf("stored source %q does not match indexed content hash", source.path)
	}
	return blob.Content, true, nil
}

func readGitSource(ctx context.Context, scope moduleScope, resolvedRoot, modulePath string, source verifiedSource) ([]byte, error) {
	commit := scope.snapshot.GitCommit
	if commit == "" && scope.snapshot.WorktreeState == storage.WorktreeClean {
		commit = scope.snapshot.Revision
	}
	if !pinnedGitRevision.MatchString(commit) {
		return nil, fmt.Errorf("source %q has changed since snapshot %s and no pinned Git revision can recover it", source.path, source.snapshot)
	}
	command := exec.CommandContext(ctx, "git", "-C", resolvedRoot, "rev-parse", "--show-toplevel")
	repoTop, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("locate Git repository for source %q: %w", source.path, err)
	}
	repoRoot, err := filepath.EvalSymlinks(strings.TrimSpace(string(repoTop)))
	if err != nil {
		return nil, fmt.Errorf("resolve Git repository for source %q: %w", source.path, err)
	}
	relative, err := filepath.Rel(repoRoot, modulePath)
	if err != nil || !filepath.IsLocal(relative) {
		return nil, fmt.Errorf("source %q escapes Git repository %q", source.path, repoRoot)
	}
	command = exec.CommandContext(ctx, "git", "-C", repoRoot, "cat-file", "blob", commit+":"+filepath.ToSlash(relative))
	content, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("read Git source %q at %s: %w", source.path, commit, err)
	}
	if contentHash(content) != source.hash {
		return nil, fmt.Errorf("git source %q at %s does not match indexed content hash", source.path, commit)
	}
	return content, nil
}

func contentHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
