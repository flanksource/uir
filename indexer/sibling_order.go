package indexer

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// orderAfter orders the nodes 0..count-1 in input order, except that a node comes after every remaining
// node it must follow and, where that still leaves a choice, after every remaining node it should
// follow. The must relation has to be acyclic. A cycle of should edges is broken at the first node of
// a cycle nothing else is waiting on, so a node outside the cycle that the cycle should follow still
// comes first.
func orderAfter(count int, must, should func(node, other int) bool) []int {
	remaining := make([]int, count)
	for index := range remaining {
		remaining[index] = index
	}
	follows := func(rule func(int, int) bool, node int) bool {
		return slices.ContainsFunc(remaining, func(other int) bool { return other != node && rule(node, other) })
	}
	ordered := make([]int, 0, count)
	for len(remaining) > 0 {
		next := slices.IndexFunc(remaining, func(node int) bool { return !follows(must, node) && !follows(should, node) })
		if next < 0 {
			sink := sinkComponents(remaining, should)
			next = slices.IndexFunc(remaining, func(node int) bool { return sink[node] && !follows(must, node) })
		}
		if next < 0 {
			next = slices.IndexFunc(remaining, func(node int) bool { return !follows(must, node) })
		}
		if next < 0 {
			panic(fmt.Sprintf("orderAfter: the must relation is cyclic among nodes %v", remaining))
		}
		ordered = append(ordered, remaining[next])
		remaining = slices.Delete(remaining, next, next+1)
	}
	return ordered
}

// sinkComponents is the members of every strongly connected component of the should relation among nodes
// that has no should edge to a node outside it.
func sinkComponents(nodes []int, should func(node, other int) bool) map[int]bool {
	neighbors := func(position int) []int {
		var targets []int
		for target, other := range nodes {
			if target != position && should(nodes[position], other) {
				targets = append(targets, target)
			}
		}
		return targets
	}
	sinks := map[int]bool{}
	for _, component := range dependencyComponents(len(nodes), neighbors) {
		leaves := slices.ContainsFunc(component, func(position int) bool {
			return slices.ContainsFunc(neighbors(position), func(target int) bool { return !slices.Contains(component, target) })
		})
		if !leaves {
			for _, position := range component {
				sinks[nodes[position]] = true
			}
		}
	}
	return sinks
}

func never(int, int) bool { return false }

// siblingOrder orders roots so a workspace sibling comes before the roots that import it. Forced roots
// are never reused, so they keep their order.
func (check *headCheck) siblingOrder(roots []discoveredRoot, force bool) ([]int, error) {
	if force {
		return orderAfter(len(roots), never, never), nil
	}
	imports, err := siblingEdges(roots, check.siblingsOf)
	if err != nil {
		return nil, err
	}
	return orderAfter(len(roots), never, func(node, other int) bool { return imports[node][other] }), nil
}

// siblingEdges maps each root's index to the indexes of the other roots whose packages it imports as
// workspace siblings, by the locations locate finds for a root's sibling imports.
func siblingEdges(roots []discoveredRoot, locate func(discoveredRoot) (map[string]string, error)) ([]map[int]bool, error) {
	byLocation := make(map[string]int, len(roots))
	for index, root := range roots {
		byLocation[root.LocalPath] = index
	}
	edges := make([]map[int]bool, len(roots))
	for index, root := range roots {
		siblings, err := locate(root)
		if err != nil {
			return nil, fmt.Errorf("order module %q at %q: %w", root.RootKey, root.LocalPath, err)
		}
		edges[index] = map[int]bool{}
		for _, location := range siblings {
			if target, found := byLocation[location]; found && target != index {
				edges[index][target] = true
			}
		}
	}
	return edges, nil
}

// targetImports maps each target's index to the indexes of the other targets whose module paths own a
// package its Go files import, reading only the files' import declarations.
func targetImports(targets []moduleTarget) ([]map[int]bool, error) {
	modules := map[string][]int{}
	for index, target := range targets {
		modules[target.rootKey] = append(modules[target.rootKey], index)
	}
	edges := make([]map[int]bool, len(targets))
	for index, target := range targets {
		paths, err := locationImports(target.location)
		if err != nil {
			return nil, fmt.Errorf("order module %q at %q: %w", target.rootKey, target.location, err)
		}
		edges[index] = map[int]bool{}
		for _, path := range paths {
			for _, other := range modules[owningModule(modules, path)] {
				if other != index && targets[other].rootKey != target.rootKey {
					edges[index][other] = true
				}
			}
		}
	}
	return edges, nil
}

// locationImports is every import path of the Go files of the module at location, nested modules,
// vendor and testdata directories excluded.
func locationImports(location string) ([]string, error) {
	fileSet := token.NewFileSet()
	var imports []string
	err := filepath.WalkDir(location, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return skipForeignDirectory(location, current, entry.Name())
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		parsed, err := parser.ParseFile(fileSet, current, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("read imports of %q: %w", current, err)
		}
		for _, spec := range parsed.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return fmt.Errorf("import %s in %q: %w", spec.Path.Value, current, err)
			}
			imports = append(imports, path)
		}
		return nil
	})
	return imports, err
}

func skipForeignDirectory(location, current, name string) error {
	if current == location {
		return nil
	}
	if name == "vendor" || shouldSkipRootDiscovery(name) {
		return filepath.SkipDir
	}
	goMod := filepath.Join(current, "go.mod")
	_, err := os.Stat(goMod)
	switch {
	case err == nil:
		return filepath.SkipDir
	case os.IsNotExist(err):
		return nil
	default:
		return fmt.Errorf("stat %q: %w", goMod, err)
	}
}
