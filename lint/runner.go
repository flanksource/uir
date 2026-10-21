package lint

import (
	"github.com/flanksource/clicky"
	commonsContext "github.com/flanksource/commons/context"
	"github.com/flanksource/gavel/linters"
	"github.com/flanksource/gavel/models"
	"gorm.io/gorm"
)

type sqlLinter struct {
	database *gorm.DB
	config   *Config
	options  Options
	result   *Result
}

func (linter *sqlLinter) Run(ctx commonsContext.Context, _ *clicky.Task) ([]models.Violation, error) {
	err := execute(ctx, linter.database, linter.config, linter.options, linter.result)
	linter.result.filterFiles(linter.options.Files)
	linter.result.sortGroups()
	return linter.result.Violations, err
}

func (*sqlLinter) Name() string                              { return "uir" }
func (*sqlLinter) DefaultIncludes() []string                 { return nil }
func (*sqlLinter) DefaultExcludes() []string                 { return nil }
func (*sqlLinter) SupportsJSON() bool                        { return true }
func (*sqlLinter) JSONArgs() []string                        { return nil }
func (*sqlLinter) SupportsFix() bool                         { return false }
func (*sqlLinter) FixArgs() []string                         { return nil }
func (*sqlLinter) ValidateConfig(*models.LinterConfig) error { return nil }
func (linter *sqlLinter) GetFileCount() int                  { return linter.result.FileCount }
func (linter *sqlLinter) GetRuleCount() int                  { return linter.result.RuleCount }

func (result *Result) filterFiles(files []string) {
	if files == nil {
		return
	}
	linters.FilterViolationsByUserScope([]*linters.LinterResult{result.LinterResult}, result.WorkDir, files)
	groups := result.Groups[:0]
	for _, group := range result.Groups {
		filtered := &linters.LinterResult{Violations: group.Violations, WorkDir: result.WorkDir}
		linters.FilterViolationsByUserScope([]*linters.LinterResult{filtered}, result.WorkDir, files)
		if len(filtered.Violations) != 0 {
			group.Violations = filtered.Violations
			groups = append(groups, group)
		}
	}
	result.Groups = groups
	seen := map[string]bool{}
	for _, finding := range result.Violations {
		if finding.File != "" {
			seen[finding.File] = true
		}
	}
	result.FileCount = len(seen)
}
