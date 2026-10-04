package query

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
)

// SuggestSelectors completes a partially typed selector or Entity:Field reference. A value may be
// quoted, and may then contain whitespace; a suggested value is quoted when it needs to be.
func (pipeline *Pipeline) SuggestSelectors(ctx context.Context, prefix string, options ModuleScopeOptions) (ItemsWithWarnings[string], error) {
	if pipeline == nil || pipeline.database == nil {
		return ItemsWithWarnings[string]{}, fmt.Errorf("UIR query database is required")
	}
	parts, err := selectorPrefixParts(prefix)
	if err != nil {
		return ItemsWithWarnings[string]{}, err
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
	if err != nil {
		return ItemsWithWarnings[string]{}, err
	}
	choices, err := pipeline.selectorChoices(ctx, prefix, parts, selection.scopes)
	if err != nil {
		return ItemsWithWarnings[string]{}, err
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

// selectorPrefixParts splits a partial selector into its kind, or entity, and at least one value. An
// unquoted part may not contain whitespace.
func selectorPrefixParts(prefix string) ([]selectorPart, error) {
	if len(prefix) > maximumSelectorPattern {
		return nil, fmt.Errorf("invalid selector prefix %q: longer than %d bytes", prefix, maximumSelectorPattern)
	}
	parts, err := splitSelector(prefix, true)
	if err != nil {
		return nil, fmt.Errorf("invalid selector prefix %q: %w", prefix, err)
	}
	if len(parts) < 2 || parts[0].text == "" {
		return nil, fmt.Errorf("invalid selector prefix %q: expected <kind>:<value> or <entity>:<field>", prefix)
	}
	for _, part := range parts {
		if !part.quoted && strings.ContainsAny(part.text, " \t\r\n") {
			return nil, fmt.Errorf("invalid selector prefix %q: quote a value that contains whitespace", prefix)
		}
	}
	return parts, nil
}

func (pipeline *Pipeline) selectorChoices(ctx context.Context, prefix string, parts []selectorPart, scopes []moduleScope) (map[string]bool, error) {
	kind := parts[0].text
	if parts[0].quoted {
		kind = ""
	}
	value, literal := globLiteral(parts[1].text)
	switch kind {
	case "mod", "module":
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid selector prefix %q: %s takes one value", prefix, kind)
		}
		choices := map[string]bool{}
		for _, scope := range scopes {
			if literal && strings.HasPrefix(scope.root.RootKey, value) {
				choices[kind+":"+quoteSelectorValue(scope.root.RootKey)] = true
			}
		}
		return choices, nil
	case "pkg", "package":
		return pipeline.suggestPackages(ctx, kind, parts[1:], scopes)
	case "func", "method", "var", "type", "field", "struct", "all", "path":
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid selector prefix %q: %s takes one value", prefix, kind)
		}
		return pipeline.suggestSymbols(ctx, Selector{Kind: kind, Pattern: parts[1].text}, scopes, func(symbol ModuleSymbol) string {
			return kind + ":" + quoteSelectorValue(symbol.QueryName)
		})
	case "kind":
		return pipeline.suggestKinds(ctx, prefix, parts[1:], scopes)
	}
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid selector prefix %q: an Entity:Field reference has one field", prefix)
	}
	owner := parts[0].text
	if parts[0].quoted {
		owner = `"` + owner + `"`
	}
	return pipeline.suggestSymbols(ctx, Selector{Kind: "field", Owner: parts[0].text, Pattern: parts[1].text}, scopes, func(symbol ModuleSymbol) string {
		return owner + ":" + quoteSelectorValue(symbol.Name)
	})
}

// suggestPackages completes a package selector's full path, or with a module pattern its path
// relative to each matching module root.
func (pipeline *Pipeline) suggestPackages(ctx context.Context, kind string, values []selectorPart, scopes []moduleScope) (map[string]bool, error) {
	scoped := len(values) == 2
	if len(values) > 2 || kind == "package" && scoped {
		return nil, fmt.Errorf("%s selector takes a package path or, for pkg:, a module pattern and a relative package", kind)
	}
	pattern, literal := globLiteral(values[len(values)-1].text)
	choices := map[string]bool{}
	if !literal {
		return choices, nil
	}
	var moduleGlob selectorGlob
	if scoped {
		var err error
		if moduleGlob, err = compileSelectorGlob(values[0].text); err != nil {
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
			if !scoped {
				if strings.HasPrefix(pkg.PackagePath, pattern) {
					choices[kind+":"+quoteSelectorValue(pkg.PackagePath)] = true
				}
				continue
			}
			relative, inside := relativePackage(pkg.PackagePath, scope.root.RootKey)
			if inside && strings.HasPrefix(relative, pattern) {
				choices[kind+":"+quoteSelectorValue(scope.root.RootKey)+":"+quoteSelectorValue(relative)] = true
			}
		}
	}
	return choices, nil
}

// suggestSymbols completes a symbol selector's literal pattern to the active symbols it selects as a
// prefix, each spelled by render. A pattern with a wildcard has no completions.
func (pipeline *Pipeline) suggestSymbols(ctx context.Context, selector Selector, scopes []moduleScope, render func(ModuleSymbol) string) (map[string]bool, error) {
	choices := map[string]bool{}
	pattern, literal := globLiteral(selector.Pattern)
	if !literal {
		return choices, nil
	}
	base, err := newIndexContext(ctx, pipeline.database, scopes)
	if err != nil {
		return nil, err
	}
	selector.Pattern = globEscaper.Replace(pattern) + "*"
	selected, err := newCompactIndex(base).resolveSelector(ctx, selector)
	if err != nil {
		return nil, err
	}
	for _, symbol := range selected.symbols {
		choices[render(symbol)] = true
	}
	return choices, nil
}

// suggestKinds completes a kind selector: the registered kind names before its second colon, and the
// query names of that kind's active symbols after it.
func (pipeline *Pipeline) suggestKinds(ctx context.Context, prefix string, values []selectorPart, scopes []moduleScope) (map[string]bool, error) {
	name, literal := globLiteral(values[0].text)
	switch {
	case len(values) > 2 || len(values) == 2 && !literal:
		return nil, fmt.Errorf("invalid selector prefix %q: expected kind:<kind>[:<name>]", prefix)
	case len(values) == 2:
		return pipeline.suggestSymbols(ctx, Selector{Kind: "kind", SymbolKind: name, Pattern: values[1].text}, scopes, func(symbol ModuleSymbol) string {
			return "kind:" + name + ":" + quoteSelectorValue(symbol.QueryName)
		})
	}
	choices := map[string]bool{}
	if !literal {
		return choices, nil
	}
	kinds, err := storage.LoadSymbolKinds(ctx, pipeline.database)
	if err != nil {
		return nil, err
	}
	for _, spec := range kinds.All() {
		if strings.HasPrefix(spec.Name, name) {
			choices["kind:"+spec.Name] = true
		}
	}
	return choices, nil
}
