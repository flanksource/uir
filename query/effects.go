package query

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/uir/effects"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

// EffectsOptions select the function, method or package-level variable whose effects to report: a
// compact selector, as `uir query` resolves it, or a canonical symbol id. Scope selects the snapshots.
type EffectsOptions struct {
	Selector string
	Symbol   string
	Scope    ModuleScopeOptions
}

// SymbolEffect is one effect a symbol has, with the shortest chain of calls and reads that reaches a
// symbol having it itself.
type SymbolEffect struct {
	Effect effects.Effect `json:"effect"`
	// Path names the symbols from the reported one to the one that has the effect itself, both included.
	Path []string `json:"path"`
	// Evidence is the source text, call or library symbol that has the effect.
	Evidence string `json:"evidence"`
}

// EffectsResult is a symbol's effects, sorted, and whether none breaks determinism. A selector that
// matches several symbols returns them as Candidates with no effects.
type EffectsResult struct {
	Symbol        *ModuleSymbol  `json:"symbol,omitempty"`
	Deterministic bool           `json:"deterministic"`
	Effects       []SymbolEffect `json:"effects"`
	Candidates    []ModuleSymbol `json:"candidates,omitempty"`
}

// Effects reports what a symbol of the selected snapshots does beyond computing from its inputs and
// the indexed source: its own effects and those of every symbol it calls, references as a function
// value, or reads or writes as a package-level variable, however deep.
func (pipeline *Pipeline) Effects(ctx context.Context, options EffectsOptions) (EffectsResult, error) {
	if pipeline == nil || pipeline.database == nil {
		return EffectsResult{}, errors.New("UIR query database is required")
	}
	if (options.Selector == "") == (options.Symbol == "") {
		return EffectsResult{}, &InvalidQueryError{Message: "effects take a selector or a symbol, exactly one", Hint: "Name one function, method or package-level variable."}
	}
	session, _ := pipeline.session()
	selection, err := session.moduleScopes(ctx, options.Scope, false)
	if err != nil {
		return EffectsResult{}, err
	}
	coverage, err := session.scopeCoverage(ctx, selection.scopes)
	if err != nil {
		return EffectsResult{}, err
	}
	result := EffectsResult{Effects: []SymbolEffect{}}
	if len(selection.scopes) == 0 {
		return result, nil
	}
	base, err := newIndexContext(ctx, session, selection.scopes)
	if err != nil {
		return EffectsResult{}, err
	}
	index := newCompactIndex(base)
	roots, err := index.graphRoots(ctx, GraphOptions{Selector: options.Selector, Symbol: options.Symbol, Access: graphAccessTypes}, coverage)
	if err != nil {
		return EffectsResult{}, err
	}
	if len(roots) > 1 {
		result.Candidates = roots
		return result, nil
	}
	analysis := newEffectAnalysis(index)
	set, err := analysis.of(ctx, roots[0].ID)
	if err != nil {
		return EffectsResult{}, err
	}
	result.Symbol, result.Deterministic = &roots[0], effects.Deterministic(set)
	result.Effects = analysis.explain(roots[0].ID, set)
	return result, nil
}

// effectNode is one symbol the analysis reached: the effects it has itself, each with its evidence,
// and the symbols whose effects it takes on.
type effectNode struct {
	label   string
	local   map[effects.Effect]string
	edges   []string
	order   int
	lowlink int
	stacked bool
}

// effectAnalysis computes the effects of symbols over the selected snapshots of one index, memoizing
// every symbol it reaches, so many symbols cost one walk of what they share. A cycle of calls is one
// strongly connected component whose members share every effect any of them reaches.
type effectAnalysis struct {
	index   *compactIndex
	nodes   map[string]*effectNode
	totals  map[string][]effects.Effect
	mutable map[string]bool
	stack   []string
	counter int
}

func newEffectAnalysis(index *compactIndex) *effectAnalysis {
	return &effectAnalysis{index: index, nodes: map[string]*effectNode{}, totals: map[string][]effects.Effect{}, mutable: map[string]bool{}}
}

// of is the sorted effects of a symbol.
func (analysis *effectAnalysis) of(ctx context.Context, id string) ([]effects.Effect, error) {
	if total, done := analysis.totals[id]; done {
		return total, nil
	}
	if err := analysis.visit(ctx, id); err != nil {
		return nil, err
	}
	return analysis.totals[id], nil
}

