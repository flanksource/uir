package lint

import (
	"cmp"
	"slices"
	"time"

	"github.com/flanksource/clicky/api"
	"github.com/flanksource/gavel/linters"
	"github.com/flanksource/gavel/models"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/internal/canonicalhash"
	"github.com/flanksource/uir/query"
)

type Result struct {
	*linters.LinterResult
	GroupBy string  `json:"group_by,omitempty"`
	Groups  []Group `json:"groups,omitempty"`
	// Cache reports how the run used stored findings; it is nil when no rule ran.
	Cache  *Cache `json:"cache,omitempty"`
	groups map[groupKey]int
}

// Cache reports how a run used the findings UIR stores: Source is always "uir", Fingerprint digests
// every (rule version, subject, closure fingerprint) pair the run reported, StoredAt is when the oldest
// reused pair was produced, and Hit is true only when every pair was reused.
type Cache struct {
	Source      string      `json:"source"`
	Fingerprint string      `json:"fingerprint"`
	StoredAt    *time.Time  `json:"stored_at,omitempty"`
	Hit         bool        `json:"hit"`
	Evaluated   int         `json:"evaluated"`
	Reused      int         `json:"reused"`
	Rules       []RuleCache `json:"rules"`
}

// RuleCache is how one rule used stored findings: the closure its pairs were checked by, and how many
// (rule, subject) pairs it evaluated and reused.
type RuleCache struct {
	ID        string               `json:"id"`
	Closure   query.SQLClosureKind `json:"closure"`
	Evaluated int                  `json:"evaluated"`
	Reused    int                  `json:"reused"`
}

// cacheReport accumulates a run's Cache.
type cacheReport struct {
	now    time.Time
	cache  Cache
	pairs  []string
	active bool
}

func newCacheReport(now time.Time) *cacheReport {
	return &cacheReport{now: now, cache: Cache{Source: "uir", Rules: []RuleCache{}}}
}

func (report *cacheReport) add(run *ruleRun) {
	report.active = true
	report.cache.Rules = append(report.cache.Rules, run.stats)
	report.cache.Evaluated += run.stats.Evaluated
	report.cache.Reused += run.stats.Reused
	report.pairs = append(report.pairs, run.pairs...)
	if run.stored != nil && (report.cache.StoredAt == nil || run.stored.Before(*report.cache.StoredAt)) {
		report.cache.StoredAt = run.stored
	}
}

func (report *cacheReport) finish(result *Result) {
	if !report.active {
		return
	}
	digest := canonicalhash.New(closureVersion)
	slices.Sort(report.pairs)
	digest.Count(len(report.pairs))
	for _, pair := range report.pairs {
		digest.Text(pair)
	}
	report.cache.Fingerprint = digest.Sum()
	report.cache.Hit = report.cache.Evaluated == 0 && report.cache.Reused > 0
	result.Cache = &report.cache
}

type groupKey struct{ file, symbol string }

type Group struct {
	DeclarationKey string             `json:"declaration_key,omitempty"`
	File           string             `json:"file,omitempty"`
	SymbolID       string             `json:"symbol_id,omitempty"`
	Symbol         string             `json:"symbol,omitempty"`
	Violations     []models.Violation `json:"violations"`
	workDir        string
}

func (result *Result) addFinding(finding models.Violation, symbol *query.ModuleSymbol) {
	result.Violations = append(result.Violations, finding)
	if result.GroupBy == "" {
		return
	}
	group := Group{File: finding.File, workDir: result.WorkDir}
	if result.GroupBy == "symbol" && symbol != nil {
		group.SymbolID, group.Symbol = symbol.ID, symbol.QueryName
		group.DeclarationKey = symbol.DeclarationKey
	}
	if result.groups == nil {
		result.groups = map[groupKey]int{}
	}
	key := groupKey{file: group.File, symbol: group.SymbolID}
	if group.DeclarationKey != "" {
		key.symbol = group.DeclarationKey
	}
	position, exists := result.groups[key]
	if !exists {
		position = len(result.Groups)
		result.groups[key] = position
		result.Groups = append(result.Groups, group)
	}
	if group.SymbolID != "" {
		result.Groups[position].SymbolID = group.SymbolID
	}
	result.Groups[position].Violations = append(result.Groups[position].Violations, finding)
}

func (result *Result) sortGroups() {
	slices.SortFunc(result.Groups, func(left, right Group) int {
		return cmp.Or(cmp.Compare(left.File, right.File), cmp.Compare(left.Symbol, right.Symbol), cmp.Compare(left.SymbolID, right.SymbolID))
	})
	result.groups = nil
}

func (result *Result) GetChildren() []api.TreeNode {
	if result.GroupBy != "symbol" {
		return result.LinterResult.GetChildren()
	}
	children := make([]api.TreeNode, len(result.Groups))
	for i := range result.Groups {
		children[i] = &result.Groups[i]
	}
	return children
}

func (group *Group) Pretty() api.Text {
	if group.File == "" {
		return api.Text{}.Append("Run diagnostics", uir.StyleWarning)
	}
	if group.Symbol == "" {
		return api.Text{}.Append("File findings", uir.StyleFileName)
	}
	return api.Text{}.Append(group.Symbol, uir.StyleDeclarationName).Space().Append("(").Append(len(group.Violations)).Append(")", uir.StyleTextMuted)
}

func (group *Group) GetChildren() []api.TreeNode {
	files := (&linters.LinterResult{WorkDir: group.workDir, Violations: group.Violations}).GetChildren()
	if group.File == "" && len(files) == 1 {
		return files[0].GetChildren()
	}
	return files
}
