package query

import (
	"cmp"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// lookupBatch bounds every IN list the index queries send.
const lookupBatch = 256

// indexScope is one selected head or snapshot with its active documents keyed by document id.
type indexScope struct {
	moduleScope
	documents map[uuid.UUID]storage.ActiveDocument
}

// decodedDocument is a validated document with its declared symbols keyed by canonical id.
type decodedDocument struct {
	path     string
	coverage storage.Coverage
	content  storage.DocumentContent
	entries  map[string]storage.DocumentSymbol
}

// scopedPosting is one posting whose document is active in one scope.
type scopedPosting struct {
	scope    int
	path     string
	document uuid.UUID
	symbol   string
}

// indexContext answers symbol queries from postings intersected with each scope's active set, and
// decodes only the documents a posting selected, once each.
type indexContext struct {
	database *gorm.DB
	scopes   []indexScope
	decoded  map[uuid.UUID]decodedDocument
}

func newIndexContext(ctx context.Context, database *gorm.DB, scopes []moduleScope) (*indexContext, error) {
	index := &indexContext{database: database, scopes: make([]indexScope, 0, len(scopes)), decoded: map[uuid.UUID]decodedDocument{}}
	for _, scope := range scopes {
		active, err := storage.ActiveDocuments(ctx, database, scope.snapshot.ID)
		if err != nil {
			return nil, err
		}
		documents := make(map[uuid.UUID]storage.ActiveDocument, len(active))
		for _, document := range active {
			documents[document.Document.ID] = document
		}
		index.scopes = append(index.scopes, indexScope{moduleScope: scope, documents: documents})
	}
	return index, nil
}

// postings returns the postings of the symbols with one of the roles whose documents are active in a
// scope, one entry per scope that activates the document, ordered by scope, path, and symbol.
func (index *indexContext) postings(ctx context.Context, symbolIDs []string, roles ...string) ([]scopedPosting, error) {
	if len(symbolIDs) == 0 {
		return nil, nil
	}
	active := map[uuid.UUID]map[uuid.UUID]bool{}
	var roots []uuid.UUID
	for _, scope := range index.scopes {
		if active[scope.root.ID] == nil {
			active[scope.root.ID] = map[uuid.UUID]bool{}
			roots = append(roots, scope.root.ID)
		}
		for id := range scope.documents {
			active[scope.root.ID][id] = true
		}
	}
	var found []scopedPosting
	for _, rootID := range roots {
		rows, err := rootPostings(ctx, index.database, symbolIDs, roles, rootID, active[rootID])
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			for position, scope := range index.scopes {
				if document, ok := scope.documents[row.DocumentID]; ok && scope.root.ID == rootID {
					found = append(found, scopedPosting{scope: position, path: document.Document.PathKey, document: row.DocumentID, symbol: row.SymbolID})
				}
			}
		}
	}
	sort.Slice(found, func(i, j int) bool {
		left, right := found[i], found[j]
		return cmp.Or(cmp.Compare(left.scope, right.scope), strings.Compare(left.path, right.path), strings.Compare(left.symbol, right.symbol)) < 0
	})
	return found, nil
}

// rootPostings intersects one root's postings of (symbols, roles) with its active documents, reading
// whichever side is smaller: the posting range when it holds no more rows than the active set, and
// otherwise the active documents in IN batches probing the posting index.
func rootPostings(ctx context.Context, database *gorm.DB, symbolIDs, roles []string, rootID uuid.UUID, active map[uuid.UUID]bool) ([]storage.SymbolPosting, error) {
	documents := make([]uuid.UUID, 0, len(active))
	for id := range active {
		documents = append(documents, id)
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].String() < documents[j].String() })
	var found []storage.SymbolPosting
	for start := 0; start < len(symbolIDs); start += lookupBatch {
		symbols := symbolIDs[start:min(start+lookupBatch, len(symbolIDs))]
		selected := func() *gorm.DB {
			return database.WithContext(ctx).Model(&storage.SymbolPosting{}).Where("symbol_id IN ? AND role IN ? AND root_id = ?", symbols, roles, rootID)
		}
		var count int64
		if err := selected().Count(&count).Error; err != nil {
			return nil, fmt.Errorf("count %v postings of %d symbols in root %s: %w", roles, len(symbols), rootID, err)
		}
		if count <= int64(len(documents)) {
			var rows []storage.SymbolPosting
			if err := selected().Select("document_id", "symbol_id").Find(&rows).Error; err != nil {
				return nil, fmt.Errorf("load %v postings of %d symbols in root %s: %w", roles, len(symbols), rootID, err)
			}
			for _, row := range rows {
				if active[row.DocumentID] {
					found = append(found, row)
				}
			}
			continue
		}
		for offset := 0; offset < len(documents); offset += lookupBatch {
			var rows []storage.SymbolPosting
			batch := documents[offset:min(offset+lookupBatch, len(documents))]
			if err := selected().Where("document_id IN ?", batch).Select("document_id", "symbol_id").Find(&rows).Error; err != nil {
				return nil, fmt.Errorf("probe %v postings of %d symbols in root %s: %w", roles, len(symbols), rootID, err)
			}
			found = append(found, rows...)
		}
	}
	return found, nil
}