// visit is Tarjan's strongly connected components over the symbols reached from id: a component's
// effects are its members' own and those of every finished component it reaches.
func (analysis *effectAnalysis) visit(ctx context.Context, id string) error {
	node, err := analysis.expand(ctx, id)
	if err != nil {
		return err
	}
	node.order, node.lowlink, node.stacked = analysis.counter, analysis.counter, true
	analysis.counter++
	analysis.stack = append(analysis.stack, id)
	for _, next := range node.edges {
		if _, done := analysis.totals[next]; done {
			continue
		}
		child, seen := analysis.nodes[next]
		if !seen {
			if err := analysis.visit(ctx, next); err != nil {
				return err
			}
			node.lowlink = min(node.lowlink, analysis.nodes[next].lowlink)
		} else if child.stacked {
			node.lowlink = min(node.lowlink, child.order)
		}
	}
	if node.lowlink != node.order {
		return nil
	}
	position := slices.Index(analysis.stack, id)
	members := slices.Clone(analysis.stack[position:])
	analysis.stack = analysis.stack[:position]
	union := map[effects.Effect]bool{}
	for _, member := range members {
		analysis.nodes[member].stacked = false
		for effect := range analysis.nodes[member].local {
			union[effect] = true
		}
		for _, next := range analysis.nodes[member].edges {
			for _, effect := range analysis.totals[next] {
				union[effect] = true
			}
		}
	}
	total := slices.Sorted(maps.Keys(union))
	for _, member := range members {
		analysis.totals[member] = total
	}
	return nil
}

