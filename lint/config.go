// Package lint executes architecture rules against immutable UIR snapshots.
package lint

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/flanksource/gavel/models"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"gopkg.in/yaml.v3"
)

const ConfigName = ".uir.yaml"

type Config struct {
	Version         int             `yaml:"version"`
	Sources         []SourceMapping `yaml:"sources,omitempty"`
	Lint            Settings        `yaml:"lint"`
	Path            string          `yaml:"-"`
	Root            string          `yaml:"-"`
	includeTestsSet bool
}

type Settings struct {
	IncludeTests       bool   `yaml:"include_tests"`
	Rules              []Rule `yaml:"rules"`
	CompatibilityLevel int    `yaml:"compatibility_level,omitempty"`
}

type Rule struct {
	ID         string                   `yaml:"id"`
	Severity   models.ViolationSeverity `yaml:"severity"`
	Message    string                   `yaml:"message"`
	Query      string                   `yaml:"query"`
	message    *template.Template
	mode       storage.AnalysisMode
	expression *query.Expr
}

func Load(path string) (config *Config, err error) {
	data, err := readConfig(path)
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var document struct {
		Expires string          `yaml:"_expires"`
		Version int             `yaml:"version"`
		Sources []SourceMapping `yaml:"sources"`
		Lint    struct {
			IncludeTests       *bool  `yaml:"include_tests"`
			Rules              []Rule `yaml:"rules"`
			CompatibilityLevel int    `yaml:"compatibility_level"`
		} `yaml:"lint"`
	}
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%s: expected one YAML document: %v", path, err)
	}
	config = &Config{Version: document.Version, Sources: document.Sources, Lint: Settings{Rules: document.Lint.Rules, CompatibilityLevel: document.Lint.CompatibilityLevel}}
	if document.Lint.IncludeTests != nil {
		config.includeTestsSet = true
		config.Lint.IncludeTests = *document.Lint.IncludeTests
	}
	config.Path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	config.Root = filepath.Dir(config.Path)
	if filepath.Base(config.Root) == ConfigDirectoryName {
		config.Root = filepath.Dir(config.Root)
	}
	if config.Version != 1 {
		return nil, fmt.Errorf("%s: unsupported version %d", path, config.Version)
	}
	if len(config.Lint.Rules) == 0 && len(config.Sources) == 0 {
		return nil, errors.New("lint.rules must contain at least one rule")
	}
	if err := validateSources(config.Sources); err != nil {
		return nil, err
	}
	if err := validateCompatibilityLevel(config.Lint.CompatibilityLevel); err != nil {
		return nil, err
	}
	for _, rule := range config.Lint.Rules {
		if rule.ID == compatibilityRuleID {
			return nil, fmt.Errorf("lint rule ID %q is reserved", rule.ID)
		}
	}
	return config, compileRules(config.Lint.Rules)
}

func compileRules(rules []Rule) error {
	seen := map[string]bool{}
	for i := range rules {
		rule := &rules[i]
		if strings.TrimSpace(rule.ID) == "" || rule.ID == indexer.RuleCoverage || rule.ID == indexer.RuleIndex || seen[rule.ID] {
			return fmt.Errorf("invalid or duplicate lint rule ID %q", rule.ID)
		}
		seen[rule.ID] = true
		if rule.Severity == "" {
			rule.Severity = models.SeverityError
		}
		switch rule.Severity {
		case models.SeverityError, models.SeverityWarning, models.SeverityInfo:
		default:
			return fmt.Errorf("rule %s: invalid severity %q", rule.ID, rule.Severity)
		}
		if strings.TrimSpace(rule.Message) == "" {
			return fmt.Errorf("rule %s: message is required", rule.ID)
		}
		var err error
		if err := rule.compileQuery(); err != nil {
			return fmt.Errorf("rule %s: %w", rule.ID, err)
		}
		rule.message, err = template.New(rule.ID).Option("missingkey=error").Parse(rule.Message)
		if err != nil {
			return fmt.Errorf("rule %s message: %w", rule.ID, err)
		}
	}
	return nil
}

func (config *Config) Select(ids []string) ([]Rule, error) {
	if len(ids) == 0 {
		return config.Lint.Rules, nil
	}
	requested := map[string]bool{}
	for _, id := range ids {
		requested[id] = true
	}
	var selected []Rule
	for _, rule := range config.Lint.Rules {
		if requested[rule.ID] {
			selected = append(selected, rule)
			delete(requested, rule.ID)
		}
	}
	for _, id := range ids {
		if requested[id] {
			return nil, fmt.Errorf("unknown lint rule %q", id)
		}
	}
	return selected, nil
}
