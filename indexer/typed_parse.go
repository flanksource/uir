package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
)

// typedSources parses the files of a typed load. A file in the standard library or the module cache
// belongs to a package whose export shape is a digest of its path, version and toolchain, so the
// load needs only its package-level declarations: it is parsed without comments and its function
// declarations lose their bodies, which go/types then never checks or records. Every other file,
// the root's and each workspace module's, is parsed in full and its content hash kept.
type typedSources struct {
	root         string
	dependencies []string
	mutex        sync.Mutex
	parsed       map[string]string
	declarations map[string]bool
}

// newTypedSources asks the go command that loads the root, under the load's environment, for its
// GOROOT and GOMODCACHE.
func newTypedSources(ctx context.Context, root discoveredRoot, environment []string) (*typedSources, error) {
	command := exec.CommandContext(ctx, "go", "env", "-json", "GOROOT", "GOMODCACHE")
	command.Dir, command.Env = root.LocalPath, environment
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("read go env in %q: %w: %s", root.LocalPath, err, strings.TrimSpace(stderr.String()))
	}
	var values map[string]string
	if err := json.Unmarshal(output, &values); err != nil {
		return nil, fmt.Errorf("read go env in %q: decode %q: %w", root.LocalPath, output, err)
	}
	sources := &typedSources{root: root.LocalPath, parsed: map[string]string{}, declarations: map[string]bool{}}
	for _, name := range []string{"GOROOT", "GOMODCACHE"} {
		if values[name] == "" {
			return nil, fmt.Errorf("read go env in %q: %s is empty", root.LocalPath, name)
		}
		sources.dependencies = append(sources.dependencies, values[name])
	}
	return sources, nil
}

// dependencyFile reports whether filename is a standard-library or module-cache file outside the root.
func (sources *typedSources) dependencyFile(filename string) bool {
	if pathWithin(filename, sources.root) {
		return false
	}
	for _, directory := range sources.dependencies {
		if pathWithin(filename, directory) {
			return true
		}
	}
	return false
}

// parse is the load's packages.Config.ParseFile.
func (sources *typedSources) parse(fileSet *token.FileSet, filename string, source []byte) (*ast.File, error) {
	if !sources.dependencyFile(filename) {
		hash := hashBytes(source)
		sources.mutex.Lock()
		sources.parsed[filename] = hash
		sources.mutex.Unlock()
		return parser.ParseFile(fileSet, filename, source, parser.AllErrors|parser.ParseComments|parser.SkipObjectResolution)
	}
	file, err := parser.ParseFile(fileSet, filename, source, parser.AllErrors|parser.SkipObjectResolution)
	if file != nil {
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok {
				function.Body = nil
			}
		}
	}
	sources.mutex.Lock()
	sources.declarations[filename] = true
	sources.mutex.Unlock()
	return file, err
}

// requireFull fails for a workspace package any of whose files was parsed as declarations only: its
// export shape digests its checked API and its type errors, which body-less files would change. The
// load does not request CompiledGoFiles; the only compiled files GoFiles lacks are cgo's output in
// the build cache, which is never parsed as declarations.
func (sources *typedSources) requireFull(pkg *packages.Package) error {
	for _, name := range pkg.GoFiles {
		if sources.declarations[name] {
			return fmt.Errorf("workspace package %q was parsed without function bodies: its source %q is in GOROOT or GOMODCACHE", pkg.PkgPath, name)
		}
	}
	return nil
}
