package indexer

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
	"golang.org/x/tools/go/packages"
)

// buildImportGraph records the import graph of a typed load: every non-test package reachable from the
// root's own non-test packages, other than the standard library, with its module and its imports. The
// standard library is left out because it is never charged, and test variants because a test's imports
// are not the package's. A root package with no Go files under the build variant has no imports and no row. A root package that failed to load has no module and no imports to record;
// its load errors are reported as coverage diagnostics, and it has no row. The graph depends only on
// the load, so equal loads give equal graphs.
func buildImportGraph(root discoveredRoot, roots []*packages.Package) (storage.ImportGraph, error) {
	own := map[string]bool{}
	for _, pkg := range roots {
		if isBuildVariant(pkg) {
			continue
		}
		if pkg.Module != nil {
			own[pkg.PkgPath] = true
		} else if len(pkg.Errors) == 0 {
			return storage.ImportGraph{}, fmt.Errorf("package %q of root %q has no module in the Go load", pkg.PkgPath, root.RootKey)
		}
	}
	reached := map[string]*packages.Package{}
	packages.Visit(roots, nil, func(pkg *packages.Package) {
		if class := originOf(pkg).Class; !isBuildVariant(pkg) && class != standardPackage && class != unresolvedPackage {
			reached[pkg.PkgPath] = pkg
		}
	})
	paths := sortedKeys(reached)
	index := make(map[string]int, len(paths))
	modules := map[string]bool{}
	for position, path := range paths {
		index[path] = position
		modules[reached[path].Module.Path] = true
	}
	graph := storage.ImportGraph{Version: storage.ImportGraphVersion, Modules: sortedKeys(modules), Packages: make([]storage.ImportGraphPackage, len(paths))}
	for position, path := range paths {
		node := importGraphNode(reached[path], index, graph.Modules)
		if node.Own = own[path] && len(reached[path].GoFiles) > 0; node.Own {
			var err error
			if node.Dir, err = rootRelative(root, reached[path], reached[path].Dir); err != nil {
				return storage.ImportGraph{}, err
			}
			if node.File, err = representativeFile(root, reached[path]); err != nil {
				return storage.ImportGraph{}, err
			}
		}
		graph.Packages[position] = node
	}
	return graph, nil
}

// isBuildVariant reports whether pkg is a test variant or a generated test main, which the graph omits.
func isBuildVariant(pkg *packages.Package) bool {
	return isTestVariant(pkg) || strings.HasSuffix(pkg.ID, ".test")
}

// importGraphNode is pkg with its module and the sorted indexes of the recorded packages it imports.
func importGraphNode(pkg *packages.Package, index map[string]int, modules []string) storage.ImportGraphPackage {
	node := storage.ImportGraphPackage{Path: pkg.PkgPath, Module: slices.Index(modules, pkg.Module.Path)}
	for _, imported := range pkg.Imports {
		if target, found := index[imported.PkgPath]; found {
			node.Imports = append(node.Imports, target)
		}
	}
	slices.Sort(node.Imports)
	node.Imports = slices.Compact(node.Imports)
	return node
}

func rootRelative(root discoveredRoot, pkg *packages.Package, absolute string) (string, error) {
	relative, err := filepath.Rel(root.LocalPath, absolute)
	if err != nil {
		return "", fmt.Errorf("path %q of package %q outside root %q: %w", absolute, pkg.PkgPath, root.RootKey, err)
	}
	return filepath.ToSlash(relative), nil
}

// representativeFile is the package's doc.go, else its lexicographically first non-test Go file, relative
// to the root, so a finding about the package can be reported against a real file.
func representativeFile(root discoveredRoot, pkg *packages.Package) (string, error) {
	files := slices.Clone(pkg.GoFiles)
	slices.SortFunc(files, func(left, right string) int {
		if (filepath.Base(left) == "doc.go") != (filepath.Base(right) == "doc.go") {
			if filepath.Base(left) == "doc.go" {
				return -1
			}
			return 1
		}
		return strings.Compare(filepath.Base(left), filepath.Base(right))
	})
	return rootRelative(root, pkg, files[0])
}
