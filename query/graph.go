package query

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/storage"
)

// GraphOptions select the root of a call graph and bound it. Exactly one of Selector and Symbol is
// set. A zero Direction, Depth, or Limit takes graph.DirectionBoth, graph.DefaultDepth, or
// graph.DefaultLimit. Scope selects the snapshots as it does for a query; its Limit must be zero.
type GraphOptions struct {
	// Selector is a compact symbol expression, resolved as `uir query` resolves it.
	Selector string
	// Symbol is the canonical id of a symbol, as a graph node carries it.
	Symbol    string
	Direction graph.Direction
	// Depth is how many calls away from the root the graph reaches, 1 through graph.MaxDepth.
	Depth int
	// Limit is the most nodes the graph holds, 1 through graph.MaxLimit.
	Limit int
	// Exclude are the package patterns whose nodes the graph leaves out, besides the root: an import
	// path, a path ending in /..., or the keywords ExcludeStd, ExcludeBuiltin and ExcludeExternal.
	// None takes DefaultGraphExclusions, and ExcludeNone alone excludes nothing.
	Exclude []string
	Scope   ModuleScopeOptions
}

// GraphResult is a call graph with how it was resolved. A selector that matches several functions
// or methods returns them as Candidates with an empty graph.
type GraphResult struct {
	graph.Graph
	// Exclude are the effective exclusion patterns.
	Exclude []string `json:"exclude"`
	// Packages are the packages reached while walking, drawn or excluded.
	Packages   []GraphPackage       `json:"packages"`
	Stages     []ResolutionStage    `json:"stages"`
	Warnings   []MissingHeadWarning `json:"warnings"`
	Candidates []ModuleSymbol       `json:"candidates"`
}

func invalidGraph(format string, arguments ...any) error {
	return &InvalidQueryError{Message: fmt.Sprintf(format, arguments...), Hint: fmt.Sprintf(
		"Give a selector or a symbol that names one function or method, direction %s, %s or %s, depth 1 through %d, and limit 1 through %d.",
		graph.DirectionCallees, graph.DirectionCallers, graph.DirectionBoth, graph.MaxDepth, graph.MaxLimit)}
}

// normalized fills in the defaults, validates the options, and parses the effective exclusions.
func (options GraphOptions) normalized() (GraphOptions, graphExclusions, error) {
	if options.Direction == "" {
		options.Direction = graph.DirectionBoth
	}
	if options.Depth == 0 {
		options.Depth = graph.DefaultDepth
	}
	if options.Limit == 0 {
		options.Limit = graph.DefaultLimit
	}
	if len(options.Exclude) == 0 {
		options.Exclude = slices.Clone(DefaultGraphExclusions)
	}
	if err := options.validate(); err != nil {
		return options, graphExclusions{}, err
	}
	exclusions, err := parseGraphExclusions(options.Exclude)
	return options, exclusions, err
}

func (options GraphOptions) validate() error {
	switch {
	case options.Selector == "" && options.Symbol == "":
		return invalidGraph("a call graph requires a selector or a symbol")
	case options.Selector != "" && options.Symbol != "":
		return invalidGraph("a call graph takes a selector or a symbol, not both")
	case options.Direction != graph.DirectionCallees && options.Direction != graph.DirectionCallers && options.Direction != graph.DirectionBoth:
		return invalidGraph("direction %q is not one of %s, %s, %s", options.Direction, graph.DirectionCallees, graph.DirectionCallers, graph.DirectionBoth)
	case options.Depth < 1 || options.Depth > graph.MaxDepth:
		return invalidGraph("depth %d is outside 1..%d", options.Depth, graph.MaxDepth)
	case options.Limit < 1 || options.Limit > graph.MaxLimit:
		return invalidGraph("limit %d is outside 1..%d", options.Limit, graph.MaxLimit)
	case options.Scope.Limit != 0:
		return invalidGraph("the node limit of a call graph is Limit, not the scope's row limit")
	case options.Scope.Location != "" && options.Scope.SnapshotID != "":
		return invalidGraph("location and snapshot selectors are mutually exclusive")
	}
	return nil
}

