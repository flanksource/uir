package query

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
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

// rootDocument is a document active in at least one scope of its root, with those scope positions.
type rootDocument struct {
	id     uuid.UUID
	path   string
	scopes []int
}

// rootDocuments is the union of the active documents of the scopes that select one root, keyed by
// document ordinal, which is what postings store; ordinals is the same set sorted.
type rootDocuments struct {
	ordinal   int32
	scopes    []int
	ordinals  []int64
	documents map[int64]rootDocument
}

// indexContext answers symbol queries from postings intersected with each scope's active set, and
// decodes only the documents a posting selected, once each. It maps symbol ids to handles and learns
// each snapshot's defined symbols at most once per query.
type indexContext struct {
	database *gorm.DB
	scopes   []indexScope
	roots    []rootDocuments
	decoded  map[uuid.UUID]decodedDocument
	handles  symbolHandles
	defined  map[uuid.UUID]map[int64]bool
}

func newIndexContext(ctx context.Context, database *gorm.DB, scopes []moduleScope) (*indexContext, error) {
	index := &indexContext{
		database: database, scopes: make([]indexScope, 0, len(scopes)), decoded: map[uuid.UUID]decodedDocument{},
		handles: newSymbolHandles(), defined: map[uuid.UUID]map[int64]bool{},
	}
	roots := map[int32]int{}
	for position, scope := range scopes {
		active, err := storage.ActiveDocuments(ctx, database, scope.snapshot.ID, storage.ActiveDocumentOptions{Content: true})
		if err != nil {
			return nil, err
		}
		root, found := roots[scope.root.Ordinal]
		if !found {
			root, roots[scope.root.Ordinal] = len(index.roots), len(index.roots)
			index.roots = append(index.roots, rootDocuments{ordinal: scope.root.Ordinal, documents: map[int64]rootDocument{}})
		}
		index.roots[root].scopes = append(index.roots[root].scopes, position)
		documents := make(map[uuid.UUID]storage.ActiveDocument, len(active))
		for _, document := range active {
			documents[document.Document.ID] = document
			entry := index.roots[root].documents[document.Document.Ordinal]
			if entry.scopes != nil && entry.id != document.Document.ID {
				return nil, fmt.Errorf("documents %s and %s of root %q share ordinal %d", entry.id, document.Document.ID, scope.root.RootKey, document.Document.Ordinal)
			}
			entry.id, entry.path, entry.scopes = document.Document.ID, document.Document.PathKey, append(entry.scopes, position)
			index.roots[root].documents[document.Document.Ordinal] = entry
		}
		index.scopes = append(index.scopes, indexScope{moduleScope: scope, documents: documents})
	}
	for position := range index.roots {
		index.roots[position].ordinals = slices.Sorted(maps.Keys(index.roots[position].documents))
	}
	return index, nil
}

// postings returns the postings of the symbols with one of the roles whose documents are active in a
// scope, one entry per scope that activates the document, ordered by scope, path, and symbol. Postings
// are stored by symbol handle, root ordinal, and document ordinal; ids map to handles once per query.
func (index *indexContext) postings(ctx context.Context, symbolIDs []string, roles ...string) ([]scopedPosting, error) {
	if len(symbolIDs) == 0 {
		return nil, nil
	}
	handles, err := index.handlesOf(ctx, symbolIDs)
	if err != nil {
		return nil, err
	}
	codes := make([]storage.PostingRole, len(roles))
	for i, role := range roles {
		if codes[i], err = storage.ParsePostingRole(role); err != nil {
			return nil, err
		}
	}
	definitionsOnly := slices.Equal(codes, []storage.PostingRole{storage.RoleDefinition})
	var found []scopedPosting
	for _, root := range index.roots {
		rootHandles := handles
		if definitionsOnly {
			rootHandles = slices.DeleteFunc(slices.Clone(handles), func(handle int64) bool { return index.knownUndefined(root, handle) })
		}
		rows, err := rootPostings(ctx, index.database, rootHandles, codes, root)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			document := root.documents[row.DocumentOrdinal]
			for _, position := range document.scopes {
				found = append(found, scopedPosting{scope: position, path: document.path, document: document.id, symbol: index.handles.byHandle[row.SymbolHandle]})
			}
		}
	}
	sort.Slice(found, func(i, j int) bool {
		left, right := found[i], found[j]
		return cmp.Or(cmp.Compare(left.scope, right.scope), strings.Compare(left.path, right.path), strings.Compare(left.symbol, right.symbol)) < 0
	})
	return found, nil
}

