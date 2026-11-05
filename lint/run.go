package lint

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"time"

	"github.com/flanksource/commons-db/git"
	"github.com/flanksource/gavel/linters"
	"github.com/flanksource/gavel/models"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

type Options struct {
	Rules     []string
	Tags      []string
	Include   []string
	Exclude   []string
	NoIndex   bool
	Snapshot  string
	GroupBy   string
	Changed   bool
	Staged    bool
	Revision  string
	Source    *git.PreparedSource
	Files     []string
	WorkDir   string
	All       bool
	Directory string
	// NoCache evaluates every rule and subject, ignoring stored findings, and overwrites them.
	NoCache bool
}

func Run(ctx context.Context, database *gorm.DB, config *Config, options Options) *Result {
	result := &Result{LinterResult: &linters.LinterResult{Linter: "uir", Violations: []models.Violation{}}, GroupBy: options.GroupBy}
	workDir := options.WorkDir
	if workDir == "" && config != nil {
		workDir = config.Root
	}
	result.WorkDir = workDir
	adapter := &sqlLinter{database: database, config: config, options: options, result: result}
	runOptions := linters.RunOptions{WorkDir: workDir, ForceJSON: true}
	if config != nil {
		runOptions.ConfigDir = config.Root
	}
	result.LinterResult = linters.RunLinterWithTask(ctx, nil, adapter, runOptions)
	return result
}

func execute(ctx context.Context, database *gorm.DB, config *Config, options Options, result *Result) error {
	if config == nil || config.Path == "" || config.Root == "" {
		return errors.New("a loaded lint configuration is required")
	}
	if err := options.Validate(); err != nil {
		return err
	}
	selection, err := config.Filter(options)
	if err != nil {
		return err
	}
	rules := selection.Rules
	result.RuleCount = len(rules)
	for _, rule := range rules {
		if rule.message == nil {
			return fmt.Errorf("rule %s: configuration must be compiled with lint.Load", rule.ID)
		}
	}
	if len(rules) == 0 || (options.Files != nil && len(options.Files) == 0) {
		return nil
	}
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return err
	}
	coverage := map[string]bool{}
	files := map[string]bool{}
	var sourceSets map[string]string
	cache := newCacheReport(time.Now().UTC())
	defer result.orderRules(rules)
	defer cache.finish(result)
	for _, mode := range []storage.AnalysisMode{storage.AnalysisSyntax, storage.AnalysisTyped} {
		selected := false
		for _, rule := range rules {
			selected = selected || rule.requirements.Mode == mode
		}
		if !selected {
			continue
		}
		scopeOptions, err := snapshots(ctx, database, config, options, mode)
		if err != nil {
			return err
		}
		currentSets, err := analysisSourceSets(ctx, database, scopeOptions)
		if err != nil {
			return err
		}
		if sourceSets != nil && !maps.Equal(sourceSets, currentSets) {
			return errors.New("project sources changed between syntax and typed analysis; rerun uir lint")
		}
		sourceSets = currentSets
		gaps, err := pipeline.ScopeCoverage(ctx, scopeOptions)
		if err != nil {
			return err
		}
		if err := result.addCoverage(gaps, coverage); err != nil {
			return err
		}
		scope, err := loadScope(ctx, database, scopeOptions)
		if err != nil {
			return err
		}
		for _, rule := range rules {
			if rule.requirements.Mode != mode {
				continue
			}
			run := &ruleRun{database: database, pipeline: pipeline, scope: scope, rule: rule, noCache: options.NoCache, now: cache.now}
			findings, err := run.lint(ctx)
			cache.add(run)
			if err == nil {
				err = result.emit(rule, findings, scope, options, selection, files)
			}
			result.FileCount = len(files)
			if err != nil {
				return fmt.Errorf("rule %s: %w", rule.ID, err)
			}
		}
	}
	return nil
}

// emit adds a rule's findings that the selection's path filters keep, located in the scope's checkouts.
func (result *Result) emit(rule Rule, findings []finding, scope *lintScope, options Options, selection *Selection, files map[string]bool) error {
	for _, found := range findings {
		location, known := scope.locations[found.Root]
		if !known {
			return fmt.Errorf("finding in root %q outside the lint scope", found.Root)
		}
		if found.Path != "" {
			path, err := filepath.Rel(result.WorkDir, filepath.Join(location, found.Path))
			if options.All {
				path = found.Path
			}
			if err != nil {
				return err
			}
			if !selection.MatchesPath(filepath.ToSlash(path)) {
				continue
			}
		}
		violation := models.NewViolationBuilder().WithMessage(found.Message).WithSource("uir").WithRuleFromLinter("uir", rule.ID).Build()
		violation.Severity = rule.Severity
		if found.Path != "" {
			violation.File = filepath.Join(location, found.Path)
			files[violation.File] = true
		}
		if found.Line != nil {
			violation.Line = *found.Line
		}
		if found.Column != nil {
			violation.Column = *found.Column
		}
		var witness *query.ModuleSymbol
		if found.Symbol != nil {
			witness = &query.ModuleSymbol{ID: found.Symbol.ID, QueryName: found.Symbol.QueryName, DeclarationKey: found.Symbol.DeclarationKey}
		}
		result.addFinding(violation, witness)
	}
	return nil
}

// addCoverage adds the warning of every diagnostic of the packages in scope that are not fully
// indexed, once each across rules and analysis modes: uir/index for a file indexed with a degraded
// document, uir/coverage for a load or type error.
func (result *Result) addCoverage(gaps []query.ModuleCoverage, seen map[string]bool) error {
	for _, gap := range gaps {
		warnings, err := indexer.CoverageViolations(gap.Location, gap.Diagnostics)
		if err != nil {
			return fmt.Errorf("package %s in snapshot %s: %w", gap.PackagePath, gap.SnapshotID, err)
		}
		for _, warning := range warnings {
			key := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s", warning.Rule.Method, warning.File, warning.Line, warning.Column, *warning.Message)
			if !seen[key] {
				seen[key] = true
				result.addFinding(warning, nil)
			}
		}
	}
	return nil
}