// document decodes one active document of a scope, validating it against its source revision.
func (index *indexContext) document(posting scopedPosting) (decodedDocument, error) {
	if decoded, found := index.decoded[posting.document]; found {
		return decoded, nil
	}
	active, found := index.scopes[posting.scope].documents[posting.document]
	if !found {
		return decodedDocument{}, fmt.Errorf("document %s is not active in snapshot %s", posting.document, index.scopes[posting.scope].snapshot.ID)
	}
	content, err := storage.DecodeDocument(active.Document, active.Source)
	if err != nil {
		return decodedDocument{}, err
	}
	entries := make(map[string]storage.DocumentSymbol, len(content.Symbols))
	for _, symbol := range content.Symbols {
		if symbol.ID != nil {
			entries[*symbol.ID] = symbol
		}
	}
	decoded := decodedDocument{path: active.Document.PathKey, coverage: active.Document.Coverage, content: content, entries: entries}
	index.decoded[posting.document] = decoded
	return decoded, nil
}

// entry is the declared symbol a definition or implements posting promises the document holds.
func (index *indexContext) entry(posting scopedPosting, document decodedDocument, id string) (storage.DocumentSymbol, error) {
	entry, found := document.entries[id]
	if !found {
		return storage.DocumentSymbol{}, fmt.Errorf("document %s for %q has a posting for symbol %s but does not declare it", posting.document, document.path, id)
	}
	return entry, nil
}

// match is a row located in one document of one scope, without a position.
func (index *indexContext) match(posting scopedPosting, document decodedDocument, kind string) ModuleMatch {
	scope := index.scopes[posting.scope]
	return ModuleMatch{
		Kind: kind, RootKey: scope.root.RootKey, Location: scope.location.CanonicalPath, SnapshotID: scope.snapshot.ID.String(),
		Path: document.path, Coverage: string(document.coverage),
	}
}

// declarationMatch is a row for a declared symbol, positioned at its name.
func (index *indexContext) declarationMatch(posting scopedPosting, document decodedDocument, kind, role string, entry storage.DocumentSymbol) ModuleMatch {
	match := index.match(posting, document, kind)
	match.Line, match.Column, match.EndLine, match.EndColumn = rangePosition(entry.Name)
	match.Role, match.Identifier, match.SymbolID = role, entry.Identifier, *entry.ID
	return match
}

// occurrenceMatch is a row for one occurrence, named by the declaration enclosing it; an occurrence at
// file scope is named by its package.
func (index *indexContext) occurrenceMatch(posting scopedPosting, document decodedDocument, kind string, occurrence storage.DocumentOccurrence) ModuleMatch {
	match := index.match(posting, document, kind)
	match.Line, match.Column, match.EndLine, match.EndColumn = rangePosition(occurrence.Range)
	match.Role = occurrence.Role
	match.Identifier = uir.Identifier{Package: document.content.PackagePath, NodeType: uir.NodeTypePackage}
	if occurrence.Symbol != nil {
		match.SymbolID = *occurrence.Symbol
	}
	if occurrence.Enclosing != nil {
		enclosing := document.entries[*occurrence.Enclosing]
		match.EnclosingID, match.EnclosingKey, match.Identifier = *occurrence.Enclosing, enclosing.Key, enclosing.Identifier
	}
	return match
}

func rangePosition(value storage.Range) (line, column, endLine, endColumn *int) {
	return &value[0], &value[1], &value[2], &value[3]
}

// sortMatches orders occurrence and declaration rows by root, checkout, path, and position.
func sortMatches(matches []ModuleMatch) {
	position := func(value *int) int {
		if value == nil {
			return 0
		}
		return *value
	}
	sort.SliceStable(matches, func(i, j int) bool {
		left, right := matches[i], matches[j]
		return cmp.Or(
			strings.Compare(left.RootKey, right.RootKey), strings.Compare(left.Location, right.Location), strings.Compare(left.Path, right.Path),
			cmp.Compare(position(left.Line), position(right.Line)), cmp.Compare(position(left.Column), position(right.Column)),
			strings.Compare(left.Kind, right.Kind), strings.Compare(left.SymbolID, right.SymbolID),
		) < 0
	})
}
