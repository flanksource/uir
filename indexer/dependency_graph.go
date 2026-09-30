package indexer

import (
	"context"
	"fmt"
	"path/filepath"
)

type localDependencyGraph struct {
	roots  []discoveredRoot
	byPath map[string]int
	groups [][]int
}

func discoverLocalDependencyGraph(ctx context.Context, roots []discoveredRoot, includeTests bool) (localDependencyGraph, error) {
	graph := localDependencyGraph{roots: roots, byPath: make(map[string]int, len(roots))}
	for index, root := range roots {
		graph.byPath[root.LocalPath] = index
	}
	for index := 0; index < len(graph.roots); index++ {
		for dependencyIndex := range graph.roots[index].Dependencies {
			dependency := &graph.roots[index].Dependencies[dependencyIndex]
			if dependency.LocalDir == "" {
				continue
			}
			path, err := filepath.EvalSymlinks(dependency.LocalDir)
			if err != nil {
				dependency.Edge.UnresolvedReason = fmt.Sprintf("resolve local dependency %q: %v", dependency.LocalDir, err)
				continue
			}
			dependency.LocalDir = path
			if _, found := graph.byPath[path]; found {
				continue
			}
			discovered, err := discoverModules(ctx, path, includeTests)
			if err != nil {
				return localDependencyGraph{}, fmt.Errorf("discover local dependency %q: %w", path, err)
			}
			var target *discoveredRoot
			for rootIndex := range discovered {
				if discovered[rootIndex].LocalPath == path {
					target = &discovered[rootIndex]
					break
				}
			}
			if target == nil {
				return localDependencyGraph{}, fmt.Errorf("local dependency %q has no module at its root", path)
			}
			target.Dependencies, err = readDependencies(ctx, *target)
			if err != nil {
				return localDependencyGraph{}, fmt.Errorf("read dependencies of %q: %w", path, err)
			}
			graph.byPath[path] = len(graph.roots)
			graph.roots = append(graph.roots, *target)
		}
	}
	graph.groups = graph.components()
	return graph, nil
}

func (graph localDependencyGraph) components() [][]int {
	return dependencyComponents(len(graph.roots), func(node int) []int {
		var targets []int
		for _, dependency := range graph.roots[node].Dependencies {
			if target, found := graph.byPath[dependency.LocalDir]; found {
				targets = append(targets, target)
			}
		}
		return targets
	})
}

func (graph localDependencyGraph) cyclic() bool {
	for _, group := range graph.groups {
		if len(group) > 1 {
			return true
		}
		for _, dependency := range graph.roots[group[0]].Dependencies {
			if dependency.LocalDir == graph.roots[group[0]].LocalPath {
				return true
			}
		}
	}
	return false
}
