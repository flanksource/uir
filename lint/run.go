package lint

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/gavel/linters"
	"github.com/flanksource/gavel/models"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

type Options struct {
	Rules    []string
	Include  []string
	Exclude  []string
	NoIndex  bool
	Snapshot string
}

func Run(ctx context.Context, database *gorm.DB, config *Config, options Options) *linters.LinterResult {
	started := time.Now()
	result := &linters.LinterResult{Linter: "uir", Violations: []models.Violation{}}
	err := execute(ctx, database, config, options, result)
	result.Duration = time.Since(started)
	result.Success = err == nil
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

func execute(ctx context.Context, database *gorm.DB, config *Config, options Options, result *linters.LinterResult) error {
	if config == nil || config.Path == "" {
		return errors.New("a loaded lint configuration is required")
	}
	result.WorkDir = filepath.Dir(config.Path)
	if options.NoIndex && options.Snapshot != "" {
		return errors.New("--no-index and --snapshot cannot be combined")
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
	if len(rules) == 0 {
		return nil
	}
	scopes, err := snapshots(ctx, database, config, options)
	if err != nil {
		return err
	}
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return err
	}
	coverage := map[string]bool{}
	files := map[string]bool{}
	for _, rule := range rules {
		for _, scope := range scopes {
			queried, err := pipeline.VisitSQL(ctx, rule.Query, scope, func(row query.SQLRow) error {
				if row.Match.Path != "" {
					path, err := filepath.Rel(result.WorkDir, filepath.Join(row.Match.Location, row.Match.Path))
					if err != nil {
						return err
					}
					if !selection.MatchesPath(filepath.ToSlash(path)) {
						return nil
					}
				}
				var message strings.Builder
				if err := rule.message.Execute(&message, row.Values); err != nil {
					return err
				}
				finding := models.NewViolationBuilder().WithMessage(message.String()).WithSource("uir").WithRuleFromLinter("uir", rule.ID).Build()
				finding.Severity = rule.Severity
				if row.Match.Path != "" {
					finding.File = filepath.Join(row.Match.Location, row.Match.Path)
					files[finding.File] = true
				}
				if row.Match.Line != nil {
					finding.Line = *row.Match.Line
				}
				if row.Match.Column != nil {
					finding.Column = *row.Match.Column
				}
				result.Violations = append(result.Violations, finding)
				return nil
			})
			for _, gap := range queried.Coverage {
				if gap.Coverage != string(storage.CoveragePartial) && gap.Coverage != string(storage.CoverageSyntax) {
					continue
				}
				key := gap.SnapshotID + ":" + gap.PackagePath
				if coverage[key] {
					continue
				}
				coverage[key] = true
				warning := models.NewViolationBuilder().WithMessage(fmt.Sprintf("%s: %s coverage in snapshot %s (%d diagnostics)", gap.PackagePath, gap.Coverage, gap.SnapshotID, gap.Diagnostics)).WithSource("uir").WithRuleFromLinter("uir", "uir/coverage").Build()
				warning.Severity = models.SeverityWarning
				result.Violations = append(result.Violations, warning)
			}
			result.FileCount = len(files)
			if err != nil {
				return fmt.Errorf("rule %s, snapshot %s: %w", rule.ID, scope.SnapshotID, err)
			}
		}
	}
	return nil
}

func snapshots(ctx context.Context, database *gorm.DB, config *Config, options Options) ([]query.ModuleScopeOptions, error) {
	if options.Snapshot != "" {
		return []query.ModuleScopeOptions{{SnapshotID: options.Snapshot}}, nil
	}
	engine, err := indexer.New(database)
	if err != nil {
		return nil, err
	}
	root := filepath.Dir(config.Path)
	var modules []indexer.ModuleResult
	if options.NoIndex {
		modules, err = engine.CheckModulesCurrent(ctx, root, indexer.CheckOptions{IncludeTests: config.Lint.IncludeTests, AllowIncomplete: true})
	} else {
		var run *indexer.ModuleIndexRun
		run, err = indexer.StartModulesTask(ctx, engine, []indexer.ModuleOptions{{Path: root, IncludeTests: config.Lint.IncludeTests, Reason: storage.ReasonReindex}})
		if err == nil {
			modules, err = run.Wait(ctx)
		}
	}
	if err != nil {
		return nil, err
	}
	if len(modules) == 0 {
		return nil, fmt.Errorf("no Go modules below %q", root)
	}
	scopes := make([]query.ModuleScopeOptions, 0, len(modules))
	for _, module := range modules {
		if module.Error != "" {
			return nil, fmt.Errorf("index %s: %s", module.Location, module.Error)
		}
		if module.SnapshotID == "" {
			return nil, fmt.Errorf("index %s returned no snapshot", module.Location)
		}
		scopes = append(scopes, query.ModuleScopeOptions{SnapshotID: module.SnapshotID})
	}
	return scopes, nil
}
