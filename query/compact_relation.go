package query

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
)

func (index *compactIndex) evaluateRelation(ctx context.Context, expression *Expr, result *ModuleQueryResult) (compactValue, error) {
	left, err := index.evaluate(ctx, expression.Left, result)
	if err != nil {
		return compactValue{}, err
	}
	if expression.Relation == ":inherits" {
		if err := index.requireEmbeddingFacts(); err != nil {
			return compactValue{}, err
		}
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
	compatible := 0
	for _, target := range left.symbols {
		if !relationAccepts(expression.Relation, target.Kind) {
			continue
		}
		compatible++
		rows, err := index.relationRows(ctx, target, expression.Relation, result)
		if err != nil {
			return compactValue{}, fmt.Errorf("%s %s: %w", target.QueryName, expression.Relation, err)
		}
		for position := range rows {
			rows[position].Relation, rows[position].SourceID, rows[position].SourceName = expression.Relation, target.ID, target.QueryName
		}
		matches = append(matches, rows...)
	}
	if len(left.symbols) > 0 && compatible == 0 {
		return compactValue{}, fmt.Errorf("relation %s cannot start from the selected node kinds", expression.Relation)
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

func relationAccepts(relation, kind string) bool {
	switch relation {
	case ">":
		return kind == "func" || kind == "method"
	case ":impl", ":inherits", ":methods":
		return kind == "type"
	default:
		return kind != "module" && kind != "package"
	}
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
	case ":inherits":
		return index.embedders(ctx, target)
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
