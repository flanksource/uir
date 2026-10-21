package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

type DiscoveryOptions struct {
	Directory string
	Explicit  string
	// Boundary restricts discovery to a captured source tree and excludes home configuration.
	Boundary string
}

func Discover(options DiscoveryOptions) (*Config, error) {
	if options.Explicit != "" {
		if options.Boundary != "" && !insideSource(options.Explicit, options.Boundary) {
			return nil, fmt.Errorf("no lint configuration inside the selected Git source")
		}
		return Load(options.Explicit)
	}
	directory, err := filepath.Abs(options.Directory)
	if err != nil {
		return nil, err
	}
	if options.Boundary != "" && !insideSource(directory, options.Boundary) {
		return nil, fmt.Errorf("directory %q is outside configuration boundary %q", directory, options.Boundary)
	}
	paths, root, err := configurationPaths(directory, options.Boundary)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no %s configuration inside %s or its configured ancestors", ConfigName, directory)
	}
	config := &Config{Version: 1, Root: root}
	positions := map[string]int{}
	for _, path := range paths {
		layer, err := Load(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		config.Path = layer.Path
		if layer.includeTestsSet {
			config.Lint.IncludeTests = layer.Lint.IncludeTests
		}
		for _, rule := range layer.Lint.Rules {
			if position, exists := positions[rule.ID]; exists {
				config.Lint.Rules[position] = rule
			} else {
				positions[rule.ID] = len(config.Lint.Rules)
				config.Lint.Rules = append(config.Lint.Rules, rule)
			}
		}
	}
	return config, nil
}

func configurationPaths(directory, boundary string) ([]string, string, error) {
	var paths []string
	gitRoot := boundary
	for current := directory; ; current = filepath.Dir(current) {
		path := filepath.Join(current, ConfigName)
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		} else if !os.IsNotExist(err) {
			return nil, "", err
		}
		if gitRoot == "" {
			if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
				gitRoot = current
			} else if !os.IsNotExist(err) {
				return nil, "", err
			}
		}
		if current == boundary || filepath.Dir(current) == current {
			break
		}
	}
	root := directory
	if gitRoot != "" {
		root = gitRoot
	}
	if len(paths) > 0 && (gitRoot == "" || insideSource(filepath.Dir(paths[0]), gitRoot)) {
		root = filepath.Dir(paths[0])
	}
	slices.Reverse(paths)
	if boundary == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, "", err
		}
		path := filepath.Join(home, ConfigName)
		// Home is always the base layer, including when it is also an ancestor.
		paths = slices.DeleteFunc(paths, func(candidate string) bool { return candidate == path })
		if _, err := os.Stat(path); err == nil {
			paths = append([]string{path}, paths...)
			if root == home {
				root = directory
			}
		} else if !os.IsNotExist(err) {
			return nil, "", err
		}
	}
	return paths, root, nil
}
