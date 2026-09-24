package query

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
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
			return compactValue{}, fmt.Errorf("symbol %q matched no indexed symbols; coverage %s", expression.Symbol, coverageSummary(result.Coverage))
		}
		if len(symbols) > 1 && !strings.HasSuffix(expression.Symbol, ".*") {
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
		matched, err := index.resolveSelector(ctx, modifier.Selector)
		if err != nil {
			return compactValue{}, err
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
	matches, err := index.symbolRows(ctx, symbols)
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

func (index *compactIndex) evaluateRelation(ctx context.Context, expression *Expr, result *ModuleQueryResult) (compactValue, error) {
	left, err := index.evaluate(ctx, expression.Left, result)
	if err != nil {
		return compactValue{}, err
	}
	if strings.HasPrefix(expression.Relation, "<<") {
		value, err := index.transitiveCallers(ctx, left.symbols, expression.Depth, expression.Filters, result)
		if err != nil || expression.Right == nil {
			return value, err
		}
		value.targets = left.symbols
		return index.filterRelationRight(ctx, value, expression, result)
	}
	var matches []ModuleMatch
	for _, target := range left.symbols {
		rows, err := index.relationRows(ctx, target, expression.Relation, result)
		if err != nil {
			return compactValue{}, fmt.Errorf("%s %s: %w", target.QueryName, expression.Relation, err)
		}
		matches = append(matches, rows...)
	}
	if len(left.matches) > 0 {
		activeScopes := map[string]map[string]bool{}
		for _, match := range left.matches {
			if activeScopes[match.RootKey] == nil {
				activeScopes[match.RootKey] = map[string]bool{}
			}
			activeScopes[match.RootKey][matchScope(match)] = true
		}
		matches = slices.DeleteFunc(matches, func(match ModuleMatch) bool {
			return len(activeScopes[match.RootKey]) > 0 && !activeScopes[match.RootKey][matchScope(match)]
		})
	}
	matches, err = applyCompactFilters(matches, expression.Relation, expression.Filters)
	if err != nil {
		return compactValue{}, err
	}
	symbols, err := index.project(ctx, matches, expression.Relation, result)
	if err != nil {
		return compactValue{}, err
	}
	value := compactValue{symbols: symbols, targets: left.symbols, matches: matches}
	if expression.Right != nil {
		value, err = index.filterRelationRight(ctx, value, expression, result)
		if err != nil {
			return compactValue{}, err
		}
	}
	if len(left.symbols) == 1 && expression.Left.Kind == ExprSymbol && expression.Relation != "=" {
		value.declarations, err = index.declarations(ctx, []string{left.symbols[0].ID}, "definition")
	}
	return value, err
}

func (index *compactIndex) filterRelationRight(ctx context.Context, value compactValue, expression *Expr, result *ModuleQueryResult) (compactValue, error) {
	right, err := index.evaluate(ctx, expression.Right, result)
	if err != nil {
		return compactValue{}, err
	}
	allowed := map[string]bool{}
	scoped := map[string]map[string]bool{}
	callables := 0
	for _, symbol := range right.symbols {
		allowed[symbol.ID] = true
		if callable(symbol) {
			callables++
		}
	}
	for _, match := range right.matches {
		id := projectedID(expression.Right, match)
		if id != "" && match.SnapshotID != "" {
			if scoped[id] == nil {
				scoped[id] = map[string]bool{}
			}
			scoped[id][matchScope(match)] = true
		}
	}
	if expression.Relation == ">" && len(right.symbols) > 0 && callables == 0 {
		return compactValue{}, fmt.Errorf("outgoing relation requires a function or method on the right")
	}
	value.matches = slices.DeleteFunc(value.matches, func(match ModuleMatch) bool {
		id := match.SymbolID
		if expression.Relation == "<" || expression.Relation == "~w" {
			id = match.EnclosingID
		}
		return !allowed[id] || len(scoped[id]) > 0 && !scoped[id][matchScope(match)]
	})
	value.symbols, err = index.project(ctx, value.matches, expression.Relation, result)
	return value, err
}

func matchScope(match ModuleMatch) string {
	return match.RootKey + "\x00" + match.Location + "\x00" + match.SnapshotID
}

func (index *compactIndex) relationRows(ctx context.Context, target ModuleSymbol, relation string, result *ModuleQueryResult) ([]ModuleMatch, error) {
	switch relation {
	case "<":
		if target.Kind == "func" || target.Kind == "method" {
			dispatch := target.Kind == "method"
			if dispatch {
				owner, err := index.declarations(ctx, []string{target.OwnerID}, "definition")
				if err != nil {
					return nil, err
				}
				if len(owner) == 0 {
					dispatch = false
					result.Stages = append(result.Stages, ResolutionStage{Name: "dispatch", Value: "receiver declaration outside selected snapshots"})
				}
			}
			partial := &ModuleQueryResult{}
			if err := index.callers(ctx, target, dispatch, partial); err != nil {
				return nil, err
			}
			return partial.Matches, nil
		}
		return index.occurrences(ctx, target.ID, map[string]bool{target.ID: true}, "reference", func(occurrence storage.DocumentOccurrence) bool {
			return occurrence.Role != "definition"
		})
	case ">":
		if target.Kind != "func" && target.Kind != "method" {
			return nil, fmt.Errorf("outgoing calls require a function or method, got %s", target.Kind)
		}
		return index.callees(ctx, target)
	case "=":
		return index.declarations(ctx, []string{target.ID}, "definition")
	case ":impl":
		return index.implementations(ctx, target)
	case ":methods":
		if target.Kind != "type" {
			return nil, fmt.Errorf("methods require a type, got %s", target.Kind)
		}
		var rows []storage.Symbol
		if err := index.database.WithContext(ctx).Where("owner_id = ? AND kind = ?", target.ID, "method").Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load methods of %s: %w", target.QueryName, err)
		}
		methods, err := index.activeSymbols(ctx, rows)
		if err != nil {
			return nil, err
		}
		return index.symbolRows(ctx, methods)
	case "~w":
		return index.occurrences(ctx, target.ID, map[string]bool{target.ID: true}, "reference", func(occurrence storage.DocumentOccurrence) bool {
			return occurrence.Role == "write"
		})
	}
	return nil, fmt.Errorf("unknown relation %q", relation)
}

func applyCompactFilters(matches []ModuleMatch, relation string, filters []Filter) ([]ModuleMatch, error) {
	for _, filter := range filters {
		if filter.Kind == "~w" && relation != "<" && relation != "~w" {
			return nil, fmt.Errorf("~w applies only to incoming references, not %s", relation)
		}
		matches = slices.DeleteFunc(matches, func(match ModuleMatch) bool {
			switch filter.Kind {
			case "-f":
				return strings.HasSuffix(match.Path, filter.Value)
			case "+pkg":
				return !matchesPackageFilter(match.PackagePath, filter.Value)
			case "-pkg":
				return matchesPackageFilter(match.PackagePath, filter.Value)
			case "~w":
				return match.Role != "write"
			}
			return true
		})
	}
	return matches, nil
}

func matchesPackageFilter(packagePath, value string) bool {
	if strings.HasSuffix(value, "/...") {
		prefix := strings.TrimSuffix(value, "/...")
		return packagePath == prefix || strings.HasPrefix(packagePath, prefix+"/")
	}
	if strings.Contains(value, "/") {
		return packagePath == value
	}
	return packagePath == value || strings.HasSuffix(packagePath, "/"+value)
}

func (index *compactIndex) project(ctx context.Context, matches []ModuleMatch, relation string, result *ModuleQueryResult) ([]ModuleSymbol, error) {
	ids := map[string]bool{}
	omitted := 0
	for _, match := range matches {
		id := match.SymbolID
		if relation == "<" || relation == "~w" {
			id = match.EnclosingID
		}
		if id == "" {
			omitted++
			continue
		}
		ids[id] = true
	}
	if omitted > 0 {
		result.Stages = append(result.Stages, ResolutionStage{Name: "projection", Value: fmt.Sprintf("%d occurrences have no canonical symbol", omitted)})
	}
	keys := make([]string, 0, len(ids))
	for id := range ids {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	return index.symbolsByID(ctx, keys)
}

func (index *compactIndex) symbolRows(ctx context.Context, symbols []ModuleSymbol) ([]ModuleMatch, error) {
	ids := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		ids = append(ids, symbol.ID)
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
		if !defined[symbol.ID] {
			rows = append(rows, ModuleMatch{Kind: "symbol", SymbolID: symbol.ID, Identifier: symbol.identifier()})
		}
	}
	sortMatches(rows)
	return rows, nil
}
