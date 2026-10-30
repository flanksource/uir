package indexer

import (
	"errors"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"sort"

	"github.com/flanksource/uir/internal/canonicalhash"
	"golang.org/x/tools/go/packages"
)

// exportShapes computes and memoizes export_shape_hash for every package of a typed load.
type exportShapes struct {
	resolver  *symbolResolver
	load      *typedLoad
	toolchain string
	memo      map[string]string
	// siblings is every workspace package of another module than the root the recorded variants
	// import, transitively, by package path.
	siblings map[string]*packages.Package
	// loaded maps every type-checked package the shapes have walked to its load entry.
	loaded map[*types.Package]*packages.Package
}

func newExportShapes(resolver *symbolResolver, load *typedLoad) *exportShapes {
	return &exportShapes{
		resolver: resolver, load: load, toolchain: load.root.Variant.GoVersion, memo: map[string]string{}, siblings: map[string]*packages.Package{},
		loaded: map[*types.Package]*packages.Package{},
	}
}

// shaped reports whether some package input hash of the load folds in the package's export shape,
// directly or through the packages an export shape references.
func (shapes *exportShapes) shaped(pkg *packages.Package) bool {
	_, found := shapes.memo[pkg.ID]
	return found
}

// of is a package's export shape. A package loaded from source in the workspace digests its
// exported symbols' (id, API shape), the selectors its exported types promote, and the export shapes
// of only the packages whose types its exported API names: an importer can reach no other package
// through it, so a change elsewhere in its import closure does not re-key its importers. One that
// did not type-check cleanly has no proven shape, so it digests its files' bytes and its direct
// imports' shapes instead. A module-cache package is identified by module path, version, and
// toolchain, and the standard library by the toolchain alone.
func (shapes *exportShapes) of(pkg *packages.Package) (string, error) {
	if shape, found := shapes.memo[pkg.ID]; found {
		return shape, nil
	}
	origin := originOf(pkg)
	var shape string
	var err error
	switch origin.Class {
	case standardPackage:
		shape = digestTexts(standardExportShapeVersion, "std", shapes.toolchain)
	case modulePackage:
		shape = digestTexts(moduleExportShapeVersion, origin.ModulePath, origin.Version, shapes.toolchain)
	case unresolvedPackage:
		shape = digestTexts(unresolvedExportShapeVersion, pkg.PkgPath)
	case workspacePackage:
		shape, err = shapes.workspace(pkg)
	}
	if err != nil {
		return "", fmt.Errorf("export shape of %q: %w", pkg.PkgPath, err)
	}
	shapes.memo[pkg.ID] = shape
	return shape, nil
}

// importsOf is every direct import of the variants with the export shape it presents.
func (shapes *exportShapes) importsOf(variants []*packages.Package) ([]importShape, error) {
	seen := map[importShape]bool{}
	var imports []importShape
	for _, variant := range variants {
		for _, path := range sortedKeys(variant.Imports) {
			shape, err := shapes.of(variant.Imports[path])
			if err != nil {
				return nil, err
			}
			imported := importShape{Path: path, ExportShapeHash: shape}
			if !seen[imported] {
				seen[imported] = true
				imports = append(imports, imported)
			}
		}
	}
	return imports, nil
}

// workspace is a workspace package's export shape. A package whose own load reported no errors can
// still export a type an erroring import left invalid; its API is then as unproven as one that failed
// to type-check itself.
func (shapes *exportShapes) workspace(pkg *packages.Package) (string, error) {
	if len(pkg.Errors) > 0 || pkg.TypesInfo == nil {
		return shapes.unprovenWorkspace(pkg)
	}
	pairs, err := shapes.exportedSymbols(pkg.Types)
	if errors.Is(err, errUnprovenType) {
		return shapes.unprovenWorkspace(pkg)
	}
	if err != nil {
		return "", err
	}
	referenced, err := shapes.referencedShapes(pkg)
	if err != nil {
		return "", err
	}
	digest := canonicalhash.New(exportShapeVersion)
	digest.Count(len(pairs))
	for _, pair := range pairs {
		digest.Text(pair[0])
		digest.Text(pair[1])
	}
	digest.Count(len(referenced))
	for _, imported := range referenced {
		digest.Text(imported.Path)
		digest.Text(imported.ExportShapeHash)
	}
	return digest.Sum(), nil
}

