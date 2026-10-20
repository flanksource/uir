package lint

import "github.com/flanksource/uir/query"

type Selection struct {
	Rules       []Rule
	MatchesPath func(string) bool
}

func (config *Config) Filter(options Options) (*Selection, error) {
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
