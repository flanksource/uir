package lint

import (
	"errors"
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
	config := &Config{Version: 1, Root: directory}
	if root != "" {
		config.Root = root
	}
	home := ""
	if options.Boundary == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	positions := map[string]int{}
	for _, path := range paths {
		layer, err := Load(path)
		if errors.Is(err, errExpiredConfig) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		config.Path = layer.Path
		if layer.Root != home && (root == "" || insideSource(layer.Root, root)) {
			config.Root = layer.Root
		}
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
	if config.Path == "" {
		return nil, fmt.Errorf("no unexpired lint configuration inside %s or its configured ancestors", directory)
	}
	return config, nil
}

func configurationPaths(directory, boundary string) ([]string, string, error) {
	var directories []string
	gitRoot := boundary
	for current := directory; ; current = filepath.Dir(current) {
		directories = append(directories, current)
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
	slices.Reverse(directories)
	if boundary == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, "", err
		}
		directories = slices.DeleteFunc(directories, func(candidate string) bool { return candidate == home })
		directories = append([]string{home}, directories...)
	}
	var paths []string
	for _, current := range directories {
		layer, err := directoryConfigurations(current)
		if err != nil {
			return nil, "", err
		}
		paths = append(paths, layer...)
	}
	return paths, gitRoot, nil
}
