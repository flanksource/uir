package lint

import (
	"cmp"
	"slices"

	"github.com/flanksource/clicky/api"
	"github.com/flanksource/gavel/linters"
	"github.com/flanksource/gavel/models"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/query"
)

type Result struct {
	*linters.LinterResult
	GroupBy string  `json:"group_by,omitempty"`
	Groups  []Group `json:"groups,omitempty"`
	groups  map[groupKey]int
}

type groupKey struct{ file, symbol string }

type Group struct {
	File       string             `json:"file,omitempty"`
	SymbolID   string             `json:"symbol_id,omitempty"`
	Symbol     string             `json:"symbol,omitempty"`
	Violations []models.Violation `json:"violations"`
	workDir    string
}

func (result *Result) addFinding(finding models.Violation, symbol *query.ModuleSymbol) {
	result.Violations = append(result.Violations, finding)
	if result.GroupBy == "" {
		return
	}
	group := Group{File: finding.File, workDir: result.WorkDir}
	if result.GroupBy == "symbol" && symbol != nil {
		group.SymbolID, group.Symbol = symbol.ID, symbol.QueryName
	}
	if result.groups == nil {
		result.groups = map[groupKey]int{}
	}
	key := groupKey{file: group.File, symbol: group.SymbolID}
	position, exists := result.groups[key]
	if !exists {
		position = len(result.Groups)
		result.groups[key] = position
		result.Groups = append(result.Groups, group)
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
