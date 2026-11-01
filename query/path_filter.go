package query

import (
	"fmt"
	"slices"
)

// CompilePathFilter uses SQL MATCH globs on slash-separated relative file paths.
// Includes are alternatives; exclusions win. An empty path is a run diagnostic.
func CompilePathFilter(include, exclude []string) (func(string) bool, error) {
	groups := [2][]selectorGlob{}
	for i, patterns := range [][]string{include, exclude} {
		for _, pattern := range patterns {
			if pattern == "" {
				return nil, fmt.Errorf("file filter glob must not be empty")
			}
			glob, err := compileSQLGlob(pattern)
			if err != nil {
				return nil, fmt.Errorf("file filter %q: %w", pattern, err)
			}
			groups[i] = append(groups[i], glob)
		}
	}
	return func(path string) bool {
		if path == "" {
			return true
		}
		matches := func(glob selectorGlob) bool { return glob.matches(path) }
		return (len(groups[0]) == 0 || slices.ContainsFunc(groups[0], matches)) && !slices.ContainsFunc(groups[1], matches)
	}, nil
}
