package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

// symbolSubject is one symbol a subject rule may report on: a symbol of a kind its selector selects,
// defined by a snapshot of the scope.
type symbolSubject struct {
	snapshot   *scopeSnapshot
	symbol     storage.EffectiveSymbol
	id         string
	documents  []int64
	selected   string
	unselected string
}

// documentIDs are the ids of the subject's declaring documents, sorted.
func (subject *symbolSubject) documentIDs() []string {
	ids := make([]string, len(subject.documents))
	for i, ordinal := range subject.documents {
		ids[i] = subject.snapshot.documentIDs[ordinal].String()
	}
	slices.Sort(ids)
	return ids
}

// subjects lints a rule whose rows read only their subject symbol. A selected subject's closure is the
// symbol at the layer the rule reads, plus its declaring documents when the rule reads document facts or
// the symbol is declared in more than one document. A subject the selector does not select has as closure
// what selection reads: its identity, plus its private layer for struct:, which selects by the type form
// of its private shape. So a change to a symbol the rule does not select re-evaluates nothing, except a
// shape change under struct:. A reused finding whose declaration moved to
// another document is relocated there.
func (run *ruleRun) subjects(ctx context.Context) ([]finding, error) {
	kinds, err := run.selectorKinds(ctx)
	if err != nil || len(kinds) == 0 {
		return nil, errUnattributed
	}
	universe, err := run.universe(ctx, kinds)
	if err != nil {
		return nil, err
	}
	handles := slices.Sorted(maps.Keys(universe))
	subjects := make([]storage.LintSubject, len(handles))
	for i, handle := range handles {
		subjects[i] = storage.LintSubject{Handle: handle}
	}
	stored, err := storage.LintFindings(ctx, run.database, run.rule.ID, subjects)
	if err != nil {
		return nil, err
	}
	var findings []finding
	var relocated []storage.LintFinding
	stale := map[string]bool{}
	for _, handle := range handles {
		subject := universe[handle]
		row, found := stored[storage.LintSubject{Handle: handle}]
		switch {
		case run.reusable(row, found, storage.LintClosureUnselected, subject.unselected):
		case run.reusable(row, found, storage.LintClosureSubject, subject.selected):
			moved, located, err := run.relocate(ctx, &row, subject)
			if err != nil {
				return nil, err
			}
			if !located {
				stale[subject.id] = true
				continue
			}
			if moved {
				relocated = append(relocated, row)
			}
		default:
			stale[subject.id] = true
			continue
		}
		reused, err := run.reuse(row)
		if err != nil {
			return nil, err
		}
		findings = append(findings, reused...)
	}
	if len(stale) > 0 {
		evaluated, err := run.evaluateSubjects(ctx, universe, handles, stale)
		if err != nil {
			return nil, err
		}
		for _, row := range evaluated {
			var decoded []finding
			if err := json.Unmarshal(row.Findings, &decoded); err != nil {
				return nil, err
			}
			findings = append(findings, decoded...)
		}
		relocated = append(relocated, evaluated...)
	}
	return findings, storage.SaveLintFindings(ctx, run.database, relocated)
}

// universe is every symbol of the selected kinds the scope's snapshots define, by handle, with its
// declaring documents and both closure fingerprints. A handle two snapshots define cannot be attributed.
func (run *ruleRun) universe(ctx context.Context, kinds map[symbolhandle.Kind]bool) (map[int64]*symbolSubject, error) {
	universe := map[int64]*symbolSubject{}
	for _, snapshot := range run.scope.snapshots {
		symbols, err := snapshot.effectiveSymbols(ctx, run.database)
		if err != nil {
			return nil, err
		}
		definitions, err := snapshot.definitionDocuments(ctx, run.database)
		if err != nil {
			return nil, err
		}
		for _, symbol := range symbols {
			fields, err := symbolhandle.Unpack(symbol.Handle)
			if err != nil {
				return nil, err
			}
			if !kinds[fields.Kind] {
				continue
			}
			if _, duplicate := universe[symbol.Handle]; duplicate {
				return nil, errUnattributed
			}
			universe[symbol.Handle] = &symbolSubject{snapshot: snapshot, symbol: symbol, documents: definitions[symbol.Handle]}
		}
	}
	ids, err := storage.SymbolIDs(ctx, run.database, slices.Collect(maps.Keys(universe)))
	if err != nil {
		return nil, err
	}
	registry, err := storage.LoadSymbolKinds(ctx, run.database)
	if err != nil {
		return nil, err
	}
	kind, _, _ := strings.Cut(run.rule.requirements.Closure.Selector, ":")
	shapeSelected := kind == "struct"
	for handle, subject := range universe {
		subject.id = ids[handle]
		// Selection reads the symbol's identity and, for struct:, the type form of its private shape.
		unselected := run.header(storage.LintClosureUnselected)
		unselected.Text(subject.id)
		if shapeSelected {
			unselected.Text(fingerprintText(subject.symbol.PrivateFP))
		}
		subject.unselected = unselected.Sum()
		fields, _ := symbolhandle.Unpack(handle)
		spec, err := registry.Spec(fields.Kind)
		if err != nil {
			return nil, err
		}
		selected := run.header(storage.LintClosureSubject)
		selected.Text(subject.id)
		run.symbolLayer(selected, subject.symbol, spec.Category == symbolhandle.CategoryCallable)
		if run.rule.requirements.Closure.Document || len(subject.documents) > 1 {
			selected.Count(len(subject.documents))
			for _, id := range subject.documentIDs() {
				selected.Text(id)
			}
		}
		subject.selected = selected.Sum()
	}
	return universe, nil
}

