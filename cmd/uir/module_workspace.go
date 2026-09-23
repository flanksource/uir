package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/term"
)

type addPathOptions struct {
	Path                 string
	IncludeWorkspaceUses bool
	NoWorkspaceUses      bool
	Input                io.Reader
	Prompt               io.Writer
}

func addPaths(options addPathOptions) ([]string, error) {
	include, exclude := options.IncludeWorkspaceUses, options.NoWorkspaceUses
	if include && exclude {
		return nil, errors.New("--include-workspace-uses and --no-workspace-uses are mutually exclusive")
	}
	absolute, err := filepath.Abs(options.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve add path %q: %w", options.Path, err)
	}
	selected, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("canonicalize add path %q: %w", absolute, err)
	}
	external, err := externalWorkspaceUses(selected)
	if err != nil {
		return nil, err
	}
	if len(external) == 0 || exclude {
		return []string{selected}, nil
	}
	if !include {
		input, ok := options.Input.(*os.File)
		if !ok || !term.IsTerminal(int(input.Fd())) {
			return nil, fmt.Errorf("go.work has %d use paths outside %q; specify --include-workspace-uses or --no-workspace-uses", len(external), selected)
		}
		if options.Prompt == nil {
			return nil, errors.New("interactive add requires a prompt writer")
		}
		_, err := fmt.Fprintf(options.Prompt, "Add %d go.work use paths outside %s? [y/N] ", len(external), selected)
		if err != nil {
			return nil, err
		}
		answer, err := bufio.NewReader(input).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read go.work use choice: %w", err)
		}
		if strings.EqualFold(strings.TrimSpace(answer), "y") || strings.EqualFold(strings.TrimSpace(answer), "yes") {
			include = true
		}
	}
	if include {
		return append([]string{selected}, external...), nil
	}
	return []string{selected}, nil
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
