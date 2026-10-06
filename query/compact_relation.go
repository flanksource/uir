package query

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

func (index *compactIndex) evaluateRelation(ctx context.Context, expression *Expr, result *ModuleQueryResult) (compactValue, error) {
	left, err := index.evaluate(ctx, expression.Left, result)
	if err != nil {
		return compactValue{}, err
	}
	if expression.Relation == ":inherits" {
		if err := index.requireEmbeddingFacts(ctx); err != nil {
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
	scope, scoped, err := relationScope(expression)
	if err != nil {
		return compactValue{}, err
	}
	var roots func(string) bool
	if scoped {
		roots = scope.admitsRoot
	}
	matches, err := index.relationMatches(ctx, expression, left, roots, result)
	if err != nil {
		return compactValue{}, err
	}
	value := compactValue{targets: left.symbols, matches: matches}
	switch {
	case scoped:
		value, err = index.filterRelationScope(ctx, value, expression.Relation, scope, result)
	case expression.Right != nil:
		if value.symbols, err = index.project(ctx, matches, expression.Relation, result); err == nil {
			value, err = index.filterRelationRight(ctx, value, expression, result)
		}
	default:
		value.symbols, err = index.project(ctx, matches, expression.Relation, result)
	}
	if err != nil {
		return compactValue{}, err
	}
	if len(left.symbols) == 1 && expression.Left.Kind == ExprSymbol && expression.Relation != "=" {
		value.declarations, err = index.declarations(ctx, []string{left.symbols[0].ID}, "definition")
	}
	return value, err
}

// relationScope is the right operand of an incoming relation as a scope predicate, when it is one.
func relationScope(expression *Expr) (scopePredicate, bool, error) {
	if expression.Relation != "<" && expression.Relation != "~w" {
		return scopePredicate{}, false, nil
	}
	return compileScopePredicate(expression.Right)
}

// relationMatches lists the relation's rows from every target on its left that the relation can start
// from, in the roots roots admits (every root when nil), keeps those in the scopes the left operand
// selected, and applies the relation's filters.
func (index *compactIndex) relationMatches(ctx context.Context, expression *Expr, left compactValue, roots func(string) bool, result *ModuleQueryResult) ([]ModuleMatch, error) {
	var matches []ModuleMatch
	var batch []ModuleSymbol
	compatible := 0
	for _, target := range left.symbols {
		if !index.relationAccepts(expression.Relation, target.Kind) {
			continue
		}
		compatible++
		if index.batchedRelation(expression.Relation, target) {
			batch = append(batch, target)
			continue
		}
		rows, err := index.relationRowsIn(ctx, target, expression.Relation, roots, result)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", target.QueryName, expression.Relation, err)
		}
		matches = append(matches, sourced(rows, expression.Relation, func(ModuleMatch) ModuleSymbol { return target })...)
	}
	if len(left.symbols) > 0 && compatible == 0 {
		return nil, fmt.Errorf("relation %s cannot start from the selected node kinds", expression.Relation)
	}
	batched, err := index.batchedRows(ctx, batch, expression.Relation, roots)
	if err != nil {
		return nil, err
	}
	matches = append(matches, batched...)
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
	return applyCompactFilters(matches, expression.Relation, expression.Filters)
}

// batchedRelation reports whether the target's rows of the relation are read together with the other
// such targets' in one pass over their postings: the references or writes of any symbol, and the calls
// of a callable that is not a method, whose callers include no dispatch.
func (index *compactIndex) batchedRelation(relation string, target ModuleSymbol) bool {
	switch relation {
	case "~w":
		return true
	case "<":
		return !index.callable(target) || target.Kind != symbolhandle.KindMethod.String()
	}
	return false
}

// batchedRows reads the incoming rows of every batched target at once: the calls of the callable
// ones and the other references or the writes of the rest, each row sourced from its target.
func (index *compactIndex) batchedRows(ctx context.Context, targets []ModuleSymbol, relation string, roots func(string) bool) ([]ModuleMatch, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	byID := map[string]ModuleSymbol{}
	calls, uses := map[string]string{}, map[string]string{}
	for _, target := range targets {
		byID[target.ID] = target
		if relation == "<" && index.callable(target) {
			calls[target.ID] = target.ID
		} else {
			uses[target.ID] = target.ID
		}
	}
	keep := func(occurrence storage.DocumentOccurrence) bool { return occurrence.Role != "definition" }
	if relation == "~w" {
		keep = func(occurrence storage.DocumentOccurrence) bool { return occurrence.Role == "write" }
	}
	var matches []ModuleMatch
	for _, read := range []occurrenceQuery{
		{targets: calls, kind: "caller", keep: isCall, roots: roots},
		{targets: uses, kind: "reference", keep: keep, roots: roots},
	} {
		if len(read.targets) == 0 {
			continue
		}
		rows, err := index.occurrences(ctx, read)
		if err != nil {
			return nil, fmt.Errorf("%d targets %s: %w", len(read.targets), relation, err)
		}
		matches = append(matches, sourced(rows, relation, func(row ModuleMatch) ModuleSymbol { return byID[read.targets[row.SymbolID]] })...)
	}
	return matches, nil
}

// sourced marks each row with the relation and the target source says it was read for.
func sourced(rows []ModuleMatch, relation string, source func(ModuleMatch) ModuleSymbol) []ModuleMatch {
	for position := range rows {
		target := source(rows[position])
		rows[position].Relation, rows[position].SourceID, rows[position].SourceName = relation, target.ID, target.QueryName
	}
	return rows
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
		if index.callable(symbol) {
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

// filterRelationScope keeps the incoming rows whose enclosing declaration the scope predicate admits in
// the row's own root: the rows the scope's symbols, listed as a set, would keep.
func (index *compactIndex) filterRelationScope(ctx context.Context, value compactValue, relation string, scope scopePredicate, result *ModuleQueryResult) (compactValue, error) {
	enclosing := map[string]bool{}
	for _, match := range value.matches {
		if match.EnclosingID != "" {
			enclosing[match.EnclosingID] = true
		}
	}
	rows, err := index.symbolRowsByID(ctx, sortedKeys(enclosing))
	if err != nil {
		return compactValue{}, err
	}
	value.matches = slices.DeleteFunc(value.matches, func(match ModuleMatch) bool {
		row, found := rows[match.EnclosingID]
		return !found || !scope.admits(match.RootKey, row.ModuleKey, row.PackagePath)
	})
	value.symbols, err = index.project(ctx, value.matches, relation, result)
	return value, err
}

func (index *compactIndex) relationRows(ctx context.Context, target ModuleSymbol, relation string, result *ModuleQueryResult) ([]ModuleMatch, error) {
	return index.relationRowsIn(ctx, target, relation, nil, result)
}

// relationRowsIn lists one target's rows of the relation, its incoming rows only in the roots roots
// admits (every root when nil).
func (index *compactIndex) relationRowsIn(ctx context.Context, target ModuleSymbol, relation string, roots func(string) bool, result *ModuleQueryResult) ([]ModuleMatch, error) {
	switch relation {
	case "<":
		if index.callable(target) {
			return index.incomingCalls(ctx, target, roots, result)
		}
		return index.occurrences(ctx, occurrenceQuery{targets: singleTarget(target.ID), kind: "reference", roots: roots, keep: func(occurrence storage.DocumentOccurrence) bool {
			return occurrence.Role != "definition"
		}})
	case ">":
		if !index.callable(target) {
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
		return index.methods(ctx, target)
	case "~w":
		return index.occurrences(ctx, occurrenceQuery{targets: singleTarget(target.ID), kind: "reference", roots: roots, keep: func(occurrence storage.DocumentOccurrence) bool {
			return occurrence.Role == "write"
		}})
	}
	return nil, fmt.Errorf("unknown relation %q", relation)
}

// incomingCalls lists the calls of a callable target, with the calls through the interface methods a
// method's receiver implements when its receiver is declared in scope.
func (index *compactIndex) incomingCalls(ctx context.Context, target ModuleSymbol, roots func(string) bool, result *ModuleQueryResult) ([]ModuleMatch, error) {
	dispatch := target.Kind == symbolhandle.KindMethod.String()
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
	return index.callers(ctx, target, dispatch, roots, partial)
}

func (index *compactIndex) methods(ctx context.Context, target ModuleSymbol) ([]ModuleMatch, error) {
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
	return index.symbolsByID(ctx, sortedKeys(ids))
}
