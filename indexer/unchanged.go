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
// type-checking: the head's revision, content set (sources and manifests) and configuration equal
// the discovered root's, and no file imports a module the toolchain loads from another directory.
// Under those conditions every package's input hash, and so the context hash, is unchanged by
// construction: sources, dependency versions and toolchain are fixed, standard-library and
// module-cache export shapes are functions of them, and the root's own packages' shapes recurse only
// through those. A workspace sibling's export shape is not recorded against the snapshots that
// consumed it, so a root importing one is always type-checked and compared by context hash.
func reusableHead(ctx context.Context, database *gorm.DB, root discoveredRoot, force bool) (uuid.UUID, bool, error) {
	if force {
		return uuid.Nil, false, nil
	}
	var head storage.ModuleSnapshot
	err := database.WithContext(ctx).Table("snapshots AS snapshot").Select("snapshot.*").
		Joins("JOIN location_heads AS head ON head.location_id = snapshot.location_id AND head.snapshot_id = snapshot.id").
		Joins("JOIN locations AS location ON location.id = head.location_id").
		Joins("JOIN modules AS root ON root.id = location.root_id").
		Where("root.root_key = ? AND location.canonical_path = ?", root.RootKey, root.LocalPath).
		Take(&head).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("load head snapshot of %q at %q: %w", root.RootKey, root.LocalPath, err)
	}
	if head.Revision != root.Revision || head.ContentSetHash != root.ContentSetHash || head.ConfigurationHash != root.ConfigurationHash {
		return uuid.Nil, false, nil
	}
	siblings, err := workspaceSiblingImports(root)
	if err != nil || len(siblings) > 0 {
		return uuid.Nil, false, err
	}
	return head.ID, true, nil
}

// workspaceSiblingImports lists the root's imports that resolve, by longest module-path prefix, to
// a module other than the root that the toolchain loads from disk: a go.work use, or a module a
// go.work or go.mod replace points at a local directory. Build constraints are ignored, so the list
// is a superset of what any variant imports.
func workspaceSiblingImports(root discoveredRoot) ([]string, error) {
	modules, err := diskModules(root)
	if err != nil {
		return nil, err
	}
	modules[root.RootKey] = true
	fileSet := token.NewFileSet()
	var siblings []string
	for _, file := range root.Files {
		parsed, err := parser.ParseFile(fileSet, file.AbsolutePath, file.Content, parser.ImportsOnly)
		if err != nil {
			return nil, fmt.Errorf("read imports of %q: %w", file.PathKey, err)
		}
		for _, spec := range parsed.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return nil, fmt.Errorf("import %s in %q: %w", spec.Path.Value, file.PathKey, err)
			}
			if module := owningModule(modules, path); module != "" && module != root.RootKey {
				siblings = append(siblings, path)
			}
		}
	}
	return siblings, nil
}

func owningModule(modules map[string]bool, importPath string) string {
	for candidate := importPath; ; {
		if modules[candidate] {
			return candidate
		}
		slash := strings.LastIndex(candidate, "/")
		if slash < 0 {
			return ""
		}
		candidate = candidate[:slash]
	}
}

// diskModules is every module path the root's build resolves from a local directory.
func diskModules(root discoveredRoot) (map[string]bool, error) {
	modules := map[string]bool{}
	goMod := filepath.Join(root.LocalPath, "go.mod")
	content, err := os.ReadFile(goMod)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", goMod, err)
	}
	parsed, err := modfile.Parse(goMod, content, nil)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", goMod, err)
	}
	addLocalReplaces(modules, parsed.Replace)
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
	addLocalReplaces(modules, work.Replace)
	for _, use := range work.Use {
		directory := use.Path
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(filepath.Dir(root.WorkFile), directory)
		}
		useMod := filepath.Join(directory, "go.mod")
		content, err := os.ReadFile(useMod)
		if err != nil {
			return nil, fmt.Errorf("read go.work use %q: %w", useMod, err)
		}
		if path := modfile.ModulePath(content); path != "" {
			modules[path] = true
			continue
		}
		return nil, fmt.Errorf("go.work use %q declares no module path", useMod)
	}
	return modules, nil
}

func addLocalReplaces(modules map[string]bool, replaces []*modfile.Replace) {
	for _, replace := range replaces {
		if replace.New.Version == "" {
			modules[replace.Old.Path] = true
		}
	}
}