// rootPostings intersects one root's postings of (symbols, roles) with its active documents over the
// (symbol_handle, role, root_ordinal, document_ordinal) index, reading whichever side is smaller: the
// root's posting range when it holds no more rows than the active set, and otherwise the active
// document ordinals in IN batches probing the posting key. The range read is bounded at one row past
// the active set, which is both the count that decides and the rows it returns.
func rootPostings(ctx context.Context, database *gorm.DB, handles []int64, roles []storage.PostingRole, root rootDocuments) ([]storage.SymbolPosting, error) {
	if len(root.ordinals) == 0 {
		return nil, nil
	}
	var found []storage.SymbolPosting
	for start := 0; start < len(handles); start += lookupBatch {
		symbols := handles[start:min(start+lookupBatch, len(handles))]
		selected := func() *gorm.DB {
			return database.WithContext(ctx).Model(&storage.SymbolPosting{}).Select("document_ordinal", "symbol_handle").
				Where("symbol_handle IN ? AND role IN ? AND root_ordinal = ?", symbols, roles, root.ordinal)
		}
		var rows []storage.SymbolPosting
		if err := selected().Limit(len(root.ordinals) + 1).Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load %v postings of %d symbols in root %d: %w", roles, len(symbols), root.ordinal, err)
		}
		if len(rows) <= len(root.ordinals) {
			for _, row := range rows {
				if _, ok := root.documents[row.DocumentOrdinal]; ok {
					found = append(found, row)
				}
			}
			continue
		}
		for offset := 0; offset < len(root.ordinals); offset += lookupBatch {
			var probed []storage.SymbolPosting
			batch := root.ordinals[offset:min(offset+lookupBatch, len(root.ordinals))]
			if err := selected().Where("document_ordinal IN ?", batch).Find(&probed).Error; err != nil {
				return nil, fmt.Errorf("probe %v postings of %d symbols in root %d: %w", roles, len(symbols), root.ordinal, err)
			}
			found = append(found, probed...)
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
		Path: document.path, PackagePath: document.content.PackagePath, Coverage: string(document.coverage),
	}
}

// declarationMatch is a row for a declared symbol, positioned at its name.
func (index *indexContext) declarationMatch(posting scopedPosting, document decodedDocument, kind, role string, entry storage.DocumentSymbol) ModuleMatch {
	match := index.match(posting, document, kind)
	match.Line, match.Column, match.EndLine, match.EndColumn = rangePosition(entry.Name)
	match.Role, match.NodeKind, match.Identifier, match.SymbolID = role, entry.Kind, entry.Identifier, *entry.ID
	match.Declaration, match.DefinitionLine = entry.Shape, &entry.Extent[0]
	return match
}

// occurrenceMatch is a row for one occurrence, named by the declaration enclosing it; an occurrence at
// file scope is named by its package.
func (index *indexContext) occurrenceMatch(posting scopedPosting, document decodedDocument, kind string, occurrence storage.DocumentOccurrence) ModuleMatch {
	match := index.match(posting, document, kind)
	match.Line, match.Column, match.EndLine, match.EndColumn = rangePosition(occurrence.Range)
	match.Role, match.span, match.text = occurrence.Role, occurrence.Bytes, occurrence.Text
	match.Identifier = uir.Identifier{Package: document.content.PackagePath, NodeType: uir.NodeTypePackage}
	if occurrence.Symbol != nil {
		match.SymbolID = *occurrence.Symbol
	}
	if occurrence.Enclosing != nil {
		enclosing := document.entries[*occurrence.Enclosing]
		match.EnclosingID, match.EnclosingKey, match.Identifier = *occurrence.Enclosing, enclosing.Key, enclosing.Identifier
		match.Declaration, match.DefinitionLine = enclosing.Shape, &enclosing.Extent[0]
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
