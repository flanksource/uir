package query

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

type compactValue struct {
	symbols      []ModuleSymbol
	targets      []ModuleSymbol
	matches      []ModuleMatch
	declarations []ModuleMatch
	path         *CallPath
}

type ambiguousSymbols struct {
	symbols []ModuleSymbol
}

func (errorValue ambiguousSymbols) Error() string { return "symbol spelling is ambiguous" }

type UnresolvedSymbolError struct {
	Symbol   string
	Coverage string
}

func (err *UnresolvedSymbolError) Error() string {
	return fmt.Sprintf("symbol %q matched no indexed symbols; coverage %s", err.Symbol, err.Coverage)
}

func expressionOperation(expression *Expr) Operation {
	if expression == nil {
		return ""
	}
	switch expression.Kind {
	case ExprSymbol, ExprSelector, ExprModifier:
		return OperationResolve
	case ExprIntersection, ExprUnion:
		return OperationSet
	case ExprPath:
		return OperationPath
	case ExprRelation:
		switch {
		case expression.Relation == "<":
			return OperationIncoming
		case expression.Relation == ">":
			return OperationOutgoing
		case expression.Relation == "=":
			return OperationDefinition
		case expression.Relation == ":impl":
			return OperationImplementers
		case expression.Relation == ":inherits":
			return OperationInheritors
		case expression.Relation == ":methods":
			return OperationMethods
		case expression.Relation == "~w":
			return OperationIncoming
		case strings.HasPrefix(expression.Relation, "<<"):
			return OperationTransitiveCallers
		}
	}
	return ""
}

func (pipeline *Pipeline) runCompact(ctx context.Context, expression *Expr, scopes []moduleScope, result *ModuleQueryResult) error {
	base, err := newIndexContext(ctx, pipeline.database, scopes)
	if err != nil {
		return err
	}
	index := newCompactIndex(base)
	value, err := index.evaluate(ctx, expression, result)
	var ambiguous ambiguousSymbols
	if errors.As(err, &ambiguous) {
		result.Symbols = ambiguous.symbols
		result.Matches, err = index.candidates(ctx, ambiguous.symbols)
		if err != nil {
			return err
		}
		result.Total = len(result.Matches)
		result.Stages = append(result.Stages, ResolutionStage{Name: "resolve", Value: fmt.Sprintf("%d candidates", len(ambiguous.symbols))})
		return nil
	}
	if err != nil {
		return err
	}
	result.Symbols, result.Matches, result.Declarations, result.Path = value.symbols, value.matches, value.declarations, value.path
	if len(value.targets) > 0 {
		result.Symbols = value.targets
	}
	if value.path != nil {
		result.Total = 1
	} else {
		result.Total = len(value.matches)
	}
	sortMatches(result.Matches)
	return nil
}

func (index *compactIndex) evaluate(ctx context.Context, expression *Expr, result *ModuleQueryResult) (compactValue, error) {
	switch expression.Kind {
	case ExprSelector:
		return index.resolveSelector(ctx, *expression.Selector)
	case ExprModifier:
		return index.evaluateModifiers(ctx, expression, result)
	case ExprSymbol:
		symbols, err := index.resolve(ctx, expression.Symbol)
		if err != nil {
			return compactValue{}, err
		}
		if len(symbols) == 0 {
			return compactValue{}, &UnresolvedSymbolError{Symbol: expression.Symbol, Coverage: coverageSummary(result.Coverage)}
		}
		if len(symbols) > 1 && !strings.ContainsAny(expression.Symbol, "*?") {
			return compactValue{}, ambiguousSymbols{symbols: symbols}
		}
		matches, err := index.symbolRows(ctx, symbols)
		return compactValue{symbols: symbols, matches: matches}, err
	case ExprRelation:
		return index.evaluateRelation(ctx, expression, result)
	case ExprIntersection, ExprUnion:
		return index.evaluateSet(ctx, expression, result)
	case ExprPath:
		left, err := index.evaluate(ctx, expression.Left, result)
		if err != nil {
			return compactValue{}, err
		}
		right, err := index.evaluate(ctx, expression.Right, result)
		if err != nil {
			return compactValue{}, err
		}
		path, err := index.shortestPath(ctx, left.symbols, right.symbols, expression.Depth, result)
		return compactValue{path: path}, err
	}
	return compactValue{}, fmt.Errorf("unsupported compact expression %q", expression.Kind)
}