// evaluateSubjects evaluates the rule over the stale subjects and builds their rows. Every row must
// belong to a stale subject the selector selected, and a subject with findings must have exactly one
// declaring document to relocate them from later.
func (run *ruleRun) evaluateSubjects(ctx context.Context, universe map[int64]*symbolSubject, handles []int64, stale map[string]bool) ([]storage.LintFinding, error) {
	selected := map[string]bool{}
	evaluated, err := run.evaluate(ctx, &query.SQLSubjects{Symbols: stale, Selected: func(id string) { selected[id] = true }})
	if err != nil {
		return nil, err
	}
	byWitness := map[string][]finding{}
	for _, result := range evaluated {
		if !stale[result.witness] || !selected[result.witness] {
			return nil, errUnattributed
		}
		byWitness[result.witness] = append(byWitness[result.witness], result.finding)
	}
	var rows []storage.LintFinding
	for _, handle := range handles {
		subject := universe[handle]
		if !stale[subject.id] {
			continue
		}
		source, fingerprint := storage.LintClosureUnselected, subject.unselected
		if selected[subject.id] {
			source, fingerprint = storage.LintClosureSubject, subject.selected
		}
		found := byWitness[subject.id]
		if len(found) > 0 && len(subject.documents) != 1 {
			return nil, errUnattributed
		}
		row, err := run.row(storage.LintSubject{Handle: handle}, source, fingerprint, found)
		if err != nil {
			return nil, err
		}
		row.ContentGeneration = new(subject.symbol.ContentGeneration)
		if len(subject.documents) == 1 {
			row.DocumentID = new(subject.snapshot.documentIDs[subject.documents[0]])
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// relocate moves a reused row's findings to the subject's declaring document when its declaration moved
// there since the row was produced. It reports whether it moved them, and whether it located the
// declaration: one it cannot locate is evaluated instead.
func (run *ruleRun) relocate(ctx context.Context, row *storage.LintFinding, subject *symbolSubject) (moved, located bool, err error) {
	if len(subject.documents) != 1 || row.DocumentID == nil || *row.DocumentID == subject.snapshot.documentIDs[subject.documents[0]] {
		return false, true, nil
	}
	var findings []finding
	if err := json.Unmarshal(row.Findings, &findings); err != nil {
		return false, false, fmt.Errorf("decode the stored findings of rule %s: %w", run.rule.ID, err)
	}
	if len(findings) > 0 {
		var document storage.Document
		if err := run.database.WithContext(ctx).Select("ordinal", "path_key", "content").Where("ordinal = ?", subject.documents[0]).Take(&document).Error; err != nil {
			return false, false, fmt.Errorf("load document %d to relocate lint findings: %w", subject.documents[0], err)
		}
		var content struct {
			Symbols []declaredName `json:"symbols"`
		}
		if err := json.Unmarshal(document.Content, &content); err != nil {
			return false, false, fmt.Errorf("decode document %d to relocate lint findings: %w", document.Ordinal, err)
		}
		index := slices.IndexFunc(content.Symbols, func(symbol declaredName) bool { return symbol.ID != nil && *symbol.ID == subject.id })
		if index < 0 {
			return false, false, nil
		}
		declaration := content.Symbols[index]
		for i := range findings {
			findings[i].Path, findings[i].Line, findings[i].Column = document.PathKey, new(declaration.Name[0]), new(declaration.Name[1])
			if findings[i].Symbol != nil {
				findings[i].Symbol.DeclarationKey = fmt.Sprintf("%s:%s:%d:%d", subject.snapshot.rootKey, document.PathKey, declaration.NameBytes[0], declaration.NameBytes[1])
			}
		}
		encoded, err := json.Marshal(findings)
		if err != nil {
			return false, false, err
		}
		row.Findings = storage.JSON(encoded)
	}
	row.DocumentID = new(subject.snapshot.documentIDs[subject.documents[0]])
	return true, true, nil
}

// declaredName is the part of a stored document symbol that locates its name.
type declaredName struct {
	ID        *string          `json:"id"`
	Name      storage.Range    `json:"name"`
	NameBytes storage.ByteSpan `json:"name_bytes"`
}
