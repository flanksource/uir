package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
)

const (
	defaultLimit = 100
	maximumLimit = 1000
)

type ModuleScopeOptions struct {
	RootKey    string
	Location   string
	SnapshotID string
	Limit      int
}

type ModuleMatch struct {
	Kind         string         `json:"kind"`
	NodeKind     string         `json:"node_kind,omitempty"`
	RootKey      string         `json:"root_key"`
	Location     string         `json:"location"`
	SnapshotID   string         `json:"snapshot_id"`
	Path         string         `json:"path"`
	PackagePath  string         `json:"package_path,omitempty"`
	Line         *int           `json:"line,omitempty"`
	Column       *int           `json:"column,omitempty"`
	Identifier   uir.Identifier `json:"identifier"`
	EndLine      *int           `json:"end_line,omitempty"`
	EndColumn    *int           `json:"end_column,omitempty"`
	Role         string         `json:"role,omitempty"`
	Relation     string         `json:"relation,omitempty"`
	SourceID     string         `json:"source_id,omitempty"`
	SourceName   string         `json:"source_name,omitempty"`
	SymbolID     string         `json:"symbol_id,omitempty"`
	EnclosingID  string         `json:"enclosing_id,omitempty"`
	EnclosingKey string         `json:"enclosing_key,omitempty"`
	Coverage     string         `json:"coverage,omitempty"`
	Dispatch     bool           `json:"dispatch,omitempty"`
	Depth        int            `json:"depth,omitempty"`
	// Payload is the producer's payload on a declaration row's symbol (storage.DocumentSymbol.Payload),
	// as stored; occurrence rows and declarations without one carry none.
	Payload        json.RawMessage `json:"payload,omitempty"`
	Declaration    string          `json:"-"`
	DefinitionLine *int            `json:"-"`
	// span and text locate an occurrence row in its source and keep the call as the index recorded
	// it; the call graph derives a site's guards and label from them.
	span storage.ByteSpan
	text string
}

type CallPath struct {
	Symbols []ModuleSymbol `json:"symbols"`
	Calls   []ModuleMatch  `json:"calls"`
}

type ModuleQueryResult struct {
	Operation    Operation            `json:"operation"`
	Matches      []ModuleMatch        `json:"matches"`
	Stages       []ResolutionStage    `json:"stages"`
	Total        int                  `json:"total"`
	Symbols      []ModuleSymbol       `json:"symbols,omitempty"`
	Declarations []ModuleMatch        `json:"declarations,omitempty"`
	Coverage     []ModuleCoverage     `json:"coverage"`
	Warnings     []MissingHeadWarning `json:"warnings"`
	Path         *CallPath            `json:"path,omitempty"`
}

type moduleScope struct {
	root     storage.ModuleRoot
	location storage.ModuleLocation
	snapshot storage.ModuleSnapshot
}

func (pipeline *Pipeline) RunModules(ctx context.Context, input string, options ModuleScopeOptions) (ModuleQueryResult, error) {
	parsed, err := Parse(input)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	return pipeline.RunExpr(ctx, parsed.Expr, options)
}

func (pipeline *Pipeline) RunExpr(ctx context.Context, expression *Expr, options ModuleScopeOptions) (ModuleQueryResult, error) {
	if pipeline == nil || pipeline.database == nil {
		return ModuleQueryResult{}, errors.New("UIR query database is required")
	}
	if expression == nil {
		return ModuleQueryResult{}, errors.New("UIR query expression is required")
	}
	if options.Limit == 0 {
		options.Limit = defaultLimit
	}
	if options.Limit < 1 || options.Limit > maximumLimit {
		return ModuleQueryResult{}, fmt.Errorf("UIR query limit must be between 1 and %d, got %d", maximumLimit, options.Limit)
	}
	if options.Location != "" && options.SnapshotID != "" {
		return ModuleQueryResult{}, errors.New("location and snapshot selectors are mutually exclusive")
	}
	session, trace := pipeline.session()
	clock := newStageClock()
	selection, err := session.moduleScopes(ctx, options, false)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	clock.lap("scope")
	coverage, err := session.scopeCoverage(ctx, selection.scopes)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	clock.lap("coverage")
	result := ModuleQueryResult{Operation: expressionOperation(expression), Matches: []ModuleMatch{}, Coverage: coverage, Warnings: selection.warnings, Stages: []ResolutionStage{
		{Name: "parse", Value: string(expressionOperation(expression))}, {Name: "scope", Value: fmt.Sprintf("%d snapshots", len(selection.scopes))},
		selectionCoverageStage(selection, coverage),
	}}
	if err := session.execute(ctx, expression, selection, &result, clock); err != nil {
		return ModuleQueryResult{}, err
	}
	if len(result.Matches) > options.Limit {
		result.Matches = result.Matches[:options.Limit]
	}
	result.Stages = append(result.Stages, ResolutionStage{Name: "execute", Value: fmt.Sprintf("%d rows", result.Total)})
	result.Stages = append(result.Stages, clock.stages(trace)...)
	return result, nil
}

// execute evaluates the expression over the selected scopes into result. With nothing to evaluate, or
// a symbol no scope resolves while registered checkouts lack a head, the result has no rows.
func (pipeline *Pipeline) execute(ctx context.Context, expression *Expr, selection moduleScopeSelection, result *ModuleQueryResult, clock *stageClock) error {
	if len(selection.scopes) == 0 {
		return nil
	}
	err := pipeline.runCompact(ctx, expression, selection.scopes, result, clock)
	var unresolved *UnresolvedSymbolError
	if len(selection.warnings) > 0 && errors.As(err, &unresolved) {
		result.Total = 0
		return nil
	}
	return err
}

// selectionCoverageStage is the coverage stage, which counts the registered checkouts without an
// indexed head first.
func selectionCoverageStage(selection moduleScopeSelection, coverage []ModuleCoverage) ResolutionStage {
	stage := coverageStage(coverage)
	if len(selection.warnings) == 0 {
		return stage
	}
	stage.Value = fmt.Sprintf("incomplete: %d registered checkout without an indexed head", len(selection.warnings))
	if len(selection.warnings) > 1 {
		stage.Value = fmt.Sprintf("incomplete: %d registered checkouts without indexed heads", len(selection.warnings))
	}
	if len(coverage) > 0 {
		stage.Value += "; " + coverageSummary(coverage)
	}
	return stage
}

func canonicalLocation(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve location %q: %w", path, err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("canonicalize location %q: %w", path, err)
	}
	return canonical, nil
}
