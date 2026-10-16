package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/storage"
)

func (index *compactIndex) evaluateSQL(ctx context.Context, source *sqlSource, result *ModuleQueryResult) (compactValue, bool, error) {
	if source.selector != nil {
		value, err := index.resolveSelector(ctx, *source.selector)
		return value, false, err
	}
	left, _, err := index.evaluateSQL(ctx, source.arguments[0], result)
	if err != nil {
		return compactValue{}, false, err
	}
	name := source.function.Name
	if name == "path" {
		right, _, err := index.evaluateSQL(ctx, source.arguments[1], result)
		if err != nil {
			return compactValue{}, false, err
		}
		var path *CallPath
		if len(source.options.via) > 0 {
			path, err = index.nativePath(ctx, left.symbols, right.symbols, source.options, result)
		} else {
			path, err = index.shortestPath(ctx, left.symbols, right.symbols, source.options.depth, result)
		}
		value := compactValue{path: path}
		if path != nil {
			value.symbols = path.Symbols
			value.matches = path.Calls
		}
		return value, false, err
	}
	if name == "traverse" || ((name == "callers" || name == "callees") && source.options.depth > 1) {
		value, err := index.sqlTraverse(ctx, left, source, result)
		return value, name == "callers", err
	}
	value, enclosing, err := index.sqlStep(ctx, left, name, source.options, result)
	return value, enclosing, err
}

func (index *compactIndex) sqlStep(ctx context.Context, left compactValue, name string, options sqlOptions, result *ModuleQueryResult) (compactValue, bool, error) {
	if name == "incoming" || name == "outgoing" {
		value, err := index.sqlNativeStep(ctx, left, name == "incoming", options.via, result)
		return value, name == "incoming", err
	}
	if name == "references" || name == "reads" || name == "writes" {
		targets := map[string]string{}
		for _, symbol := range left.symbols {
			targets[symbol.ID] = symbol.ID
		}
		matches, err := index.occurrences(ctx, occurrenceQuery{targets: targets, kind: "reference", keep: func(occurrence storage.DocumentOccurrence) bool {
			return name == "references" && occurrence.Role != "definition" || name == "reads" && accessRelationships[occurrence.Role] == uir.RelationshipTypeRead || name == "writes" && occurrence.Role == "write"
		}})
		if err != nil {
			return compactValue{}, false, err
		}
		symbols, err := index.project(ctx, matches, "", result)
		return compactValue{symbols: symbols, matches: matches}, false, err
	}
	relation := map[string]string{"definitions": "=", "callers": "<", "callees": ">", "methods": ":methods", "implementers": ":impl", "inheritors": ":inherits"}[name]
	if relation == "" {
		return compactValue{}, false, fmt.Errorf("unknown navigation %q", name)
	}
	if name == "callers" || name == "callees" {
		for _, symbol := range left.symbols {
			if !index.callable(symbol) {
				return compactValue{}, false, &InvalidQueryError{Message: fmt.Sprintf("%s requires a callable, got %s", name, symbol.Kind), Hint: "Use a callable selector, or native traversal with via for domain entities."}
			}
		}
	}
	if name == "inheritors" {
		if err := index.requireEmbeddingFacts(ctx); err != nil {
			return compactValue{}, false, err
		}
	}
	matches, err := index.relationMatches(ctx, &Expr{Kind: ExprRelation, Relation: relation}, left, nil, result)
	if err != nil {
		return compactValue{}, false, err
	}
	symbols, err := index.project(ctx, matches, relation, result)
	return compactValue{symbols: symbols, matches: matches}, name == "callers", err
}

