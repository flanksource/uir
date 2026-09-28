package query

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
)

func (pipeline *Pipeline) SuggestSelectors(ctx context.Context, prefix string, options ModuleScopeOptions) (ItemsWithWarnings[string], error) {
	if pipeline == nil || pipeline.database == nil {
		return ItemsWithWarnings[string]{}, fmt.Errorf("UIR query database is required")
	}
	kind, pattern, found := strings.Cut(prefix, ":")
	if !found || len(prefix) > maximumSelectorPattern || strings.ContainsAny(prefix, " \t\r\n") {
		return ItemsWithWarnings[string]{}, fmt.Errorf("invalid selector prefix %q", prefix)
	}
	if options.Limit == 0 {
		options.Limit = 20
	}
	if options.Limit < 1 || options.Limit > 100 {
		return ItemsWithWarnings[string]{}, fmt.Errorf("suggestion limit must be between 1 and 100, got %d", options.Limit)
	}
	if options.Location != "" && options.SnapshotID != "" {
		return ItemsWithWarnings[string]{}, fmt.Errorf("location and snapshot selectors are mutually exclusive")
	}
	selection, err := pipeline.moduleScopes(ctx, options, false)
	scopes := selection.scopes
	if err != nil {
		return ItemsWithWarnings[string]{}, err
	}
	choices := map[string]bool{}
	switch kind {
	case "mod", "module":
		for _, scope := range scopes {
			if strings.HasPrefix(scope.root.RootKey, pattern) {
				choices[prefix[:len(kind)+1]+scope.root.RootKey] = true
			}
		}
	case "pkg", "package":
		modulePattern, relativePrefix, scoped := strings.Cut(pattern, ":")
		if kind == "package" && scoped {
			return ItemsWithWarnings[string]{}, fmt.Errorf("package selector does not accept a module pattern")
		}
		var moduleGlob selectorGlob
		if scoped {
			moduleGlob, err = compileSelectorGlob(modulePattern)
			if err != nil {
				return ItemsWithWarnings[string]{}, fmt.Errorf("invalid package module pattern: %w", err)
			}
		}
		for _, scope := range scopes {
			if scoped && !moduleGlob.matches(scope.root.RootKey) {
				continue
			}
			var packages []storage.PackageCoverage
			if err := pipeline.database.WithContext(ctx).Where("snapshot_id = ?", scope.snapshot.ID).Find(&packages).Error; err != nil {
				return ItemsWithWarnings[string]{}, fmt.Errorf("load packages of snapshot %s: %w", scope.snapshot.ID, err)
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
						choices[kind+":"+scope.root.RootKey+":"+candidate] = true
					}
				} else if strings.HasPrefix(candidate, pattern) {
					choices[kind+":"+candidate] = true
				}
			}
		}
	case "func", "method", "var", "type", "field", "struct", "all", "path":
		if strings.ContainsAny(pattern, "*?\\:") {
			return ItemsWithWarnings[string]{Items: []string{}, Warnings: selection.warnings}, nil
		}
		base, err := newIndexContext(ctx, pipeline.database, scopes)
		if err != nil {
			return ItemsWithWarnings[string]{}, err
		}
		selected, err := newCompactIndex(base).resolveSelector(ctx, Selector{Kind: kind, Pattern: pattern + "*"})
		if err != nil {
			return ItemsWithWarnings[string]{}, err
		}
		for _, symbol := range selected.symbols {
			choices[kind+":"+symbol.QueryName] = true
		}
	default:
		return ItemsWithWarnings[string]{}, fmt.Errorf("unknown selector kind %q", kind)
	}
	result := make([]string, 0, len(choices))
	for choice := range choices {
		result = append(result, choice)
	}
	slices.Sort(result)
	if len(result) > options.Limit {
		result = result[:options.Limit]
	}
	return ItemsWithWarnings[string]{Items: result, Warnings: selection.warnings}, nil
}