// expand describes a symbol once. A predeclared identifier and a library symbol the effects catalog
// knows have the effects the catalog gives them. A constant, field or type has none. A function,
// method or package-level variable declared in a document that records effects has its declaration's
// own effects, takes on those of what its body or initializer calls (including the methods a library
// it calls calls back), references as a function value,
// or reads or writes as a package-level variable, and writes or reads (once some indexed code writes
// it) package-level state itself; one with no such document, such as a library function the catalog
// does not know, is Unknown, as are an interface method and a call the index could not resolve.
func (analysis *effectAnalysis) expand(ctx context.Context, id string) (*effectNode, error) {
	symbols, err := analysis.index.symbolsByID(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	symbol := symbols[0]
	node := &effectNode{label: symbol.QueryName, local: map[effects.Effect]string{}}
	analysis.nodes[id] = node
	add := func(set []effects.Effect, evidence string) {
		for _, effect := range set {
			if _, found := node.local[effect]; !found {
				node.local[effect] = evidence
			}
		}
	}
	if symbol.Kind == symbolhandle.KindBuiltin.String() {
		add(effects.Builtin(symbol.Owner, symbol.Name), symbol.QueryName)
		return node, nil
	}
	if set, known := effects.Lookup(symbol.PackagePath, symbol.Owner, symbol.Name); known {
		add(set, symbol.QueryName)
		return node, nil
	}
	if !analysis.index.callable(symbol) && symbol.Kind != symbolhandle.KindVar.String() {
		return node, nil
	}
	postings, err := analysis.index.postings(ctx, []string{id}, "definition")
	if err != nil {
		return nil, err
	}
	if len(postings) == 0 {
		add([]effects.Effect{effects.Unknown}, symbol.QueryName+" is not indexed")
		return node, nil
	}
	for _, posting := range postings {
		document, err := analysis.index.document(ctx, posting)
		if err != nil {
			return nil, err
		}
		if !document.content.EffectsRecorded {
			add([]effects.Effect{effects.Unknown}, symbol.QueryName+" has no recorded effects; reindex it")
			continue
		}
		if owner, found := document.entries[symbol.OwnerID]; found && owner.TypeForm == "interface" {
			add([]effects.Effect{effects.Unknown}, symbol.QueryName+" runs whatever implements it")
		}
		for _, effect := range slices.Sorted(maps.Keys(document.entries[id].Effects)) {
			add([]effects.Effect{effect}, document.entries[id].Effects[effect])
		}
	}
	edges := map[string]bool{}
	for _, posting := range postings {
		document, err := analysis.index.document(ctx, posting)
		if err != nil {
			return nil, err
		}
		for _, callee := range document.entries[id].ImplicitCalls {
			edges[callee] = true
		}
	}
	calls, err := analysis.index.callees(ctx, symbol)
	if err != nil {
		return nil, err
	}
	for _, call := range calls {
		if call.SymbolID == "" {
			add([]effects.Effect{effects.Unknown}, call.text)
			continue
		}
		edges[call.SymbolID] = true
	}
	if err := analysis.accesses(ctx, symbol, add, edges); err != nil {
		return nil, err
	}
	delete(edges, id)
	node.edges = slices.Sorted(maps.Keys(edges))
	return node, nil
}

// accesses adds what a symbol's declaration does by naming other symbols: writing a package-level
// variable writes global state, reading one reads it when indexed code writes it, and either takes on
// the effects of its initializer; naming a function as a value takes on its effects, since it may be
// called.
func (analysis *effectAnalysis) accesses(ctx context.Context, symbol ModuleSymbol, add func([]effects.Effect, string), edges map[string]bool) error {
	uses, err := analysis.index.enclosedUses(ctx, symbol, map[string]bool{"reference": true, "write": true})
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, use := range uses {
		ids[use.SymbolID] = true
	}
	targets, err := analysis.index.symbolsByID(ctx, sortedKeys(ids))
	if err != nil {
		return err
	}
	byID := make(map[string]ModuleSymbol, len(targets))
	for _, target := range targets {
		byID[target.ID] = target
	}
	for _, use := range uses {
		target := byID[use.SymbolID]
		switch {
		case analysis.index.callable(target):
			edges[target.ID] = true
		case target.Kind == symbolhandle.KindVar.String():
			edges[target.ID] = true
			if use.Role == "write" {
				add([]effects.Effect{effects.GlobalWrite}, target.QueryName)
				continue
			}
			written, err := analysis.written(ctx, target.ID)
			if err != nil {
				return err
			}
			if written {
				add([]effects.Effect{effects.GlobalRead}, target.QueryName)
			}
		}
	}
	return nil
}

// written reports whether any declaration of the selected snapshots writes a package-level variable.
func (analysis *effectAnalysis) written(ctx context.Context, id string) (bool, error) {
	if written, known := analysis.mutable[id]; known {
		return written, nil
	}
	writes, err := analysis.index.occurrences(ctx, occurrenceQuery{targets: map[string]string{id: id}, kind: "reference", keep: func(occurrence storage.DocumentOccurrence) bool {
		return occurrence.Role == "write"
	}})
	if err != nil {
		return false, fmt.Errorf("find writes of %s: %w", id, err)
	}
	analysis.mutable[id] = len(writes) > 0
	return analysis.mutable[id], nil
}

// explain finds, for each effect of a symbol, the nearest symbol reached that has it itself.
func (analysis *effectAnalysis) explain(id string, set []effects.Effect) []SymbolEffect {
	parents := map[string]string{id: ""}
	queue := []string{id}
	found := map[effects.Effect]SymbolEffect{}
	for len(queue) > 0 && len(found) < len(set) {
		current := queue[0]
		queue = queue[1:]
		node := analysis.nodes[current]
		for effect, evidence := range node.local {
			if _, known := found[effect]; known {
				continue
			}
			var path []string
			for at := current; at != ""; at = parents[at] {
				path = append(path, analysis.nodes[at].label)
			}
			slices.Reverse(path)
			found[effect] = SymbolEffect{Effect: effect, Path: path, Evidence: evidence}
		}
		for _, next := range node.edges {
			if _, seen := parents[next]; !seen {
				parents[next] = current
				queue = append(queue, next)
			}
		}
	}
	explained := make([]SymbolEffect, 0, len(set))
	for _, effect := range set {
		explained = append(explained, found[effect])
	}
	return explained
}

// sqlRecordEffects sets symbol.effects, the record's symbol's effects sorted and joined by commas, and
// symbol.deterministic on each record whose symbol is a function, method or package-level variable; on
// any other record both read NULL. One analysis serves every record, so records share what they reach.
func (index *compactIndex) sqlRecordEffects(ctx context.Context, records []sqlRecord) error {
	analysis := newEffectAnalysis(index)
	for i := range records {
		values := records[i].values
		values["symbol.effects"], values["symbol.deterministic"] = nil, nil
		id, _ := values["symbol.id"].(string)
		kind, _ := values["symbol.kind"].(string)
		if id == "" || (index.category(kind) != symbolhandle.CategoryCallable && kind != symbolhandle.KindVar.String()) {
			continue
		}
		set, err := analysis.of(ctx, id)
		if err != nil {
			return err
		}
		names := make([]string, len(set))
		for j, effect := range set {
			names[j] = string(effect)
		}
		values["symbol.effects"], values["symbol.deterministic"] = strings.Join(names, ","), effects.Deterministic(set)
	}
	return nil
}
