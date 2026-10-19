package query

import (
	"context"
	"fmt"
	"sort"

	"github.com/flanksource/uir/storage"
)

// scopeDocument is one active path of a snapshot with its validated document content.
type scopeDocument struct {
	path    string
	source  storage.SourceRevision
	content storage.DocumentContent
}

type SnapshotDocument struct {
	Path       string                  `json:"path"`
	SourceHash string                  `json:"source_hash"`
	Content    storage.DocumentContent `json:"content"`
}

func (pipeline *Pipeline) SnapshotDocuments(ctx context.Context, snapshotID string) ([]SnapshotDocument, error) {
	if pipeline == nil || pipeline.database == nil || snapshotID == "" {
		return nil, fmt.Errorf("snapshot documents require a database and snapshot ID")
	}
	selection, err := pipeline.moduleScopes(ctx, ModuleScopeOptions{SnapshotID: snapshotID}, false)
	if err != nil {
		return nil, err
	}
	if len(selection.scopes) != 1 {
		return nil, fmt.Errorf("snapshot documents require one snapshot, matched %d", len(selection.scopes))
	}
	documents, err := pipeline.scopeDocuments(ctx, selection.scopes[0])
	if err != nil {
		return nil, err
	}
	result := make([]SnapshotDocument, 0, len(documents))
	for _, document := range documents {
		result = append(result, SnapshotDocument{Path: document.path, SourceHash: document.source.ContentHash, Content: document.content})
	}
	return result, nil
}

// scopeDocuments reads a snapshot's active documents through the membership derivation, in path order.
func (pipeline *Pipeline) scopeDocuments(ctx context.Context, scope moduleScope) ([]scopeDocument, error) {
	active, err := storage.ActiveDocuments(ctx, pipeline.database, scope.snapshot.ID, storage.ActiveDocumentOptions{Content: true})
	if err != nil {
		return nil, err
	}
	kinds, err := storage.LoadSymbolKinds(ctx, pipeline.database)
	if err != nil {
		return nil, err
	}
	documents := make([]scopeDocument, 0, len(active))
	for path, document := range active {
		content, err := storage.DecodeDocument(document.Document, document.Source, storage.WithKinds(kinds))
		if err != nil {
			return nil, err
		}
		documents = append(documents, scopeDocument{path: path, source: document.Source, content: content})
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].path < documents[j].path })
	return documents, nil
}

// symbolPosition is where navigation lands for a symbol: its name's start, and its extent's last line.
func symbolPosition(symbol storage.DocumentSymbol) (line, endLine, column *int) {
	return &symbol.Name[0], &symbol.Extent[2], &symbol.Name[1]
}