// Graph builds the call graph around one function or method of the selected snapshots: its callers,
// its callees, or both, breadth-first to Depth and Limit, leaving out the nodes of the packages
// Exclude matches. Each call site carries the whole call and the guards read from its hash-verified
// source; a source that cannot be recovered is listed in Omitted.UnreadableSource, and its sites
// carry the callee as indexed and no guards.
func (pipeline *Pipeline) Graph(ctx context.Context, options GraphOptions) (GraphResult, error) {
	if pipeline == nil || pipeline.database == nil {
		return GraphResult{}, errors.New("UIR query database is required")
	}
	options, exclusions, err := options.normalized()
	if err != nil {
		return GraphResult{}, err
	}
	selection, err := pipeline.moduleScopes(ctx, options.Scope, false)
	if err != nil {
		return GraphResult{}, err
	}
	coverage, err := pipeline.scopeCoverage(ctx, selection.scopes)
	if err != nil {
		return GraphResult{}, err
	}
	result := GraphResult{
		Graph:   graph.Graph{Roots: []string{}, Nodes: []graph.Node{}, Edges: []graph.Edge{}},
		Exclude: options.Exclude, Packages: []GraphPackage{}, Warnings: selection.warnings, Candidates: []ModuleSymbol{},
		Stages: []ResolutionStage{{Name: "scope", Value: fmt.Sprintf("%d snapshots", len(selection.scopes))}, coverageStage(coverage)},
	}
	if len(selection.scopes) == 0 {
		result.Stages = append(result.Stages, ResolutionStage{Name: "resolve", Value: "no indexed snapshot to resolve in"})
		return result, nil
	}
	base, err := newIndexContext(ctx, pipeline.database, selection.scopes)
	if err != nil {
		return GraphResult{}, err
	}
	index := newCompactIndex(base)
	roots, err := index.graphRoots(ctx, options, coverage)
	var unresolved *UnresolvedSymbolError
	if len(selection.warnings) > 0 && errors.As(err, &unresolved) {
		result.Stages = append(result.Stages, ResolutionStage{Name: "resolve", Value: "0 symbols"})
		return result, nil
	}
	if err != nil {
		return GraphResult{}, err
	}
	if len(roots) > 1 {
		result.Candidates = roots
		result.Stages = append(result.Stages, ResolutionStage{Name: "resolve", Value: fmt.Sprintf("%d candidates", len(roots))})
		return result, nil
	}
	return pipeline.buildGraph(ctx, index, graphBuild{root: roots[0], options: options, exclusions: exclusions}, result)
}

type graphBuild struct {
	root       ModuleSymbol
	options    GraphOptions
	exclusions graphExclusions
}

func (pipeline *Pipeline) buildGraph(ctx context.Context, index *compactIndex, request graphBuild, result GraphResult) (GraphResult, error) {
	sources := newScopeSources(index.indexContext)
	reader := newGuardReader(pipeline.database, sources)
	source := newIndexGraphSource(index, sources, reader)
	options := request.options
	built, err := graph.Build(ctx, source, []string{request.root.ID}, graph.Options{
		Direction: options.Direction, Depth: options.Depth, Limit: options.Limit,
		Exclude: func(node graph.Node) bool { return request.exclusions.excludes(source.packageFacts(node.Group)) },
	})
	if err != nil {
		return GraphResult{}, err
	}
	result.Graph = *built
	result.Packages = graphPackages(built, source.packages, request.exclusions)
	root := request.root
	result.Stages = append(result.Stages, ResolutionStage{Name: "resolve", Value: root.QueryName})
	result.Stages = append(result.Stages, source.stages...)
	if unreadable := reader.unreadablePaths(); len(unreadable) > 0 {
		result.Omitted.UnreadableSource = unreadable
		result.Stages = append(result.Stages, ResolutionStage{Name: "guards", Value: fmt.Sprintf(
			"%d sources unreadable, their call sites carry no guards; %s: %v", len(unreadable), unreadable[0], reader.unreadable[unreadable[0]])})
	}
	result.Stages = append(result.Stages, ResolutionStage{Name: "graph", Value: fmt.Sprintf("%d nodes, %d edges", len(built.Nodes), len(built.Edges))})
	return result, nil
}

// graphRoots resolves the functions and methods a graph may be rooted at: the one a symbol id
// names, or every one a selector matches. A selector that matches several returns them all, for the
// caller to choose among.
func (index *compactIndex) graphRoots(ctx context.Context, options GraphOptions, coverage []ModuleCoverage) ([]ModuleSymbol, error) {
	if options.Symbol != "" {
		var rows []storage.Symbol
		if err := index.database.WithContext(ctx).Where("id = ?", options.Symbol).Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load symbol %s: %w", options.Symbol, err)
		}
		symbols, err := index.activeSymbols(ctx, rows)
		if err != nil {
			return nil, err
		}
		if len(symbols) == 0 {
			return nil, &UnresolvedSymbolError{Symbol: options.Symbol, Coverage: coverageSummary(coverage)}
		}
		if !index.callable(symbols[0]) {
			return nil, invalidGraph("a call graph requires a function or method, %s is a %s", symbols[0].QueryName, symbols[0].Kind)
		}
		return symbols, nil
	}
	parsed, err := Parse(options.Selector)
	if err != nil {
		return nil, err
	}
	switch parsed.Expr.Kind {
	case ExprSymbol, ExprSelector, ExprModifier, ExprIntersection, ExprUnion:
	default:
		return nil, invalidGraph("a graph selector must select symbols, %q is a %s expression", options.Selector, parsed.Expr.Kind)
	}
	value, err := index.evaluate(ctx, parsed.Expr, &ModuleQueryResult{Coverage: coverage})
	var ambiguous ambiguousSymbols
	if errors.As(err, &ambiguous) {
		value.symbols, err = ambiguous.symbols, nil
	}
	if err != nil {
		return nil, err
	}
	var roots []ModuleSymbol
	for _, symbol := range value.symbols {
		if index.callable(symbol) {
			roots = append(roots, symbol)
		}
	}
	if len(roots) == 0 {
		return nil, invalidGraph("selector %q matched no function or method", options.Selector)
	}
	return roots, nil
}
