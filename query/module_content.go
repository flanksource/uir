package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

// ReadModuleSource reads one source of a snapshot, as ReadModuleSources reads many.
func (pipeline *Pipeline) ReadModuleSource(ctx context.Context, snapshotID, path string) (ModuleSourceContent, error) {
	contents, err := pipeline.ReadModuleSources(ctx, ModuleSourcesOptions{SnapshotID: snapshotID, Paths: []string{path}})
	if err != nil {
		return ModuleSourceContent{}, err
	}
	return contents[0], nil
}

// verifiedSource names the bytes readVerifiedSources must find: a module-relative path whose content
// has the SHA-256 the snapshot indexed. snapshot is how the caller spelled the snapshot, for errors.
type verifiedSource struct {
	path     string
	hash     string
	snapshot string
}

// sourceRead is one source's indexed bytes and where they came from, or why they could not be recovered.
type sourceRead struct {
	content []byte
	origin  string
	err     error
}

// readVerifiedSources returns the indexed bytes of each source of one scope and where they came from:
// the local file while it still has the indexed hash, else the stored blob of a dirty or non-Git
// snapshot, else the pinned Git blob, every Git blob through one git cat-file process. An external
// location is a producer's URI, not a checkout, so its sources are only the blobs its publication
// stored. Bytes with any other hash are never returned; a source that cannot be recovered carries the
// error naming it, and the others are still read.
func readVerifiedSources(ctx context.Context, database *gorm.DB, scope moduleScope, sources []verifiedSource) []sourceRead {
	reads := make([]sourceRead, len(sources))
	if scope.location.Kind == storage.LocationExternal {
		for index, source := range sources {
			content, err := storage.ReadSourceBlob(ctx, database, source.hash)
			if err != nil {
				err = fmt.Errorf("source %q of external snapshot %s: %w", source.path, source.snapshot, err)
			}
			reads[index] = sourceRead{content: content, origin: "snapshot", err: err}
		}
		return reads
	}
	resolvedRoot, err := filepath.EvalSymlinks(scope.location.CanonicalPath)
	if err != nil {
		for index := range reads {
			reads[index].err = fmt.Errorf("resolve module location %q: %w", scope.location.CanonicalPath, err)
		}
		return reads
	}
	var pending []int
	for index, source := range sources {
		read, fromGit := readCheckoutSource(ctx, database, scope, resolvedRoot, source)
		reads[index] = read
		if fromGit {
			pending = append(pending, index)
		}
	}
	(&gitSources{scope: scope, resolvedRoot: resolvedRoot, sources: sources, reads: reads}).read(ctx, pending)
	return reads
}

// readCheckoutSource reads a source from the checkout or the snapshot's stored blob, reporting true
// when neither holds the indexed bytes and only the pinned Git blob can.
func readCheckoutSource(ctx context.Context, database *gorm.DB, scope moduleScope, resolvedRoot string, source verifiedSource) (sourceRead, bool) {
	modulePath := filepath.Join(scope.location.CanonicalPath, filepath.FromSlash(source.path))
	content, found, err := readLocalSource(resolvedRoot, modulePath, source)
	if err != nil || found {
		return sourceRead{content: content, origin: "local", err: err}, false
	}
	if scope.snapshot.WorktreeState == storage.WorktreeClean {
		return sourceRead{}, true
	}
	content, err = storage.ReadSourceBlob(ctx, database, source.hash)
	if err == nil {
		return sourceRead{content: content, origin: "snapshot"}, false
	}
	if !errors.Is(err, storage.ErrSourceBlobNotStored) {
		return sourceRead{err: fmt.Errorf("source %q of snapshot %s: %w", source.path, source.snapshot, err)}, false
	}
	return sourceRead{}, true
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

func contentHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
