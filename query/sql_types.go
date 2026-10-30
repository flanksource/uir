package query

import (
	"context"
	"fmt"

	"github.com/flanksource/uir/storage"
)

func (index *compactIndex) requireVariableTypes(ctx context.Context) error {
	postings := index.scopeDocumentPostings()
	if err := index.prefetch(ctx, postings); err != nil {
		return err
	}
	for _, posting := range postings {
		document, err := index.document(ctx, posting)
		if err != nil {
			return err
		}
		if document.coverage == storage.CoverageExcluded {
			continue
		}
		snapshot := index.scopes[posting.scope].snapshot.ID
		if document.coverage != storage.CoverageIndexed {
			return fmt.Errorf("symbol.type requires complete typed coverage: snapshot %s, package %q, source %q has %s coverage; fix extraction errors and reindex or republish", snapshot, document.content.PackagePath, document.path, document.coverage)
		}
		for _, symbol := range document.content.Symbols {
			if symbol.Kind == "var" && symbol.DeclaredType == nil {
				return fmt.Errorf("snapshot %s, source %q, variable %s has no resolved type; reindex or republish", snapshot, document.path, symbol.Identifier.GetName())
			}
		}
	}
	return nil
}

func (index *compactIndex) sqlRecordTypes(ctx context.Context, records []sqlRecord) error {
	ids := map[string]bool{}
	for _, record := range records {
		if record.values["symbol.kind"] == "var" {
			ids[record.values["symbol.id"].(string)] = true
		}
	}
	postings, err := index.postings(ctx, sortedKeys(ids), "definition")
	if err != nil {
		return err
	}
	byID := map[string][]scopedPosting{}
	for _, posting := range postings {
		byID[posting.symbol] = append(byID[posting.symbol], posting)
	}
	for i := range records {
		record := &records[i]
		record.values["symbol.type"] = nil
		id, _ := record.values["symbol.id"].(string)
		if !ids[id] {
			continue
		}
		posting, err := index.selectDeclaration(byID[id], record.match)
		if err != nil {
			return err
		}
		document, err := index.document(ctx, posting)
		if err != nil {
			return err
		}
		entry, err := index.entry(posting, document, id)
		if err != nil {
			return err
		}
		if entry.DeclaredType == nil {
			return fmt.Errorf("snapshot %s, source %q, variable %s has no resolved type; reindex or republish", record.match.SnapshotID, document.path, id)
		}
		record.values["symbol.type"] = *entry.DeclaredType
	}
	return nil
}
