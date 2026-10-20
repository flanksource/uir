// Package lint executes SQL architecture rules against immutable UIR snapshots.
package lint

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/flanksource/gavel/models"
	"github.com/flanksource/uir/query"
	"gopkg.in/yaml.v3"
)

const ConfigName = ".uir.yaml"

type Config struct {
	Version int      `yaml:"version"`
	Lint    Settings `yaml:"lint"`
	Path    string   `yaml:"-"`
}

type Settings struct {
	IncludeTests bool   `yaml:"include_tests"`
	Rules        []Rule `yaml:"rules"`
}

type Rule struct {
	ID       string                   `yaml:"id"`
	Severity models.ViolationSeverity `yaml:"severity"`
	Message  string                   `yaml:"message"`
	Query    string                   `yaml:"query"`
	message  *template.Template
}

func Load(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	config := &Config{}
	if err := decoder.Decode(config); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%s: expected one YAML document: %v", path, err)
	}
	config.Path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if config.Version != 1 {
		return nil, fmt.Errorf("%s: unsupported version %d", path, config.Version)
	}
	if len(config.Lint.Rules) == 0 {
		return nil, errors.New("lint.rules must contain at least one rule")
	}
	seen := map[string]bool{}
	for i := range config.Lint.Rules {
		rule := &config.Lint.Rules[i]
		if strings.TrimSpace(rule.ID) == "" || rule.ID == "uir/coverage" || seen[rule.ID] {
			return nil, fmt.Errorf("invalid or duplicate lint rule ID %q", rule.ID)
		}
		seen[rule.ID] = true
		if rule.Severity == "" {
			rule.Severity = models.SeverityError
		}
		switch rule.Severity {
		case models.SeverityError, models.SeverityWarning, models.SeverityInfo:
		default:
			return nil, fmt.Errorf("rule %s: invalid severity %q", rule.ID, rule.Severity)
		}
		if strings.TrimSpace(rule.Message) == "" {
			return nil, fmt.Errorf("rule %s: message is required", rule.ID)
		}
		if err := query.ValidateSQLRows(rule.Query); err != nil {
			return nil, fmt.Errorf("rule %s: %w", rule.ID, err)
		}
		rule.message, err = template.New(rule.ID).Option("missingkey=error").Parse(rule.Message)
		if err != nil {
			return nil, fmt.Errorf("rule %s message: %w", rule.ID, err)
		}
	}
	return config, nil
}

func Discover(directory, explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	current, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	for {
		path := filepath.Join(current, ConfigName)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", fmt.Errorf("no %s found from %s up to the repository root", ConfigName, directory)
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