// referencedShapes are the export shapes, sorted by import path, of the packages whose types the
// package's exported API names (apiReferences). Each must be in the package's import closure.
func (shapes *exportShapes) referencedShapes(pkg *packages.Package) ([]importShape, error) {
	shapes.index(pkg)
	referenced := apiReferences(pkg.Types)
	found := make([]importShape, 0, len(referenced))
	for _, imported := range referenced {
		loaded, known := shapes.loaded[imported]
		if !known {
			return nil, fmt.Errorf("exported API of %q names %q, which is not in its import closure", pkg.PkgPath, imported.Path())
		}
		shape, err := shapes.of(loaded)
		if err != nil {
			return nil, err
		}
		found = append(found, importShape{Path: imported.Path(), ExportShapeHash: shape})
	}
	return found, nil
}

// index records the load entry of every package in pkg's import closure that the shapes have not seen.
func (shapes *exportShapes) index(pkg *packages.Package) {
	if _, seen := shapes.loaded[pkg.Types]; seen {
		return
	}
	shapes.loaded[pkg.Types] = pkg
	for _, imported := range pkg.Imports {
		shapes.index(imported)
	}
}

func (shapes *exportShapes) unprovenWorkspace(pkg *packages.Package) (string, error) {
	imports, err := shapes.importsOf([]*packages.Package{pkg})
	if err != nil {
		return "", err
	}
	importHashes := make([]string, len(imports))
	for i, imported := range imports {
		importHashes[i] = imported.ExportShapeHash
	}
	sort.Strings(importHashes)
	return shapes.unproven(pkg, importHashes)
}

func (shapes *exportShapes) unproven(pkg *packages.Package, importHashes []string) (string, error) {
	files := append([]string(nil), pkg.GoFiles...)
	sort.Slice(files, func(i, j int) bool { return filepath.Base(files[i]) < filepath.Base(files[j]) })
	digest := canonicalhash.New(unprovenExportShapeVersion)
	digest.Text(pkg.PkgPath)
	digest.Count(len(files))
	for _, name := range files {
		contentHash, parsed := shapes.load.parsed[name]
		if !parsed {
			content, err := os.ReadFile(name)
			if err != nil {
				return "", fmt.Errorf("read %q: %w", name, err)
			}
			contentHash = hashBytes(content)
		}
		digest.Text(filepath.Base(name))
		digest.Text(contentHash)
	}
	digest.Count(len(importHashes))
	for _, hash := range importHashes {
		digest.Text(hash)
	}
	return digest.Sum(), nil
}

// exportedSymbols is the sorted (symbol id, API shape) of every exported symbol a package declares,
// and (type id, promoted selectors) of every exported defined type that promotes any: an unexported
// field or method of an exported type, or a renamed parameter, never changes it.
func (shapes *exportShapes) exportedSymbols(pkg *types.Package) ([][2]string, error) {
	var objects []types.Object
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		object := scope.Lookup(name)
		objects = append(objects, object)
		if typeName, ok := object.(*types.TypeName); ok && !typeName.IsAlias() {
			if named, ok := typeName.Type().(*types.Named); ok {
				for i := range named.NumMethods() {
					objects = append(objects, named.Method(i))
				}
			}
		}
	}
	for member := range shapes.resolver.memberIndex(pkg) {
		objects = append(objects, member)
	}
	seen := map[[2]string]bool{}
	pairs := make([][2]string, 0, len(objects))
	for _, object := range objects {
		resolved, err := shapes.resolver.resolve(object)
		if err != nil {
			return nil, err
		}
		if resolved.ID == "" || shapes.resolver.rows[resolved.ID].Visibility != "exported" {
			continue
		}
		api, err := shapes.resolver.apiShape(object)
		if err != nil {
			return nil, err
		}
		pair := [2]string{resolved.ID, api}
		if !seen[pair] {
			seen[pair] = true
			pairs = append(pairs, pair)
		}
	}
	selectors, err := shapes.selectorPairs(pkg)
	if err != nil {
		return nil, err
	}
	pairs = append(pairs, selectors...)
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i][0] < pairs[j][0] || (pairs[i][0] == pairs[j][0] && pairs[i][1] < pairs[j][1])
	})
	return pairs, nil
}

func digestTexts(version string, texts ...string) string {
	digest := canonicalhash.New(version)
	digest.Count(len(texts))
	for _, text := range texts {
		digest.Text(text)
	}
	return digest.Sum()
}
