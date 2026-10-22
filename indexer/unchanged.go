package indexer

import (
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"golang.org/x/mod/modfile"
	"gorm.io/gorm"
)

// reusableHead is the location's head snapshot when the root can be reported unchanged without
// type-checking: the head's revision, content set (sources and manifests), configuration and
// dependency set equal the discovered root's, and every workspace sibling package it imports still
// presents the export shape the head recorded for it. The root's dependencies must already be resolved
// to their target snapshots, as IndexModules does before extracting, because the dependency set hash
// covers those targets: a dependency with a new snapshot makes the root publish again, to link it, even
// when the root's own sources are unchanged. Under those conditions every package's input hash, and so
// the context hash, is unchanged by construction: sources, dependency versions and toolchain are fixed,
// standard-library and module-cache export shapes are functions of them, a sibling's shape is the one
// recorded, and the root's own packages' shapes recurse only through those. A sibling's current shape
// is only trusted from a snapshot this run published or confirmed; anything else type-checks the root.
// A sibling miss is counted in the running phase and logged with the sibling and why.
func reusableHead(ctx context.Context, check *headCheck, root discoveredRoot, force bool) (uuid.UUID, bool, error) {
	if force {
		return uuid.Nil, false, nil
	}
	check.checking[root.LocalPath] = true
	defer delete(check.checking, root.LocalPath)
	head, miss, err := check.headReuse(ctx, root)
	if err != nil {
		return uuid.Nil, false, err
	}
	if miss.counter != "" {
		PhaseCount(ctx, miss.counter, 1)
		if log, found := ctx.Value(phaseLogKey{}).(phaseLog); found {
			log.Infof("%s: %s at %s: %s", miss.counter, root.RootKey, root.LocalPath, miss.detail)
		}
	}
	return head, miss == (headMiss{}), nil
}

// headReuse is the root's head and, when it is not reusable, why.
func (check *headCheck) headReuse(ctx context.Context, root discoveredRoot) (uuid.UUID, headMiss, error) {
	head, found, err := loadHead(ctx, check.database, root)
	if err != nil {
		return uuid.Nil, headMiss{}, err
	}
	if miss := headMismatch(head, found, root); miss != "" {
		return uuid.Nil, headMiss{detail: miss}, nil
	}
	siblings, err := check.siblingsOf(root)
	if err != nil {
		return uuid.Nil, headMiss{}, err
	}
	miss, err := check.siblingsCurrent(ctx, root, head.ID, siblings)
	if err != nil || miss != (headMiss{}) {
		return uuid.Nil, miss, err
	}
	return head.ID, headMiss{}, nil
}

// headMismatch is which of the head's revision, content set, configuration and dependency set differs
// from the discovered root's, or that there is no head.
func headMismatch(head storage.ModuleSnapshot, found bool, root discoveredRoot) string {
	switch {
	case !found:
		return "no head"
	case head.Revision != root.Revision:
		return "revision " + root.Revision + " differs from the head's " + head.Revision
	case head.ContentSetHash != root.ContentSetHash:
		return "content set changed"
	case head.ConfigurationHash != root.ConfigurationHash:
		return "configuration changed"
	case head.DependencySetHash == nil || *head.DependencySetHash != dependencyHash(root.Dependencies):
		return "dependency set changed"
	}
	return ""
}

func loadHead(ctx context.Context, database *gorm.DB, root discoveredRoot) (storage.ModuleSnapshot, bool, error) {
	var head storage.ModuleSnapshot
	err := database.WithContext(ctx).Table("snapshots AS snapshot").Select("snapshot.*").
		Joins("JOIN location_heads AS head ON head.location_id = snapshot.location_id AND head.snapshot_id = snapshot.id").
		Joins("JOIN locations AS location ON location.id = head.location_id").
		Joins("JOIN modules AS root ON root.id = location.root_id").
		Where("root.root_key = ? AND location.canonical_path = ?", root.RootKey, root.LocalPath).
		Take(&head).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return storage.ModuleSnapshot{}, false, nil
	}
	if err != nil {
		return storage.ModuleSnapshot{}, false, fmt.Errorf("load head snapshot of %q at %q: %w", root.RootKey, root.LocalPath, err)
	}
	return head, true, nil
}

// workspaceSiblingImports maps each of the root's imports that resolves, by longest module-path
// prefix, to a module other than the root that the toolchain loads from disk (a go.work use, or a
// module a go.work or go.mod replace points at a local directory) to that module's path, and returns
// every such module's directory. Build constraints are ignored, so the imports are a superset of what
// any variant imports.
func workspaceSiblingImports(root discoveredRoot) (map[string]string, map[string]string, error) {
	modules, err := diskModules(root)
	if err != nil {
		return nil, nil, err
	}
	modules[root.RootKey] = root.LocalPath
	fileSet := token.NewFileSet()
	siblings := map[string]string{}
	for _, file := range root.Files {
		parsed, err := parser.ParseFile(fileSet, file.AbsolutePath, file.Content, parser.ImportsOnly)
		if err != nil {
			return nil, nil, fmt.Errorf("read imports of %q: %w", file.PathKey, err)
		}
		for _, spec := range parsed.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return nil, nil, fmt.Errorf("import %s in %q: %w", spec.Path.Value, file.PathKey, err)
			}
			if module := owningModule(modules, path); module != "" && module != root.RootKey {
				siblings[path] = module
			}
		}
	}
	return siblings, modules, nil
}

func owningModule[V any](modules map[string]V, importPath string) string {
	for candidate := importPath; ; {
		if _, found := modules[candidate]; found {
			return candidate
		}
		slash := strings.LastIndex(candidate, "/")
		if slash < 0 {
			return ""
		}
		candidate = candidate[:slash]
	}
}

// diskModules maps every module path the root's build resolves from a local directory to that
// directory.
func diskModules(root discoveredRoot) (map[string]string, error) {
	modules := map[string]string{}
	goMod := filepath.Join(root.LocalPath, "go.mod")
	if root.ModFile != "" {
		goMod = root.ModFile
	}
	content, err := os.ReadFile(goMod)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", goMod, err)
	}
	parsed, err := modfile.Parse(goMod, content, nil)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", goMod, err)
	}
	addLocalReplaces(modules, root.LocalPath, parsed.Replace)
	if root.WorkFile == "" {
		return modules, nil
	}
	content, err = os.ReadFile(root.WorkFile)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", root.WorkFile, err)
	}
	work, err := modfile.ParseWork(root.WorkFile, content, nil)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", root.WorkFile, err)
	}
	addLocalReplaces(modules, filepath.Dir(root.WorkFile), work.Replace)
	for _, use := range work.Use {
		directory := rootedPath(filepath.Dir(root.WorkFile), use.Path)
		useMod := filepath.Join(directory, "go.mod")
		content, err := os.ReadFile(useMod)
		if err != nil {
			return nil, fmt.Errorf("read go.work use %q: %w", useMod, err)
		}
		if path := modfile.ModulePath(content); path != "" {
			modules[path] = directory
			continue
		}
		return nil, fmt.Errorf("go.work use %q declares no module path", useMod)
	}
	return modules, nil
}

func addLocalReplaces(modules map[string]string, base string, replaces []*modfile.Replace) {
	for _, replace := range replaces {
		if replace.New.Version == "" {
			modules[replace.Old.Path] = rootedPath(base, replace.New.Path)
		}
	}
}
