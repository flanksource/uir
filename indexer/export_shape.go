package indexer

import (
	"errors"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/tools/go/packages"
)

// exportShapes computes and memoizes export_shape_hash for every package of a typed load.
type exportShapes struct {
	resolver  *symbolResolver
	load      *typedLoad
	toolchain string
	memo      map[string]string
}

func newExportShapes(resolver *symbolResolver, load *typedLoad) *exportShapes {
	return &exportShapes{resolver: resolver, load: load, toolchain: load.root.Variant.GoVersion, memo: map[string]string{}}
}

// of is a package's export shape. A package loaded from source in the workspace digests its
// exported symbols' (id, shape_hash) and its imports' export shapes; one that did not type-check
// cleanly has no proven shape, so it digests its files' bytes and its imports' shapes instead. A
// module-cache package is identified by module path, version, and toolchain, and the standard
// library by the toolchain alone.
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

func (shapes *exportShapes) workspace(pkg *packages.Package) (string, error) {
	imports, err := shapes.importsOf([]*packages.Package{pkg})
	if err != nil {
		return "", err
	}
	importHashes := make([]string, len(imports))
	for i, imported := range imports {
		importHashes[i] = imported.ExportShapeHash
	}
	sort.Strings(importHashes)
	if len(pkg.Errors) > 0 || pkg.TypesInfo == nil {
		return shapes.unproven(pkg, importHashes)
	}
	pairs, err := shapes.exportedSymbols(pkg.Types)
	if err != nil {
		return "", err
	}
	digest := newCanonicalHash(exportShapeVersion)
	digest.count(len(pairs))
	for _, pair := range pairs {
		digest.text(pair[0])
		digest.text(pair[1])
	}
	digest.count(len(importHashes))
	for _, hash := range importHashes {
		digest.text(hash)
	}
	return digest.sum(), nil
}

func (shapes *exportShapes) unproven(pkg *packages.Package, importHashes []string) (string, error) {
	files := append([]string(nil), pkg.GoFiles...)
	sort.Slice(files, func(i, j int) bool { return filepath.Base(files[i]) < filepath.Base(files[j]) })
	digest := newCanonicalHash(unprovenExportShapeVersion)
	digest.text(pkg.PkgPath)
	digest.count(len(files))
	for _, name := range files {
		contentHash, parsed := shapes.load.parsed[name]
		if !parsed {
			content, err := os.ReadFile(name)
			if err != nil {
				return "", fmt.Errorf("read %q: %w", name, err)
			}
			contentHash = hashBytes(content)
		}
		digest.text(filepath.Base(name))
		digest.text(contentHash)
	}
	digest.count(len(importHashes))
	for _, hash := range importHashes {
		digest.text(hash)
	}
	return digest.sum(), nil
}

// exportedSymbols is the sorted (symbol id, shape_hash) of every exported symbol a package declares.
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
		shape, err := shapes.resolver.shape(object)
		if errors.Is(err, errUnprovenType) {
			return nil, fmt.Errorf("exported %s has an invalid type in a package without errors", object.Name())
		}
		if err != nil {
			return nil, err
		}
		pair := [2]string{resolved.ID, shapeHash(shape)}
		if !seen[pair] {
			seen[pair] = true
			pairs = append(pairs, pair)
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i][0] < pairs[j][0] || (pairs[i][0] == pairs[j][0] && pairs[i][1] < pairs[j][1])
	})
	return pairs, nil
}

func digestTexts(version string, texts ...string) string {
	digest := newCanonicalHash(version)
	digest.count(len(texts))
	for _, text := range texts {
		digest.text(text)
	}
	return digest.sum()
}
