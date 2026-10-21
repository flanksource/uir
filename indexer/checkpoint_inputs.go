package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/commons-db/git"
)

func observeCheckpoint(ctx context.Context, directory string) error {
	checkpoint, _ := ctx.Value(checkpointContextKey{}).(*indexCheckpoint)
	if checkpoint == nil {
		return nil
	}
	checkpoint.mu.Lock()
	defer checkpoint.mu.Unlock()
	if !checkpoint.eligible {
		return nil
	}
	fingerprint, err := git.WorktreeFingerprint(ctx, directory)
	if errors.Is(err, git.ErrNotRepository) {
		checkpoint.eligible = false
		return nil
	}
	if err != nil {
		return err
	}
	if _, exists := checkpoint.inputs.Repositories[fingerprint.Root]; !exists {
		checkpoint.inputs.Repositories[fingerprint.Root] = fingerprint.SHA
	}
	return nil
}

func observeCheckpointWorkspace(ctx context.Context, root discoveredRoot) error {
	checkpoint, _ := ctx.Value(checkpointContextKey{}).(*indexCheckpoint)
	if checkpoint == nil {
		return nil
	}
	modules, err := diskModules(root)
	if err != nil {
		return err
	}
	modules[root.RootKey] = root.LocalPath
	for _, directory := range modules {
		if err := observeCheckpoint(ctx, directory); err != nil {
			return err
		}
		if err := checkpoint.watch(filepath.Join(directory, "go.mod"), filepath.Join(directory, "go.sum")); err != nil {
			return err
		}
	}
	if root.WorkFile != "" {
		return checkpoint.watch(root.WorkFile, root.WorkFile+".sum")
	}
	return nil
}

func (checkpoint *indexCheckpoint) watch(paths ...string) error {
	checkpoint.mu.Lock()
	defer checkpoint.mu.Unlock()
	for _, path := range paths {
		if _, exists := checkpoint.inputs.Files[path]; exists {
			continue
		}
		metadata, err := checkpointFile(path)
		if err != nil {
			return err
		}
		checkpoint.inputs.Files[path] = metadata
	}
	return nil
}

func checkpointFile(path string) (string, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d:%d", info.ModTime().UnixNano(), info.Size(), info.Mode()), nil
}

func (checkpoint *indexCheckpoint) refresh(ctx context.Context, inputs checkpointInputs) (string, error) {
	current := checkpointInputs{Repositories: map[string]string{}, Files: map[string]string{}}
	for directory := range inputs.Repositories {
		fingerprint, err := git.WorktreeFingerprint(ctx, directory)
		if err != nil {
			return "", err
		}
		current.Repositories[directory] = fingerprint.SHA
	}
	for path := range inputs.Files {
		metadata, err := checkpointFile(path)
		if err != nil {
			return "", err
		}
		current.Files[path] = metadata
	}
	var err error
	if inputs.Catalog != "" {
		current.Catalog, err = checkpoint.catalog(ctx)
		if err != nil {
			return "", err
		}
	}
	return checkpointDigest(current)
}
