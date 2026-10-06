package query

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/graph"
	"golang.org/x/mod/module"
)

// The keywords a call graph exclusion pattern may be besides a package pattern.
const (
	// ExcludeNone alone excludes nothing.
	ExcludeNone = "none"
	// ExcludeStd matches the standard library.
	ExcludeStd = "std"
	// ExcludeBuiltin matches Go's predeclared functions, grouped as the builtin package.
	ExcludeBuiltin = "builtin"
	// ExcludeExternal matches every package outside the modules in the query scope.
	ExcludeExternal = "external"
)

// DefaultGraphExclusions are the patterns a call graph excludes when none are given: the calls that
// are rarely what a reader follows and would crowd out the rest.
var DefaultGraphExclusions = []string{ExcludeStd, ExcludeBuiltin, "gorm.io/..."}

// GraphPackage is one package a call graph reached while walking, drawn or excluded: the facet a
// viewer switches exclusions on and off by.
type GraphPackage struct {
	Path string `json:"path"`
	// External is true for a package outside the modules in the query scope.
	External bool `json:"external"`
	// Nodes counts the package's nodes the graph drew or excluded.
	Nodes int `json:"nodes"`
	// Excluded is true when the effective patterns exclude the package. Its root is still drawn.
	Excluded bool `json:"excluded"`
}

// builtinGroup is the group of Go's predeclared functions, which have no package path. It is the
// path of Go's builtin pseudo-package, so a builtin is matched by the keyword and the path alike.
const builtinGroup = "builtin"

// graphPackageFacts is what an exclusion pattern is matched against.
type graphPackageFacts struct {
	path     string
	module   string
	builtin  bool
	external bool
}

// graphExclusions are parsed exclusion patterns.
type graphExclusions struct {
	keywords map[string]bool
	paths    map[string]bool
	// trees are the bases of the /... patterns.
	trees []string
}

func invalidExclusion(format string, arguments ...any) error {
	return &InvalidQueryError{Message: fmt.Sprintf(format, arguments...), Hint: fmt.Sprintf(
		"Give exclude as comma-separated package patterns: an import path, a path ending in /... for it and every package below it, or the keywords %s, %s and %s. %s alone excludes nothing; with no patterns the defaults %s apply.",
		ExcludeStd, ExcludeBuiltin, ExcludeExternal, ExcludeNone, strings.Join(DefaultGraphExclusions, ", "))}
}

// parseGraphExclusions parses effective patterns: a keyword, an import path, or an import path
// followed by /..., as Go package patterns are written. none must be the only pattern.
func parseGraphExclusions(patterns []string) (graphExclusions, error) {
	exclusions := graphExclusions{keywords: map[string]bool{}, paths: map[string]bool{}}
	seen := map[string]bool{}
	for _, pattern := range patterns {
		switch {
		case pattern == "":
			return graphExclusions{}, invalidExclusion("an exclude pattern is empty")
		case seen[pattern]:
			return graphExclusions{}, invalidExclusion("exclude pattern %q is given twice", pattern)
		case pattern == ExcludeNone && len(patterns) > 1:
			return graphExclusions{}, invalidExclusion("%s excludes nothing and cannot be combined with other patterns", ExcludeNone)
		}
		seen[pattern] = true
		switch pattern {
		case ExcludeNone:
			continue
		case ExcludeStd, ExcludeBuiltin, ExcludeExternal:
			exclusions.keywords[pattern] = true
			continue
		}
		base, tree := strings.CutSuffix(pattern, "/...")
		if err := module.CheckImportPath(base); err != nil {
			return graphExclusions{}, invalidExclusion("exclude pattern %q is not an import path or a path ending in /...: %v", pattern, err)
		}
		if tree {
			exclusions.trees = append(exclusions.trees, base)
		} else {
			exclusions.paths[base] = true
		}
	}
	return exclusions, nil
}

func (exclusions graphExclusions) excludes(pkg graphPackageFacts) bool {
	switch {
	case exclusions.keywords[ExcludeStd] && pkg.module == "std",
		exclusions.keywords[ExcludeBuiltin] && pkg.builtin,
		exclusions.keywords[ExcludeExternal] && pkg.external,
		exclusions.paths[pkg.path]:
		return true
	}
	return slices.ContainsFunc(exclusions.trees, func(base string) bool {
		return pkg.path == base || strings.HasPrefix(pkg.path, base+"/")
	})
}

// scopePackages are the package paths of the documents active in the selected snapshots.
func scopePackages(index *indexContext) map[string]bool {
	declared := map[string]bool{}
	for _, scope := range index.scopes {
		for _, document := range scope.documents {
			declared[document.Document.PackagePath] = true
		}
	}
	return declared
}

// graphPackages is the package facet of a built graph: each package of a drawn or excluded node with
// how many it has, ordered by that count, most first, then by path.
func graphPackages(built *graph.Graph, facts map[string]graphPackageFacts, exclusions graphExclusions) []GraphPackage {
	counts := map[string]int{}
	for _, node := range built.Nodes {
		counts[node.Group]++
	}
	for group, excluded := range built.Omitted.Excluded {
		counts[group] += excluded
	}
	packages := make([]GraphPackage, 0, len(counts))
	for path, nodes := range counts {
		pkg, known := facts[path]
		if !known {
			panic(fmt.Sprintf("query: graph package %q was never described by the graph source", path))
		}
		packages = append(packages, GraphPackage{Path: path, External: pkg.external, Nodes: nodes, Excluded: exclusions.excludes(pkg)})
	}
	slices.SortFunc(packages, func(a, b GraphPackage) int {
		return cmp.Or(cmp.Compare(b.Nodes, a.Nodes), cmp.Compare(a.Path, b.Path))
	})
	return packages
}
