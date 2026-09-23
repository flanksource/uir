package indexer

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
)

func discoverModules(ctx context.Context, path string, includeTests bool) ([]discoveredRoot, error) {
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
		modPath := filepath.Join(current, "go.mod")
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
		root := discoveredRoot{RootKey: module.Module.Mod.Path, LocalPath: current, Kind: "module"}
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
			_, err := os.Stat(filepath.Join(parent, "go.mod"))
			if err == nil {
				return discoverModules(ctx, parent, includeTests)
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
	for i := range roots {
		if err := populateRoot(ctx, &roots[i], roots, includeTests); err != nil {
			return nil, err
		}
	}
	return roots, nil
}
