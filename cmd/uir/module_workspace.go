package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
)

type addPathOptions struct {
	Path string
	All  bool
}

func addPaths(options addPathOptions) ([]string, error) {
	absolute, err := filepath.Abs(options.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve add path %q: %w", options.Path, err)
	}
	selected, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("canonicalize add path %q: %w", absolute, err)
	}
	if !options.All {
		return []string{selected}, nil
	}
	external, err := externalWorkspaceUses(selected)
	if err != nil {
		return nil, err
	}
	return append([]string{selected}, external...), nil
}

func externalWorkspaceUses(selected string) ([]string, error) {
	workfile, err := enclosingWorkfile(selected)
	if err != nil || workfile == "" {
		return nil, err
	}
	content, err := os.ReadFile(workfile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", workfile, err)
	}
	parsed, err := modfile.ParseWork(workfile, content, nil)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", workfile, err)
	}
	paths := map[string]bool{}
	for _, use := range parsed.Use {
		path := use.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(workfile), path)
		}
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, fmt.Errorf("resolve go.work use path %q: %w", path, err)
		}
		relative, err := filepath.Rel(selected, canonical)
		if err != nil {
			return nil, fmt.Errorf("compare go.work use path %q: %w", canonical, err)
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			withinUse, err := filepath.Rel(canonical, selected)
			if err != nil {
				return nil, fmt.Errorf("compare selected path with go.work use %q: %w", canonical, err)
			}
			if withinUse != ".." && !strings.HasPrefix(withinUse, ".."+string(filepath.Separator)) {
				continue
			}
			paths[canonical] = true
		}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

func enclosingWorkfile(selected string) (string, error) {
	for directory := selected; ; directory = filepath.Dir(directory) {
		candidate := filepath.Join(directory, "go.work")
		_, err := os.Stat(candidate)
		if err == nil {
			return candidate, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat %s: %w", candidate, err)
		}
		if directory == filepath.Dir(directory) {
			break
		}
	}
	return "", nil
}