func (index *compactIndex) sqlTraverse(ctx context.Context, left compactValue, source *sqlSource, result *ModuleQueryResult) (compactValue, error) {
	name := "callees"
	if source.function.Name == "callers" || source.options.direction == "incoming" {
		name = "callers"
	}
	if len(source.options.via) > 0 {
		name = "outgoing"
		if source.options.direction == "incoming" {
			name = "incoming"
		}
	}
	seen := map[string]int{}
	for _, symbol := range left.symbols {
		seen[symbol.ID] = 0
	}
	if len(seen) > maxGraphSymbols {
		return compactValue{}, sqlError(source.pos, "traversal exceeded %d symbols", maxGraphSymbols)
	}
	frontier := left
	var matches []ModuleMatch
	var symbols []ModuleSymbol
	for depth := 1; depth <= source.options.depth && len(frontier.symbols) > 0; depth++ {
		step, _, err := index.sqlStep(ctx, frontier, name, source.options, result)
		if err != nil {
			return compactValue{}, err
		}
		if name == "callees" {
			edges, err := index.forwardEdges(ctx, step.matches)
			if err != nil {
				return compactValue{}, err
			}
			step.symbols = nil
			step.matches = nil
			for _, edge := range edges {
				step.symbols = append(step.symbols, edge.symbol)
				step.matches = append(step.matches, edge.call)
			}
		}
		var next []ModuleSymbol
		for _, symbol := range step.symbols {
			if seenID(seen, symbol.ID) {
				continue
			}
			if len(seen) == maxGraphSymbols {
				return compactValue{}, fmt.Errorf("traversal exceeded %d symbols", maxGraphSymbols)
			}
			seen[symbol.ID] = depth
			next = append(next, symbol)
		}
		rows := step.matches
		if source.function.Name == "traverse" {
			rows, err = index.symbolRows(ctx, next)
			if err != nil {
				return compactValue{}, err
			}
		}
		for i := range rows {
			rows[i].Depth = depth
		}
		matches = append(matches, rows...)
		symbols = append(symbols, next...)
		frontier = compactValue{symbols: next, matches: rows}
	}
	return compactValue{symbols: symbols, matches: matches}, nil
}

func sortSQLRecords(records []sqlRecord) {
	slices.SortStableFunc(records, func(a, b sqlRecord) int {
		for _, field := range []string{"snapshot", "path", "line", "column", "symbol.id"} {
			compared := sqlCompareValues(a.values[field], b.values[field])
			if compared != 0 {
				return compared
			}
		}
		return 0
	})
}

func (pipeline *Pipeline) sqlGraph(ctx context.Context, index *compactIndex, source *sqlSource, scope ModuleScopeOptions, result ModuleQueryResult) (ModuleQueryResult, error) {
	value, _, err := index.evaluateSQL(ctx, source.arguments[0], &result)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	direction := graph.DirectionBoth
	if source.options.direction == "incoming" {
		direction = graph.DirectionCallers
	}
	if source.options.direction == "outgoing" {
		direction = graph.DirectionCallees
	}
	if len(source.options.via) > 0 {
		return pipeline.sqlNativeGraph(ctx, index, value, source, direction, result)
	}
	scope.Limit = 0
	options := GraphOptions{Selector: "sql", Direction: direction, Depth: source.options.depth, Limit: source.options.nodes, Exclude: source.options.exclude, Scope: scope}
	for _, access := range source.options.access {
		options.Access = append(options.Access, uir.RelationshipType(access))
	}
	options, exclusions, err := options.normalized()
	if err != nil {
		return ModuleQueryResult{}, err
	}
	roots, err := drawableRoots(index, value.symbols, options.Access)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	if len(roots) == 0 {
		return ModuleQueryResult{}, sqlError(source.pos, "graph source matched no symbols")
	}
	graphed := GraphResult{Graph: graph.Graph{Roots: []string{}, Nodes: []graph.Node{}, Edges: []graph.Edge{}}, Exclude: options.Exclude, Packages: []GraphPackage{}, Stages: result.Stages, Candidates: []ModuleSymbol{}}
	if len(roots) > 1 {
		graphed.Candidates = roots
	} else {
		graphed, err = pipeline.buildGraph(ctx, index, graphBuild{root: roots[0], options: options, exclusions: exclusions}, graphed)
		if err != nil {
			return ModuleQueryResult{}, err
		}
	}
	result.Graph = &graphed
	result.Symbols = value.symbols
	result.Total = len(graphed.Nodes)
	result.Stages = graphed.Stages
	return result, nil
}
