package query

import (
	"context"
	"fmt"

	"github.com/flanksource/uir/storage/symbolhandle"
)

func (index *compactIndex) sqlRecordMetrics(ctx context.Context, records []sqlRecord) error {
	ids := map[string]bool{}
	for _, record := range records {
		if id, ok := record.values["symbol.id"].(string); ok && id != "" && index.category(record.values["symbol.kind"].(string)) == symbolhandle.CategoryCallable {
			ids[id] = true
		}
	}
	postings, err := index.postings(ctx, sortedKeys(ids), "definition")
	if err != nil {
		return err
	}
	metrics, err := index.declarationMetrics(ctx, postings)
	if err != nil {
		return err
	}
	byID := make(map[string][]scopedPosting)
	for _, posting := range postings {
		byID[posting.symbol] = append(byID[posting.symbol], posting)
	}
	for i := range records {
		record := &records[i]
		record.values["symbol.metrics.param_count"], record.values["symbol.metrics.body_lines"] = nil, nil
		id, _ := record.values["symbol.id"].(string)
		if !ids[id] {
			continue
		}
		posting, err := index.selectDeclaration(byID[id], record.match)
		if err != nil {
			return err
		}
		scope := index.scopes[posting.scope]
		active := scope.documents[posting.document]
		handle, known := index.symbols.handle(id)
		if !known {
			return fmt.Errorf("no handle for metric symbol %s", id)
		}
		metric, found := metrics[declarationMetricKey{active.Document.Ordinal, handle}]
		if !found || metric.ParamCount == nil {
			return missingDeclarationMetrics(scope, active, id)
		}
		record.values["symbol.metrics.param_count"] = *metric.ParamCount
		if metric.BodyLines != nil {
			record.values["symbol.metrics.body_lines"] = *metric.BodyLines
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
