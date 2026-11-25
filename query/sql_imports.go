package query

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
)

// sqlImportFields are the columns of package_imports(): one row per non-test package of a Go snapshot.
// path is the package directory relative to the checkout, package its import path and module the root
// key. file is the package's representative non-test Go file (doc.go, else the first by name), which is
// also the path a lint finding is reported against. count is the number of distinct third-party packages in the import closure of the package's own
// direct third-party imports, direct how many such imports it has, and largest the three direct imports
// with the biggest closures.
var sqlImportFields = map[string]string{
	"path": "string", "file": "string", "module": "string", "package": "string", "snapshot": "string",
	"count": "integer", "direct": "integer", "largest": "string",
}

var sqlImportWildcardFields = []string{"path", "file", "module", "package", "snapshot", "count", "direct", "largest"}

// largestImports is how many direct imports the largest column lists.
const largestImports = 3

type importBudget struct {
	pkg           storage.ImportGraphPackage
	count, direct int
	largest       string
}

// importBudgets charges each own package of the graph for the third-party packages reachable through
// its own direct third-party imports. A third-party package is neither standard library, which the
// graph omits, nor in the own package's module; weight reachable only through another package of that
// module is that package's, so the walk never enters the module.
func importBudgets(graph storage.ImportGraph) []importBudget {
	closure := importClosures{graph: graph, sizes: map[int]int{}, seen: make([]int, len(graph.Packages))}
	union := make([]int, len(graph.Packages))
	var budgets []importBudget
	for position, pkg := range graph.Packages {
		if !pkg.Own {
			continue
		}
		direct := closure.directImports(pkg)
		budget := importBudget{pkg: pkg, direct: len(direct)}
		for _, target := range direct {
			closure.walk(target, pkg.Module, union, position+1, func(int) { budget.count++ })
		}
		budget.largest = closure.largest(direct, pkg.Module)
		budgets = append(budgets, budget)
	}
	return budgets
}

type importClosures struct {
	graph storage.ImportGraph
	sizes map[int]int
	seen  []int
}

// directImports are the third-party packages pkg imports itself.
func (closure importClosures) directImports(pkg storage.ImportGraphPackage) []int {
	var direct []int
	for _, target := range pkg.Imports {
		if closure.graph.Packages[target].Module != pkg.Module {
			direct = append(direct, target)
		}
	}
	return direct
}

// walk visits every package reachable from start outside module, once per mark in seen, and calls
// visit for each.
func (closure importClosures) walk(start, module int, seen []int, mark int, visit func(int)) {
	pending := []int{start}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[current] == mark || closure.graph.Packages[current].Module == module {
			continue
		}
		seen[current] = mark
		visit(current)
		pending = append(pending, closure.graph.Packages[current].Imports...)
	}
}

// size is the number of third-party packages in the closure of one import, itself included.
func (closure importClosures) size(target, module int) int {
	if size, found := closure.sizes[target]; found {
		return size
	}
	size := 0
	closure.walk(target, module, closure.seen, target+1, func(int) { size++ })
	closure.sizes[target] = size
	return size
}

func (closure importClosures) largest(direct []int, module int) string {
	type sized struct {
		path string
		size int
	}
	imports := make([]sized, len(direct))
	for i, target := range direct {
		imports[i] = sized{closure.graph.Packages[target].Path, closure.size(target, module)}
	}
	slices.SortFunc(imports, func(left, right sized) int {
		return cmp.Or(cmp.Compare(right.size, left.size), strings.Compare(left.path, right.path))
	})
	listed := make([]string, 0, largestImports)
	for _, next := range imports[:min(largestImports, len(imports))] {
		listed = append(listed, fmt.Sprintf("%s (%d)", next.path, next.size))
	}
	return strings.Join(listed, ", ")
}

func (pipeline *Pipeline) sqlImportRecords(ctx context.Context, scopes []moduleScope) ([]sqlRecord, error) {
	var records []sqlRecord
	for _, scope := range scopes {
		graph, err := storage.LoadSnapshotImportGraph(ctx, pipeline.database, scope.snapshot.ID)
		if err != nil {
			return nil, err
		}
		for _, budget := range importBudgets(graph) {
			if budget.pkg.File == "" {
				return nil, fmt.Errorf("snapshot %s recorded no file for package %q; reindex or republish its checkout with this version of uir", scope.snapshot.ID, budget.pkg.Path)
			}
			match := ModuleMatch{Kind: "package", RootKey: scope.root.RootKey, Location: scope.location.CanonicalPath,
				SnapshotID: scope.snapshot.ID.String(), Path: budget.pkg.File, PackagePath: budget.pkg.Path}
			records = append(records, sqlRecord{match: match, values: map[string]any{
				"path": budget.pkg.Dir, "file": budget.pkg.File, "module": match.RootKey, "package": match.PackagePath, "snapshot": match.SnapshotID,
				"count": budget.count, "direct": budget.direct, "largest": budget.largest,
			}})
		}
	}
	return records, nil
}
