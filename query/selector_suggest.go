package query

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
)

func (pipeline *Pipeline) SuggestSelectors(ctx context.Context, prefix string, options ModuleScopeOptions) ([]string, error) {
	if pipeline == nil || pipeline.database == nil {
		return nil, fmt.Errorf("UIR query database is required")
	}
	kind, pattern, found := strings.Cut(prefix, ":")
	if !found || len(prefix) > maximumSelectorPattern || strings.ContainsAny(prefix, " \t\r\n") {
		return nil, fmt.Errorf("invalid selector prefix %q", prefix)
	}
	if options.Limit == 0 {
		options.Limit = 20
	}
	if options.Limit < 1 || options.Limit > 100 {
		return nil, fmt.Errorf("suggestion limit must be between 1 and 100, got %d", options.Limit)
	}
	if options.Location != "" && options.SnapshotID != "" {
		return nil, fmt.Errorf("location and snapshot selectors are mutually exclusive")
	}
	scopes, err := pipeline.moduleScopes(ctx, options, false)
	if err != nil {
		return nil, err
	}
	choices := map[string]bool{}
	switch kind {
	case "mod":
		for _, scope := range scopes {
			if strings.HasPrefix(scope.root.RootKey, pattern) {
				choices[prefix[:len(kind)+1]+scope.root.RootKey] = true
			}
		}
	case "pkg":
		modulePattern, relativePrefix, scoped := strings.Cut(pattern, ":")
		var moduleGlob selectorGlob
		if scoped {
			moduleGlob, err = compileSelectorGlob(modulePattern)
			if err != nil {
				return nil, fmt.Errorf("invalid package module pattern: %w", err)
			}
		}
		for _, scope := range scopes {
			if scoped && !moduleGlob.matches(scope.root.RootKey) {
				continue
			}
			var packages []storage.PackageCoverage
			if err := pipeline.database.WithContext(ctx).Where("snapshot_id = ?", scope.snapshot.ID).Find(&packages).Error; err != nil {
				return nil, fmt.Errorf("load packages of snapshot %s: %w", scope.snapshot.ID, err)
			}
			for _, pkg := range packages {
				candidate := pkg.PackagePath
				if scoped {
					candidate = "."
					if pkg.PackagePath != scope.root.RootKey {
						if !strings.HasPrefix(pkg.PackagePath, scope.root.RootKey+"/") {
							continue
						}
						candidate = strings.TrimPrefix(pkg.PackagePath, scope.root.RootKey+"/")
					}
					if strings.HasPrefix(candidate, relativePrefix) {
						choices["pkg:"+scope.root.RootKey+":"+candidate] = true
					}
				} else if strings.HasPrefix(candidate, pattern) {
					choices["pkg:"+candidate] = true
				}
			}
		}
	case "func", "field", "struct":
		if strings.ContainsAny(pattern, "*?\\:") {
			return []string{}, nil
		}
		base, err := newIndexContext(ctx, pipeline.database, scopes)
		if err != nil {
			return nil, err
		}
		selected, err := newCompactIndex(base).resolveSelector(ctx, Selector{Kind: kind, Pattern: pattern + "*"})
		if err != nil {
			return nil, err
		}
		for _, symbol := range selected.symbols {
			choices[kind+":"+symbol.QueryName] = true
		}
	default:
		return nil, fmt.Errorf("unknown selector kind %q", kind)
	}
	result := make([]string, 0, len(choices))
	for choice := range choices {
		result = append(result, choice)
	}
	slices.Sort(result)
	if len(result) > options.Limit {
		result = result[:options.Limit]
	}
	return result, nil
}
