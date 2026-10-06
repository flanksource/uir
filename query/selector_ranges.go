package query

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

// rangeBatch bounds the handle ranges one candidate query ORs together.
const rangeBatch = 128

// moduleRange is every handle of one module: the first handle of its first package through the last
// handle of its last.
func moduleRange(module uint64) (symbolhandle.Range, error) {
	first, err := symbolhandle.PackageRange(module, 0)
	if err != nil {
		return symbolhandle.Range{}, err
	}
	last, err := symbolhandle.PackageRange(module, symbolhandle.MaxPackage)
	return symbolhandle.Range{Low: first.Low, High: last.High}, err
}

// mergeRanges sorts inclusive ranges and joins the ones that overlap or touch, such as the ranges of
// consecutively numbered packages.
func mergeRanges(ranges []symbolhandle.Range) []symbolhandle.Range {
	sorted := slices.SortedFunc(slices.Values(ranges), func(left, right symbolhandle.Range) int { return cmp.Compare(left.Low, right.Low) })
	var merged []symbolhandle.Range
	for _, next := range sorted {
		if last := len(merged) - 1; last >= 0 && next.Low <= merged[last].High+1 {
			merged[last].High = max(merged[last].High, next.High)
			continue
		}
		merged = append(merged, next)
	}
	return merged
}

// rangeClause ORs handle ranges into one parenthesized condition over symbols.handle.
func rangeClause(ranges []symbolhandle.Range) (string, []any) {
	conditions := make([]string, len(ranges))
	arguments := make([]any, 0, 2*len(ranges))
	for i, handles := range ranges {
		conditions[i] = "handle BETWEEN ? AND ?"
		arguments = append(arguments, handles.Low, handles.High)
	}
	return "(" + strings.Join(conditions, " OR ") + ")", arguments
}

// relativePackage names a package relative to a module root: "." for the root package, the path below
// the root otherwise, and false for a package outside the root.
func relativePackage(packagePath, moduleKey string) (string, bool) {
	if packagePath == moduleKey {
		return ".", true
	}
	if relative, inside := strings.CutPrefix(packagePath, moduleKey+"/"); inside {
		return relative, true
	}
	return "", false
}

// selectorScopes groups by root key the scopes whose root a selector admits. A selector only matches
// symbols defined in their own module's root, so a module pattern, or a mod selector's pattern, that
// rejects a root rejects every symbol that root could contribute.
func (index *compactIndex) selectorScopes(selector compiledSelector) map[string][]int {
	scopes := map[string][]int{}
	for position, scope := range index.scopes {
		root := scope.root.RootKey
		if selector.ModulePattern != "" && !selector.moduleGlob.matches(root) || selector.Kind == "mod" && !selector.glob.matches(root) ||
			selector.within != nil && !selector.within.admitsRoot(root) {
			continue
		}
		scopes[root] = append(scopes[root], position)
	}
	return scopes
}

// qualifiedPackageFilter is the packages a qualified name pattern (one with a /) can match symbols of.
// A matched key is the package path, a dot, and the name, and the glob's leading literal segments
// equal the key's leading segments, so the package either lies below those segments or is their
// prefix up to a dot. It is a necessary condition only; the glob still decides each symbol.
func qualifiedPackageFilter(pattern string) func(packagePath, moduleKey string) bool {
	literal := pattern
	if special := strings.IndexAny(pattern, "*?\\"); special >= 0 {
		literal = pattern[:special]
	}
	segments := literal[:max(strings.LastIndex(literal, "/"), 0)]
	return func(packagePath, _ string) bool {
		return segments == "" || strings.HasPrefix(packagePath, segments+"/") || strings.HasPrefix(segments, packagePath+".")
	}
}

// packageFilter is the packages a pkg selector's pattern matches, by full path or, with a module
// pattern, by the path relative to the module root.
func packageFilter(selector Selector, glob selectorGlob) func(packagePath, moduleKey string) bool {
	return func(packagePath, moduleKey string) bool {
		if selector.ModulePattern == "" {
			return glob.matches(packagePath)
		}
		relative, inside := relativePackage(packagePath, moduleKey)
		return inside && glob.matches(relative)
	}
}

// selectorRanges is the handle ranges that can hold a selector's candidates in the admitted roots:
// for a pkg selector, a symbol selector qualified by a package path, or a selector within pkg: scopes,
// the ranges of the root's registered packages it can match, and otherwise each root module's whole
// range. A root whose module key was never registered has no symbols.
func (index *compactIndex) selectorRanges(ctx context.Context, selector compiledSelector, roots []string) ([]symbolhandle.Range, error) {
	var modules []storage.SymbolModule
	if err := index.database.WithContext(ctx).Where("module_key IN ?", roots).Order("number").Find(&modules).Error; err != nil {
		return nil, fmt.Errorf("load the symbol modules of %d roots: %w", len(roots), err)
	}
	switch {
	case len(modules) == 0:
		return nil, nil
	case selector.Kind == "pkg":
		return index.packageRanges(ctx, modules, packageFilter(selector.Selector, selector.glob))
	case selector.Kind != "mod" && selector.Owner == "" && strings.Contains(selector.Pattern, "/"):
		return index.packageRanges(ctx, modules, qualifiedPackageFilter(selector.Pattern))
	case selector.within != nil && selector.within.packagesOnly():
		return index.packageRanges(ctx, modules, selector.within.admitsPackage)
	}
	ranges := make([]symbolhandle.Range, 0, len(modules))
	for _, module := range modules {
		handles, err := moduleRange(uint64(module.Number))
		if err != nil {
			return nil, fmt.Errorf("module %q: %w", module.ModuleKey, err)
		}
		ranges = append(ranges, handles)
	}
	return ranges, nil
}

// packageRanges is the ranges of the modules' registered packages that keep accepts.
func (index *compactIndex) packageRanges(ctx context.Context, modules []storage.SymbolModule, keep func(packagePath, moduleKey string) bool) ([]symbolhandle.Range, error) {
	roots := make(map[int32]string, len(modules))
	for _, module := range modules {
		roots[module.Number] = module.ModuleKey
	}
	var packages []storage.SymbolPackage
	if err := index.database.WithContext(ctx).Where("module_number IN ?", slices.Sorted(maps.Keys(roots))).Find(&packages).Error; err != nil {
		return nil, fmt.Errorf("load the packages of %d symbol modules: %w", len(roots), err)
	}
	var ranges []symbolhandle.Range
	for _, pkg := range packages {
		if !keep(pkg.PackagePath, roots[pkg.ModuleNumber]) {
			continue
		}
		handles, err := symbolhandle.PackageRange(uint64(pkg.ModuleNumber), uint64(pkg.Number))
		if err != nil {
			return nil, fmt.Errorf("package %q: %w", pkg.PackagePath, err)
		}
		ranges = append(ranges, handles)
	}
	return ranges, nil
}
