package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

// recordDeclaration is the declaration a record's snapshot selects for its symbol.
type recordDeclaration struct {
	scope  indexScope
	active storage.ActiveDocument
	key    declarationMetricKey
}

// recordDeclarations resolves every record whose symbol id is in ids to its selected declaration, by
// record index, and returns the definition postings it read.
func (index *compactIndex) recordDeclarations(ctx context.Context, records []sqlRecord, ids map[string]bool) (map[int]recordDeclaration, []scopedPosting, error) {
	postings, err := index.postings(ctx, sortedKeys(ids), "definition")
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[string][]scopedPosting)
	for _, posting := range postings {
		byID[posting.symbol] = append(byID[posting.symbol], posting)
	}
	declarations := map[int]recordDeclaration{}
	for i, record := range records {
		id, _ := record.values["symbol.id"].(string)
		if !ids[id] {
			continue
		}
		posting, err := index.selectDeclaration(byID[id], record.match)
		if err != nil {
			return nil, nil, err
		}
		handle, known := index.symbols.handle(id)
		if !known {
			return nil, nil, fmt.Errorf("no handle for metric symbol %s", id)
		}
		scope := index.scopes[posting.scope]
		active := scope.documents[posting.document]
		declarations[i] = recordDeclaration{scope: scope, active: active, key: declarationMetricKey{active.Document.Ordinal, handle}}
	}
	return declarations, postings, nil
}

func (index *compactIndex) sqlRecordMetrics(ctx context.Context, records []sqlRecord) error {
	ids := map[string]bool{}
	for _, record := range records {
		if id, ok := record.values["symbol.id"].(string); ok && id != "" && index.category(record.values["symbol.kind"].(string)) == symbolhandle.CategoryCallable {
			ids[id] = true
		}
	}
	declarations, postings, err := index.recordDeclarations(ctx, records, ids)
	if err != nil {
		return err
	}
	metrics, err := index.declarationMetrics(ctx, postings)
	if err != nil {
		return err
	}
	for i := range records {
		record := &records[i]
		record.values["symbol.metrics.param_count"], record.values["symbol.metrics.body_lines"] = nil, nil
		declaration, found := declarations[i]
		if !found {
			continue
		}
		metric, found := metrics[declaration.key]
		if !found || metric.ParamCount == nil {
			return missingDeclarationMetrics(declaration.scope, declaration.active, record.values["symbol.id"].(string))
		}
		record.values["symbol.metrics.param_count"] = *metric.ParamCount
		if metric.BodyLines != nil {
			record.values["symbol.metrics.body_lines"] = *metric.BodyLines
		}
	}
	return nil
}

// sqlRecordMeasures sets each requested producer metric on the records of kinds that declare it, from
// the selected declaration's stored measures, without reading documents. A declared metric with no
// stored value fails with republish guidance; records of kinds that do not declare it read NULL.
func (index *compactIndex) sqlRecordMeasures(ctx context.Context, records []sqlRecord, names []string, declared map[string]map[string]storage.MetricSpec) error {
	ids := map[string]bool{}
	for _, record := range records {
		for _, name := range names {
			record.values["symbol.metrics."+name] = nil
		}
		if id, ok := record.values["symbol.id"].(string); ok && id != "" && slices.ContainsFunc(names, func(name string) bool {
			_, declares := declared[record.values["symbol.kind"].(string)][name]
			return declares
		}) {
			ids[id] = true
		}
	}
	declarations, postings, err := index.recordDeclarations(ctx, records, ids)
	if err != nil {
		return err
	}
	measures, err := index.declarationMeasures(ctx, postings)
	if err != nil {
		return err
	}
	for i := range records {
		declaration, found := declarations[i]
		if !found {
			continue
		}
		kind := records[i].values["symbol.kind"].(string)
		for _, name := range names {
			if _, declares := declared[kind][name]; !declares {
				continue
			}
			value, found := measures[declaration.key][name]
			if !found {
				return missingDeclarationMeasure(declaration, records[i].values["symbol.id"].(string), kind, name)
			}
			records[i].values["symbol.metrics."+name] = int(value)
		}
	}
	return nil
}

func (index *indexContext) selectDeclaration(postings []scopedPosting, match ModuleMatch) (scopedPosting, error) {
	var selected []scopedPosting
	for _, posting := range postings {
		if index.scopes[posting.scope].snapshot.ID.String() == match.SnapshotID {
			selected = append(selected, posting)
		}
	}
	if len(selected) == 0 && len(postings) == 1 {
		selected = postings
	}
	if len(selected) == 1 {
		return selected[0], nil
	}
	if len(postings) == 0 {
		return scopedPosting{}, fmt.Errorf("snapshot %s source %q has no declaration for %s in the selected scope; index or republish the declaring root and include it in the query", match.SnapshotID, match.Path, match.SymbolID)
	}
	return scopedPosting{}, fmt.Errorf("symbol %s has several declarations in the selected snapshots; select a location or snapshot", match.SymbolID)
}
