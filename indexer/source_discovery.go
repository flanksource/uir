package indexer

import (
	"context"
	"fmt"

	"github.com/flanksource/commons-db/git"
	"github.com/flanksource/uir/storage"
)

type discoveryOptions struct {
	IncludeTests bool
	Source       *git.SourceInfo
}

func discoveryVisibility(ctx context.Context, directory string, source *git.SourceInfo) (git.VisiblePaths, error) {
	if source == nil {
		return git.ListVisiblePaths(ctx, directory)
	}
	if !pathWithin(directory, source.Dir) {
		return git.VisiblePaths{}, fmt.Errorf("discovery path %q is outside prepared source %q", directory, source.Dir)
	}
	// The prepared tree already contains only selected Git entries; enclosing ignore rules do not apply.
	return git.VisiblePaths{}, nil
}

func setSourceIdentity(root *discoveredRoot, info git.SourceInfo) {
	root.Historical = true
	root.GitCommit, root.Revision = info.TargetCommit, info.TargetCommit
	root.WorktreeState = storage.WorktreeClean
	if root.LocalPath == info.Dir {
		root.Kind = "git"
	}
	if info.Kind == git.SourceStaged {
		root.GitCommit = info.BaseCommit
		root.Revision = "index-" + info.TreeID
		root.WorktreeState = storage.WorktreeDirty
	}
}
