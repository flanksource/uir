package lint

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/flanksource/uir/query"
)

type Selection struct {
	Rules       []Rule
	MatchesPath func(string) bool
}

func (config *Config) Filter(options Options) (*Selection, error) {
	if options.GroupBy != "" && options.GroupBy != "file" && options.GroupBy != "symbol" {
		return nil, fmt.Errorf("invalid --group-by %q: expected symbol or file", options.GroupBy)
	}
	if options.NoIndex && len(config.Sources) > 0 {
		return nil, fmt.Errorf("mapped sources require indexing; select an existing snapshot with --snapshot")
	}
	level := cmp.Or(options.CompatibilityLevel, config.Lint.CompatibilityLevel)
	if err := validateCompatibilityLevel(level); err != nil {
		return nil, err
	}
	if options.CompatibilityLevel != 0 && options.Dialect != "" && options.Dialect != "tsql" {
		return nil, fmt.Errorf("a database compatibility level requires the tsql dialect")
	}
	if level != 0 && (options.SQLOnly || len(config.Sources) > 0) {
		rule, err := compatibilityRule(level)
		if err != nil {
			return nil, err
		}
		withCompatibility := *config
		withCompatibility.Lint.Rules = append(slices.Clone(config.Lint.Rules), rule)
		config = &withCompatibility
	}
	rules, err := config.Select(options.Rules)
	if err != nil {
		return nil, err
	}
	excluded := map[string]bool{}
	for _, id := range options.Exclude {
		excluded[id] = true
	}
	selection := &Selection{}
	for _, rule := range rules {
		if !excluded[rule.ID] {
			selection.Rules = append(selection.Rules, rule)
		}
	}
	for _, rule := range config.Lint.Rules {
		delete(excluded, rule.ID)
	}
	var paths []string
	for _, pattern := range options.Exclude {
		if excluded[pattern] {
			paths = append(paths, pattern)
		}
	}
	selection.MatchesPath, err = query.CompilePathFilter(options.Include, paths)
	if err != nil {
		return nil, err
	}
	return selection, nil
}
