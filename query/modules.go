package query

import (
	"context"
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
	SymbolID     string         `json:"symbol_id,omitempty"`
	EnclosingID  string         `json:"enclosing_id,omitempty"`
	EnclosingKey string         `json:"enclosing_key,omitempty"`
	Coverage     string         `json:"coverage,omitempty"`
	Dispatch     bool           `json:"dispatch,omitempty"`
	Depth        int            `json:"depth,omitempty"`
}

type CallPath struct {
	Symbols []ModuleSymbol `json:"symbols"`
	Calls   []ModuleMatch  `json:"calls"`
}

type ModuleQueryResult struct {
	Operation    Operation         `json:"operation"`
	Matches      []ModuleMatch     `json:"matches"`
	Stages       []ResolutionStage `json:"stages"`
	Total        int               `json:"total"`
	Symbols      []ModuleSymbol    `json:"symbols,omitempty"`
	Declarations []ModuleMatch     `json:"declarations,omitempty"`
	Coverage     []ModuleCoverage  `json:"coverage"`
	Path         *CallPath         `json:"path,omitempty"`
}

type moduleScope struct {
	root     storage.ModuleRoot
	location storage.ModuleLocation
	snapshot storage.ModuleSnapshot
}

func (pipeline *Pipeline) RunModules(ctx context.Context, input string, options ModuleScopeOptions) (ModuleQueryResult, error) {
	if pipeline == nil || pipeline.database == nil {
		return ModuleQueryResult{}, errors.New("UIR query database is required")
	}
	parsed, err := Parse(input)
	if err != nil {
		return ModuleQueryResult{}, err
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
	scopes, err := pipeline.moduleScopes(ctx, options, false)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	coverage, err := pipeline.scopeCoverage(ctx, scopes)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	result := ModuleQueryResult{Operation: expressionOperation(parsed.Expr), Matches: []ModuleMatch{}, Coverage: coverage, Stages: []ResolutionStage{
		{Name: "parse", Value: string(expressionOperation(parsed.Expr))}, {Name: "scope", Value: fmt.Sprintf("%d snapshots", len(scopes))}, coverageStage(coverage),
	}}
	if err := pipeline.runCompact(ctx, parsed.Expr, scopes, &result); err != nil {
		return ModuleQueryResult{}, err
	}
	if len(result.Matches) > options.Limit {
		result.Matches = result.Matches[:options.Limit]
	}
	result.Stages = append(result.Stages, ResolutionStage{Name: "execute", Value: fmt.Sprintf("%d rows", result.Total)})
	return result, nil
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
