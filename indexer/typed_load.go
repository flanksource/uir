package indexer

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/flanksource/uir/storage"
	"golang.org/x/tools/go/packages"
)

const typedLoadMode = packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes |
	packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps | packages.NeedModule

// typedFile is a root file's syntax tree in the package variant that type-checked it.
type typedFile struct {
	pkg  *packages.Package
	file *ast.File
}

// typedLoad is one go/packages load of a module root under its build variant.
type typedLoad struct {
	root        discoveredRoot
	origins     map[*types.Package]packageOrigin
	files       map[string]typedFile
	ignored     map[string]bool
	directories map[string]bool
	parsed      map[string]string
}

// loadTyped loads every package of the root (and its test variants when includeTests is set) with
// full type information for the whole dependency graph, in the root's directory and build variant.
func loadTyped(ctx context.Context, loadPackages packageLoader, root discoveredRoot, includeTests bool) (typedLoad, error) {
	var mutex sync.Mutex
	parsed := map[string]string{}
	environment := append(os.Environ(), "GOOS="+root.Variant.GOOS, "GOARCH="+root.Variant.GOARCH, "CGO_ENABLED="+root.Variant.CGOEnabled)
	config := &packages.Config{
		Mode: typedLoadMode, Context: ctx, Dir: root.LocalPath, Env: environment, Tests: includeTests, Fset: token.NewFileSet(),
		ParseFile: func(fileSet *token.FileSet, filename string, source []byte) (*ast.File, error) {
			mutex.Lock()
			parsed[filename] = hashBytes(source)
			mutex.Unlock()
			return parser.ParseFile(fileSet, filename, source, parser.AllErrors|parser.ParseComments|parser.SkipObjectResolution)
		},
	}
	if len(root.Variant.BuildTags) > 0 {
		config.BuildFlags = []string{"-tags=" + strings.Join(root.Variant.BuildTags, ",")}
	}
	roots, err := loadPackages(config, "./...")
	if err != nil {
		return typedLoad{}, fmt.Errorf("load Go packages of %q: %w", root.RootKey, err)
	}
	load := typedLoad{
		root: root, origins: map[*types.Package]packageOrigin{}, files: map[string]typedFile{},
		ignored: map[string]bool{}, directories: map[string]bool{}, parsed: parsed,
	}
	packages.Visit(roots, nil, func(pkg *packages.Package) {
		load.origins[pkg.Types] = originOf(pkg)
	})
	for _, pkg := range roots {
		if strings.HasSuffix(pkg.ID, ".test") {
			continue
		}
		load.addRootPackage(pkg)
	}
	for _, file := range root.Files {
		if _, typed := load.files[file.AbsolutePath]; typed && parsed[file.AbsolutePath] != file.ContentHash {
			return typedLoad{}, fmt.Errorf("source %q changed while indexing root %q", file.PathKey, root.RootKey)
		}
	}
	return load, nil
}

func (load *typedLoad) addRootPackage(pkg *packages.Package) {
	load.directories[pkg.Dir] = true
	for _, name := range pkg.IgnoredFiles {
		load.ignored[name] = true
	}
	for _, file := range pkg.Syntax {
		name := pkg.Fset.File(file.Pos()).Name()
		if existing, seen := load.files[name]; !seen || (isTestVariant(existing.pkg) && !isTestVariant(pkg)) {
			load.files[name] = typedFile{pkg: pkg, file: file}
		}
	}
}

// isTestVariant reports whether a package was compiled for a test: "p [p.test]" or "p_test [p.test]".
func isTestVariant(pkg *packages.Package) bool { return pkg.ID != pkg.PkgPath }

func originOf(pkg *packages.Package) packageOrigin {
	switch module := pkg.Module; {
	case module == nil && (pkg.PkgPath == "unsafe" || len(pkg.GoFiles) > 0):
		return packageOrigin{Class: standardPackage}
	case module == nil:
		return packageOrigin{Class: unresolvedPackage}
	case module.Main, module.Replace != nil && module.Replace.Version == "":
		return packageOrigin{Class: workspacePackage, ModulePath: module.Path}
	case module.Replace != nil:
		return packageOrigin{Class: modulePackage, ModulePath: module.Path, Version: module.Replace.Path + "@" + module.Replace.Version}
	default:
		return packageOrigin{Class: modulePackage, ModulePath: module.Path, Version: module.Version}
	}
}

// packageState is how one discovered package fared in the typed load.
type packageState struct {
	coverage    storage.Coverage
	variants    []*packages.Package
	typed       map[string]typedFile
	excluded    map[string]string
	diagnostics []packageDiagnostic
}

// classifyPackage maps a discovered package's files onto the load and decides its coverage: every
// file excluded by build constraints is excluded; a go list failure, a variant without type
// information, or a file the load did not type-check is syntax; any other error is partial.
func (load *typedLoad) classifyPackage(files []discoveredFile) (packageState, error) {
	state := packageState{typed: map[string]typedFile{}, excluded: map[string]string{}}
	unmapped := 0
	variants := map[string]*packages.Package{}
	for _, file := range files {
		if typed, found := load.files[file.AbsolutePath]; found {
			state.typed[file.PathKey] = typed
			variants[typed.pkg.ID] = typed.pkg
			continue
		}
		switch {
		case load.ignored[file.AbsolutePath]:
			state.excluded[file.PathKey] = "build constraints exclude the file under " + load.root.Variant.describe()
		case !load.directories[filepath.Dir(file.AbsolutePath)]:
			state.excluded[file.PathKey] = "no package builds from the directory under " + load.root.Variant.describe()
		default:
			unmapped++
			state.diagnostics = append(state.diagnostics, packageDiagnostic{Path: file.PathKey, Message: "the file was not type-checked as part of its package"})
		}
	}
	for _, id := range sortedKeys(variants) {
		state.variants = append(state.variants, variants[id])
	}
	listFailed := unmapped > 0
	for _, variant := range state.variants {
		listFailed = listFailed || variant.TypesInfo == nil
		for _, loadError := range variant.Errors {
			diagnostic := load.diagnostic(loadError, files)
			if loadError.Kind == packages.ParseError {
				return packageState{}, fmt.Errorf("parse Go source %q: %s", load.relative(loadError.Pos), diagnostic.Message)
			}
			listFailed = listFailed || loadError.Kind == packages.ListError
			state.diagnostics = append(state.diagnostics, diagnostic)
		}
	}
	switch {
	case len(state.typed) == 0 && unmapped == 0:
		state.coverage = storage.CoverageExcluded
	case listFailed:
		state.coverage = storage.CoverageSyntax
	case len(state.diagnostics) > 0:
		state.coverage = storage.CoveragePartial
	default:
		state.coverage = storage.CoverageIndexed
	}
	return state, nil
}

// exportVariant is the variant whose API importers see: the non-test package when there is one.
func (state packageState) exportVariant() *packages.Package {
	for _, variant := range state.variants {
		if !isTestVariant(variant) {
			return variant
		}
	}
	return state.variants[0]
}

func (variant buildVariant) describe() string {
	description := "GOOS=" + variant.GOOS + " GOARCH=" + variant.GOARCH + " CGO_ENABLED=" + variant.CGOEnabled
	if len(variant.BuildTags) > 0 {
		description += " tags=" + strings.Join(variant.BuildTags, ",")
	}
	return description
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