func (index *compactIndex) evaluateModifiers(ctx context.Context, expression *Expr, result *ModuleQueryResult) (compactValue, error) {
	value, err := index.evaluate(ctx, expression.Left, result)
	if err != nil {
		return compactValue{}, err
	}
	type group struct {
		positive, negative map[string]bool
		hasPositive        bool
	}
	groups := map[string]*group{}
	for _, modifier := range expression.Modifiers {
		var matched compactValue
		if modifier.Selector.Kind == "path" {
			glob, err := compileSelectorGlob(modifier.Selector.Pattern)
			if err != nil {
				return compactValue{}, err
			}
			for _, symbol := range value.symbols {
				if pathMatchesSymbol(glob, modifier.Selector.Pattern, symbol) {
					matched.symbols = append(matched.symbols, symbol)
				}
			}
		} else {
			matched, err = index.resolveSelector(ctx, modifier.Selector)
			if err != nil {
				return compactValue{}, err
			}
		}
		selected := groups[modifier.Selector.Kind]
		if selected == nil {
			selected = &group{positive: map[string]bool{}, negative: map[string]bool{}}
			groups[modifier.Selector.Kind] = selected
		}
		for _, symbol := range matched.symbols {
			if modifier.Include {
				selected.positive[symbol.ID] = true
			} else {
				selected.negative[symbol.ID] = true
			}
		}
		if modifier.Include {
			selected.hasPositive = true
		}
	}
	keep := map[string]bool{}
	value.symbols = slices.DeleteFunc(value.symbols, func(symbol ModuleSymbol) bool {
		for _, selected := range groups {
			if selected.hasPositive && !selected.positive[symbol.ID] || selected.negative[symbol.ID] {
				return true
			}
		}
		keep[symbol.ID] = true
		return false
	})
	omitted := 0
	value.matches = slices.DeleteFunc(value.matches, func(match ModuleMatch) bool {
		id := projectedID(expression.Left, match)
		if id == "" {
			omitted++
		}
		return !keep[id]
	})
	if omitted > 0 {
		result.Stages = append(result.Stages, ResolutionStage{Name: "modifier", Value: fmt.Sprintf("%d rows have no canonical projected symbol", omitted)})
	}
	return value, nil
}

func pathMatchesSymbol(glob selectorGlob, pattern string, symbol ModuleSymbol) bool {
	paths := []string{symbol.ModuleKey, symbol.PackagePath, symbol.QueryName}
	for parent := symbol.QueryName; parent != symbol.PackagePath && strings.HasPrefix(parent, symbol.PackagePath+"."); {
		parent = parent[:strings.LastIndex(parent, ".")]
		paths = append(paths, parent)
	}
	for parent := symbol.PackagePath; parent != symbol.ModuleKey && strings.HasPrefix(parent, symbol.ModuleKey+"/"); {
		parent = parent[:strings.LastIndex(parent, "/")]
		paths = append(paths, parent)
	}
	for _, path := range paths {
		if glob.matches(path) || strings.HasSuffix(pattern, "/**") && strings.TrimSuffix(pattern, "/**") == path {
			return true
		}
	}
	return false
}

