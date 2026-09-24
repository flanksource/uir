package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
)

const maxGraphSymbols = 10000

type graphNode struct {
	symbol ModuleSymbol
	depth  int
}

type graphParent struct {
	from string
	call ModuleMatch
}

func callable(symbol ModuleSymbol) bool { return symbol.Kind == "func" || symbol.Kind == "method" }

func (index *compactIndex) transitiveCallers(ctx context.Context, targets []ModuleSymbol, depth int, filters []Filter, result *ModuleQueryResult) (compactValue, error) {
	queue := make([]graphNode, 0, len(targets))
	seen := map[string]int{}
	for _, target := range targets {
		if !callable(target) {
			return compactValue{}, fmt.Errorf("transitive callers require a function or method, got %s", target.Kind)
		}
		queue = append(queue, graphNode{symbol: target})
		seen[target.ID] = 0
	}
	for position := 0; position < len(queue); position++ {
		current := queue[position]
		if current.depth == depth {
			continue
		}
		rows, err := index.relationRows(ctx, current.symbol, "<", result)
		if err != nil {
			return compactValue{}, err
		}
		rows, err = applyCompactFilters(rows, "<", filters)
		if err != nil {
			return compactValue{}, err
		}
		ids := map[string]bool{}
		for _, row := range rows {
			if row.EnclosingID != "" && !seenID(seen, row.EnclosingID) {
				ids[row.EnclosingID] = true
			}
		}
		newIDs := sortedKeys(ids)
		callers, err := index.symbolsByID(ctx, newIDs)
		if err != nil {
			return compactValue{}, err
		}
		for _, caller := range callers {
			if !callable(caller) {
				continue
			}
			if len(seen) == maxGraphSymbols {
				return compactValue{}, fmt.Errorf("transitive caller traversal exceeded %d symbols", maxGraphSymbols)
			}
			seen[caller.ID] = current.depth + 1
			queue = append(queue, graphNode{symbol: caller, depth: current.depth + 1})
		}
	}
	ids := make([]string, 0, len(seen))
	for id, hops := range seen {
		if hops > 0 {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	symbols, err := index.symbolsByID(ctx, ids)
	if err != nil {
		return compactValue{}, err
	}
	matches, err := index.symbolRows(ctx, symbols)
	if err != nil {
		return compactValue{}, err
	}
	for position := range matches {
		matches[position].Depth = seen[matches[position].SymbolID]
	}
	return compactValue{symbols: symbols, matches: matches}, nil
}

func seenID(seen map[string]int, id string) bool {
	_, found := seen[id]
	return found
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func (index *compactIndex) shortestPath(ctx context.Context, sources, targets []ModuleSymbol, depth int, result *ModuleQueryResult) (*CallPath, error) {
	goal := map[string]bool{}
	for _, target := range targets {
		if callable(target) {
			goal[target.ID] = true
		}
	}
	if len(goal) == 0 {
		return nil, fmt.Errorf("call path target must be a function or method")
	}
	queue := make([]graphNode, 0, len(sources))
	seen := map[string]bool{}
	parents := map[string]graphParent{}
	for _, source := range sources {
		if callable(source) {
			queue = append(queue, graphNode{symbol: source})
			seen[source.ID] = true
		}
	}
	if len(queue) == 0 {
		return nil, fmt.Errorf("call path source must contain a function or method")
	}
	for position := 0; position < len(queue); position++ {
		current := queue[position]
		if goal[current.symbol.ID] {
			return index.buildPath(ctx, current.symbol.ID, parents)
		}
		if current.depth == depth {
			continue
		}
		calls, err := index.callees(ctx, current.symbol)
		if err != nil {
			return nil, err
		}
		edges, err := index.forwardEdges(ctx, calls)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			if seen[edge.symbol.ID] {
				continue
			}
			if len(seen) == maxGraphSymbols {
				return nil, fmt.Errorf("call path traversal exceeded %d symbols", maxGraphSymbols)
			}
			seen[edge.symbol.ID] = true
			parents[edge.symbol.ID] = graphParent{from: current.symbol.ID, call: edge.call}
			queue = append(queue, graphNode{symbol: edge.symbol, depth: current.depth + 1})
		}
	}
	result.Stages = append(result.Stages, ResolutionStage{Name: "path", Value: fmt.Sprintf("no indexed witness within %d hops", depth)})
	return nil, nil
}

type forwardEdge struct {
	symbol ModuleSymbol
	call   ModuleMatch
}

func (index *compactIndex) forwardEdges(ctx context.Context, calls []ModuleMatch) ([]forwardEdge, error) {
	ids := map[string]bool{}
	for _, call := range calls {
		if call.SymbolID != "" {
			ids[call.SymbolID] = true
		}
	}
	symbols, err := index.symbolsByID(ctx, sortedKeys(ids))
	if err != nil {
		return nil, err
	}
	byID := map[string]ModuleSymbol{}
	for _, symbol := range symbols {
		byID[symbol.ID] = symbol
	}
	var edges []forwardEdge
	for _, call := range calls {
		if call.SymbolID == "" {
			continue
		}
		target := byID[call.SymbolID]
		edges = append(edges, forwardEdge{symbol: target, call: call})
		implementations, err := index.methodImplementations(ctx, target)
		if err != nil {
			return nil, err
		}
		for _, implementation := range implementations {
			inferred := call
			inferred.SymbolID, inferred.Dispatch = implementation.ID, true
			edges = append(edges, forwardEdge{symbol: implementation, call: inferred})
		}
	}
	slices.SortFunc(edges, func(a, b forwardEdge) int {
		if a.symbol.QueryName != b.symbol.QueryName {
			if a.symbol.QueryName < b.symbol.QueryName {
				return -1
			}
			return 1
		}
		if a.symbol.ID < b.symbol.ID {
			return -1
		}
		if a.symbol.ID > b.symbol.ID {
			return 1
		}
		return 0
	})
	return edges, nil
}

func (index *compactIndex) methodImplementations(ctx context.Context, target ModuleSymbol) ([]ModuleSymbol, error) {
	if target.Kind != "method" || target.OwnerID == "" {
		return nil, nil
	}
	if cached, found := index.dispatch[target.ID]; found {
		return cached, nil
	}
	owner, err := index.symbolsByID(ctx, []string{target.OwnerID})
	if err != nil {
		return nil, err
	}
	implementers, err := index.implementations(ctx, owner[0])
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, implementer := range implementers {
		ids[implementer.SymbolID] = true
	}
	if len(ids) == 0 {
		index.dispatch[target.ID] = nil
		return nil, nil
	}
	var rows []storage.Symbol
	if err := index.database.WithContext(ctx).Where("owner_id IN ? AND kind = ? AND name = ?", sortedKeys(ids), "method", target.Name).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load implementations of %s: %w", target.QueryName, err)
	}
	rows = slices.DeleteFunc(rows, func(row storage.Symbol) bool {
		same, compareErr := sameParameters(row.ParameterTypes, target.ParameterTypes)
		if compareErr != nil {
			err = compareErr
		}
		return !same
	})
	if err != nil {
		return nil, err
	}
	symbols, err := index.activeSymbols(ctx, rows)
	if err != nil {
		return nil, err
	}
	index.dispatch[target.ID] = symbols
	return symbols, nil
}

func (index *compactIndex) buildPath(ctx context.Context, target string, parents map[string]graphParent) (*CallPath, error) {
	ids := []string{target}
	var calls []ModuleMatch
	for current := target; ; {
		parent, found := parents[current]
		if !found {
			break
		}
		calls = append(calls, parent.call)
		ids = append(ids, parent.from)
		current = parent.from
	}
	slices.Reverse(ids)
	slices.Reverse(calls)
	symbols, err := index.symbolsByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := map[string]ModuleSymbol{}
	for _, symbol := range symbols {
		byID[symbol.ID] = symbol
	}
	ordered := make([]ModuleSymbol, 0, len(ids))
	for _, id := range ids {
		ordered = append(ordered, byID[id])
	}
	return &CallPath{Symbols: ordered, Calls: calls}, nil
}
