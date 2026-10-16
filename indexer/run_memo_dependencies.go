package indexer

import (
	"context"
	"fmt"
	"slices"
)

// localDependencyMemo is what a run has read of its modules' local dependency graphs: each local
// dependency discovered at its own root, and each module's dependencies under the inputs that decide
// them. Every target of a run walks the graph of its local dependencies, which the targets share.
type localDependencyMemo struct {
	roots map[moduleMemoKey]discoveredRoot
	reads map[dependencyReadKey][]dependencyObservation
}

// dependencyReadKey is what readDependencies reads: the module's go.mod and the go.work it is under,
// both in its content set hash, under its build configuration, through its go.mod or an alternate one.
type dependencyReadKey struct {
	location, contentSetHash, configurationHash, modFile, workFile string
	historical                                                     bool
}

func newLocalDependencyMemo() localDependencyMemo {
	return localDependencyMemo{roots: map[moduleMemoKey]discoveredRoot{}, reads: map[dependencyReadKey][]dependencyObservation{}}
}

// dependencies is root's dependencies, read once per run for the inputs that decide them and counted
// as reads in the running phase. Callers link the edges they get, so each gets its own copy.
func (memo *runMemo) dependencies(ctx context.Context, root discoveredRoot) ([]dependencyObservation, error) {
	if root.ContentSetHash == "" || root.ConfigurationHash == "" {
		return nil, fmt.Errorf("read dependencies of module %q: it was not discovered", root.LocalPath)
	}
	key := dependencyReadKey{
		location: root.LocalPath, contentSetHash: root.ContentSetHash, configurationHash: root.ConfigurationHash,
		modFile: root.ModFile, workFile: root.WorkFile, historical: root.Historical,
	}
	memo.mu.Lock()
	read, found := memo.local.reads[key]
	memo.mu.Unlock()
	if found {
		return slices.Clone(read), nil
	}
	PhaseCount(ctx, "reads", 1)
	read, err := readDependencies(ctx, root)
	if err != nil {
		return nil, err
	}
	memo.mu.Lock()
	memo.local.reads[key] = slices.Clone(read)
	memo.mu.Unlock()
	return read, nil
}

// localDependencyRoot is the module rooted at path, a local dependency, discovered once per run.
func (memo *runMemo) localDependencyRoot(ctx context.Context, path string, includeTests bool) (discoveredRoot, error) {
	key := moduleMemoKey{location: path, includeTests: includeTests}
	memo.mu.Lock()
	root, found := memo.local.roots[key]
	memo.mu.Unlock()
	if !found {
		var located bool
		var err error
		if root, located, err = discoverModuleAt(ctx, path, path, includeTests); err != nil {
			return discoveredRoot{}, fmt.Errorf("discover local dependency %q: %w", path, err)
		}
		if !located {
			return discoveredRoot{}, fmt.Errorf("local dependency %q has no module at its root", path)
		}
		memo.mu.Lock()
		memo.local.roots[key] = root
		memo.mu.Unlock()
	}
	root.Files = slices.Clone(root.Files)
	root.Dependencies = nil
	return root, nil
}