func (index *compactIndex) evaluateSet(ctx context.Context, expression *Expr, result *ModuleQueryResult) (compactValue, error) {
	left, err := index.evaluate(ctx, expression.Left, result)
	if err != nil {
		return compactValue{}, err
	}
	right, err := index.evaluate(ctx, expression.Right, result)
	if err != nil {
		return compactValue{}, err
	}
	byID := map[string]ModuleSymbol{}
	for _, symbol := range left.symbols {
		byID[symbol.ID] = symbol
	}
	if expression.Kind == ExprIntersection {
		kept := map[string]ModuleSymbol{}
		for _, symbol := range right.symbols {
			if _, found := byID[symbol.ID]; found {
				kept[symbol.ID] = symbol
			}
		}
		byID = kept
	} else {
		for _, symbol := range right.symbols {
			byID[symbol.ID] = symbol
		}
	}
	leftScopes, rightScopes := valueScopes(left, expression.Left), valueScopes(right, expression.Right)
	selectedScopes := map[string]map[string]bool{}
	for id := range byID {
		selectedScopes[id] = map[string]bool{}
		for scope := range leftScopes[id] {
			if expression.Kind == ExprUnion || len(rightScopes[id]) == 0 || rightScopes[id][scope] {
				selectedScopes[id][scope] = true
			}
		}
		for scope := range rightScopes[id] {
			if expression.Kind == ExprUnion || len(leftScopes[id]) == 0 {
				selectedScopes[id][scope] = true
			}
		}
		if expression.Kind == ExprIntersection && len(leftScopes[id]) > 0 && len(rightScopes[id]) > 0 && len(selectedScopes[id]) == 0 {
			delete(byID, id)
		}
	}
	symbols := make([]ModuleSymbol, 0, len(byID))
	for _, symbol := range byID {
		symbols = append(symbols, symbol)
	}
	slices.SortFunc(symbols, func(a, b ModuleSymbol) int { return strings.Compare(a.QueryName, b.QueryName) })
	var matches []ModuleMatch
	if expression.Kind == ExprUnion && (expression.Left.Kind == ExprRelation || expression.Right.Kind == ExprRelation) {
		matches = append(matches, left.matches...)
		matches = append(matches, right.matches...)
		sortMatches(matches)
	} else {
		matches, err = index.symbolRows(ctx, symbols)
	}
	matches = slices.DeleteFunc(matches, func(match ModuleMatch) bool {
		return len(selectedScopes[match.SymbolID]) > 0 && match.SnapshotID != "" && !selectedScopes[match.SymbolID][matchScope(match)]
	})
	return compactValue{symbols: symbols, matches: matches}, err
}

func valueScopes(value compactValue, expression *Expr) map[string]map[string]bool {
	selected := map[string]map[string]bool{}
	for _, match := range value.matches {
		id := projectedID(expression, match)
		if id == "" || match.SnapshotID == "" {
			continue
		}
		if selected[id] == nil {
			selected[id] = map[string]bool{}
		}
		selected[id][matchScope(match)] = true
	}
	return selected
}

func projectedID(expression *Expr, match ModuleMatch) string {
	for expression != nil && expression.Kind == ExprModifier {
		expression = expression.Left
	}
	if expression != nil && expression.Kind == ExprRelation && (expression.Relation == "<" || expression.Relation == "~w") {
		return match.EnclosingID
	}
	return match.SymbolID
}

func matchScope(match ModuleMatch) string {
	return match.RootKey + "\x00" + match.Location + "\x00" + match.SnapshotID
}

func (index *compactIndex) symbolRows(ctx context.Context, symbols []ModuleSymbol) ([]ModuleMatch, error) {
	ids := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		if symbol.Kind != "module" && symbol.Kind != "package" {
			ids = append(ids, symbol.ID)
		}
	}
	rows, err := index.declarations(ctx, ids, "symbol")
	if err != nil {
		return nil, err
	}
	defined := map[string]bool{}
	for _, row := range rows {
		defined[row.SymbolID] = true
	}
	for _, symbol := range symbols {
		if symbol.Kind == "module" || symbol.Kind == "package" {
			selected, err := index.resolveSelector(ctx, Selector{Kind: symbol.Kind, Pattern: symbol.PackagePath})
			if err != nil {
				return nil, err
			}
			rows = append(rows, selected.matches...)
			continue
		}
		if !defined[symbol.ID] {
			rows = append(rows, ModuleMatch{Kind: "symbol", SymbolID: symbol.ID, Identifier: symbol.identifier()})
		}
	}
	sortMatches(rows)
	return rows, nil
}
