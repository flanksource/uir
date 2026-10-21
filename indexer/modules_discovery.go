package indexer

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flanksource/commons-db/git"
	"golang.org/x/mod/modfile"
)

func discoverModules(ctx context.Context, path string, options discoveryOptions) ([]discoveredRoot, error) {
	roots, err := locateModuleRoots(ctx, path, options.Source)
	if err != nil {
		return nil, err
	}
	for i := range roots {
		if err := populateRoot(ctx, &roots[i], roots, options); err != nil {
			return nil, err
		}
	}
	return roots, nil
}

// discoverModule discovers the module rooted at checkout among the modules below path, reading only
// its sources: path still decides the module's parent and mount path.
func discoverModule(ctx context.Context, path, checkout string, includeTests bool) (discoveredRoot, error) {
	root, found, err := discoverModuleAt(ctx, path, checkout, includeTests)
	if err == nil && !found {
		return discoveredRoot{}, fmt.Errorf("registered checkout %q is not a Go module below %q", checkout, path)
	}
	return root, err
}

// discoverModuleAt is discoverModule reporting whether a module is rooted at checkout instead of
// failing when none is.
func discoverModuleAt(ctx context.Context, path, checkout string, includeTests bool) (discoveredRoot, bool, error) {
	roots, err := locateModuleRoots(ctx, path, nil)
	if err != nil {
		return discoveredRoot{}, false, err
	}
	for i := range roots {
		if roots[i].LocalPath == checkout {
			err := populateRoot(ctx, &roots[i], roots, discoveryOptions{IncludeTests: includeTests})
			return roots[i], true, err
		}
	}
	return discoveredRoot{}, false, nil
}

// locateModuleRoots finds the go.mod roots below path, or the enclosing module when there are none,
// with their parents and mount paths, without reading their sources.
func locateModuleRoots(ctx context.Context, path string, source *git.SourceInfo) ([]discoveredRoot, error) {
	if path == "" {
		path = "."
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve module path %q: %w", path, err)
	}
	workspace, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("canonicalize module path %q: %w", absolute, err)
	}
	info, err := os.Stat(workspace)
	if err != nil {
		return nil, fmt.Errorf("stat module path %q: %w", workspace, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("module path %q is not a directory", workspace)
	}
	visible, err := discoveryVisibility(ctx, workspace, source)
	if err != nil {
		return nil, err
	}
	gitViews := map[string]git.VisiblePaths{workspace: visible}
	var roots []discoveredRoot
	err = filepath.WalkDir(workspace, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		if current != workspace && (shouldSkipRootDiscovery(entry.Name()) || entry.Name() == "vendor") {
			return filepath.SkipDir
		}
		gitRoot := workspace
		for candidate := range gitViews {
			if len(candidate) > len(gitRoot) && pathWithin(current, candidate) {
				gitRoot = candidate
			}
		}
		currentVisible := gitViews[gitRoot]
		if current != workspace && !currentVisible.Contains(current) {
			if !isGitRoot(current) {
				return filepath.SkipDir
			}
			ignored, err := currentVisible.IsIgnored(ctx, current)
			if err != nil {
				return err
			}
			if ignored {
				return filepath.SkipDir
			}
		}
		if current != workspace && isGitRoot(current) {
			currentVisible, err = git.ListVisiblePaths(ctx, current)
			if err != nil {
				return err
			}
			gitViews[current] = currentVisible
		}
		modPath := filepath.Join(current, "go.mod")
		if !currentVisible.Contains(modPath) {
			return nil
		}
		content, readErr := os.ReadFile(modPath)
		if os.IsNotExist(readErr) {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("read %s: %w", modPath, readErr)
		}
		module, parseErr := modfile.Parse(modPath, content, nil)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", modPath, parseErr)
		}
		if module.Module == nil || strings.TrimSpace(module.Module.Mod.Path) == "" {
			return fmt.Errorf("%s has no module path", modPath)
		}
		root := discoveredRoot{RootKey: module.Module.Mod.Path, Name: filepath.Base(module.Module.Mod.Path), LocalPath: current, Kind: "module"}
		if isGitRoot(current) {
			root.Kind = "git"
		}
		roots = append(roots, root)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover modules below %q: %w", workspace, err)
	}
	if len(roots) == 0 {
		for parent := filepath.Dir(workspace); parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
			if source != nil && !pathWithin(parent, source.Dir) {
				break
			}
			_, err := os.Stat(filepath.Join(parent, "go.mod"))
			if err == nil {
				return locateModuleRoots(ctx, parent, source)
			}
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("stat enclosing go.mod in %q: %w", parent, err)
			}
		}
		return nil, fmt.Errorf("no go.mod found beneath %q", workspace)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].LocalPath < roots[j].LocalPath })
	for i := range roots {
		parentIndex := -1
		for j := range roots {
			if i == j || !pathWithin(roots[i].LocalPath, roots[j].LocalPath) {
				continue
			}
			if parentIndex == -1 || len(roots[j].LocalPath) > len(roots[parentIndex].LocalPath) {
				parentIndex = j
			}
		}
		if parentIndex != -1 {
			parent := roots[parentIndex]
			relative, relErr := filepath.Rel(parent.LocalPath, roots[i].LocalPath)
			if relErr != nil {
				return nil, relErr
			}
			roots[i].ParentRootKey = parent.LocalPath
			roots[i].MountPath = filepath.ToSlash(relative)
			if declaredSubmodule(parent.LocalPath, roots[i].LocalPath) {
				roots[i].Kind = "git-submodule"
			}
		}
	}
	return roots, nil
}
