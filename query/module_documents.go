package query

import (
	"context"
	"sort"

	"github.com/flanksource/uir/storage"
)

// scopeDocument is one active path of a snapshot with its validated document content.
type scopeDocument struct {
	path    string
	source  storage.SourceRevision
	content storage.DocumentContent
}

// scopeDocuments reads a snapshot's active documents through the membership derivation, in path order.
func (pipeline *Pipeline) scopeDocuments(ctx context.Context, scope moduleScope) ([]scopeDocument, error) {
	active, err := storage.ActiveDocuments(ctx, pipeline.database, scope.snapshot.ID)
	if err != nil {
		return nil, err
	}
	documents := make([]scopeDocument, 0, len(active))
	for path, document := range active {
		content, err := storage.DecodeDocument(document.Document, document.Source)
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
