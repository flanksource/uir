package indexer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/internal/canonicalhash"
	"golang.org/x/tools/go/packages"
)

// siblingRecord is one workspace sibling package a load read: the export shape it presented as a
// direct import, and the digest of its sources. Both are nil for a direct import written only in
// files no variant compiled.
type siblingRecord struct {
	shape, digest *string
}

func (shapes *exportShapes) isSibling(pkg *packages.Package) bool {
	origin := originOf(pkg)
	return origin.Class == workspacePackage && origin.ModulePath != shapes.load.root.RootKey
}

// recordSiblings records the export shape every direct sibling import of the variants presented,
// collects the siblings they import transitively, and reports whether an import path presented two
// different shapes.
func (shapes *exportShapes) recordSiblings(variants []*packages.Package, recorded map[string]siblingRecord) (bool, error) {
	conflict := false
	for _, variant := range variants {
		for path, imported := range variant.Imports {
			if !shapes.isSibling(imported) {
				continue
			}
			shape, err := shapes.of(imported)
			if err != nil {
				return false, err
			}
			if previous := recorded[path].shape; previous != nil && *previous != shape {
				conflict = true
			}
			recorded[path] = siblingRecord{shape: &shape}
		}
	}
	packages.Visit(variants, nil, func(pkg *packages.Package) {
		if _, seen := shapes.siblings[pkg.PkgPath]; !seen && shapes.isSibling(pkg) {
			shapes.siblings[pkg.PkgPath] = pkg
		}
	})
	return conflict, nil
}

// recordSiblingSources records the source digest of every sibling the load read, and an empty record
// for every sibling the root's files import that no compiled variant imported, so a head whose
// siblings are all in excluded files still has a record. A nil map, after a conflict, stays nil.
func (shapes *exportShapes) recordSiblingSources(recorded map[string]siblingRecord) error {
	if recorded == nil {
		return nil
	}
	for path, pkg := range shapes.siblings {
		module := pkg.Module
		if module.Replace != nil {
			module = module.Replace
		}
		digest, err := packageSourceDigest(module.Dir, pkg.Dir, shapes.load.parsed)
		if err != nil {
			return fmt.Errorf("digest sources of sibling %q: %w", path, err)
		}
		record := recorded[path]
		record.digest = &digest
		recorded[path] = record
	}
	imports, _, err := workspaceSiblingImports(shapes.load.root)
	if err != nil {
		return err
	}
	for path := range imports {
		if _, found := recorded[path]; !found {
			recorded[path] = siblingRecord{}
		}
	}
	return nil
}

// packageSourceDigest digests a package's sources: its module's go.mod, and the path key, relative to
// the module, and content hash of every non-test Go file in its directory that the go command reads.
// read supplies the content hash a load already read for a file, by absolute path; other files are
// hashed from disk. A directory that no longer exists has no files.
func packageSourceDigest(moduleDir, packageDir string, read map[string]string) (string, error) {
	goMod, err := os.ReadFile(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod of %q: %w", moduleDir, err)
	}
	entries, err := os.ReadDir(packageDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("list %q: %w", packageDir, err)
	}
	files := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		absolute := filepath.Join(packageDir, name)
		hash, found := read[absolute]
		if !found {
			content, err := os.ReadFile(absolute)
			if err != nil {
				return "", fmt.Errorf("read %q: %w", absolute, err)
			}
			hash = hashBytes(content)
		}
		key, err := filepath.Rel(moduleDir, absolute)
		if err != nil {
			return "", fmt.Errorf("path of %q in module %q: %w", absolute, moduleDir, err)
		}
		files[filepath.ToSlash(key)] = hash
	}
	keys := sortedKeys(files)
	digest := canonicalhash.New(siblingSourceVersion)
	digest.Text(hashBytes(goMod))
	digest.Count(len(keys))
	for _, key := range keys {
		digest.Text(key)
		digest.Text(files[key])
	}
	return digest.Sum(), nil
}
