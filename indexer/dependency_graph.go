package indexer

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
)

type localDependencyGraph struct {
	roots  []discoveredRoot
	byPath map[string]int
	groups [][]int
}

func discoverLocalDependencyGraph(ctx context.Context, roots []discoveredRoot, includeTests bool) (localDependencyGraph, error) {
	ctx, memo := ensureRunMemo(ctx)
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
			target, err := memo.localDependencyRoot(ctx, path, includeTests)
			if err != nil {
				return localDependencyGraph{}, err
			}
			if target.Dependencies, err = memo.dependencies(ctx, target); err != nil {
				return localDependencyGraph{}, fmt.Errorf("read dependencies of %q: %w", path, err)
			}
			graph.byPath[path] = len(graph.roots)
			graph.roots = append(graph.roots, target)
		}
	}
	graph.groups = graph.components()
	if !graph.cyclic() {
		return graph, nil
	}
	return graph, graph.orderSiblingsFirst()
}

// orderSiblingsFirst orders the groups so a group follows the groups its members depend on and, where
// that leaves a choice, the groups whose members they import as workspace siblings. Sibling imports
// only order the groups: they are not dependency edges, so they never join or split a group.
func (graph *localDependencyGraph) orderSiblingsFirst() error {
	edges, err := siblingEdges(graph.roots, siblingLocations)
	if err != nil {
		return err
	}
	groupOf := make([]int, len(graph.roots))
	for group, members := range graph.groups {
		for _, member := range members {
			groupOf[member] = group
		}
	}
	linked := func(edges func(int) []int) func(node, other int) bool {
		return func(node, other int) bool {
			return slices.ContainsFunc(graph.groups[node], func(member int) bool {
				return slices.ContainsFunc(edges(member), func(target int) bool { return groupOf[target] == other })
			})
		}
	}
	order := orderAfter(len(graph.groups), linked(graph.dependencyTargets), linked(func(member int) []int { return slices.Collect(maps.Keys(edges[member])) }))
	groups := make([][]int, 0, len(order))
	for _, index := range order {
		groups = append(groups, graph.groups[index])
	}
	graph.groups = groups
	return nil
}

func (graph localDependencyGraph) components() [][]int {
	return dependencyComponents(len(graph.roots), graph.dependencyTargets)
}

// dependencyTargets is the indexes of the roots a root depends on through a local directory.
func (graph localDependencyGraph) dependencyTargets(node int) []int {
	var targets []int
	for _, dependency := range graph.roots[node].Dependencies {
		if target, found := graph.byPath[dependency.LocalDir]; found {
			targets = append(targets, target)
		}
	}
	return targets
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
