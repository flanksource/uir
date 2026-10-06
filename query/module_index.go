package query

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// lookupBatch bounds every IN list the index queries send.
const lookupBatch = 256

// indexScope is one selected head or snapshot with its active documents keyed by document id, which
// carry no content: a document's content is read only when a posting selects it.
type indexScope struct {
	moduleScope
	documents map[uuid.UUID]storage.ActiveDocument
	facts     *snapshotFacts
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
	key       string
	scopes    []int
	ordinals  []int64
	documents map[int64]rootDocument
}

// indexContext answers symbol queries from postings intersected with each scope's active set, and
// decodes only the documents a posting selected, once each. Its scopes' active documents, defined
// symbols, decoded documents, symbol handles, and symbol rows come from the pipeline's cache, which
// keeps them across queries; decoded holds what this query decoded, whether or not the cache kept it.
// kinds is the database's kind registry, which decides each symbol's category.
type indexContext struct {
	database *gorm.DB
	cache    *indexCache
	scopes   []indexScope
	roots    []rootDocuments
	rootKeys map[string]int
	decoded  map[uuid.UUID]decodedDocument
	symbols  *symbolFacts
	kinds    symbolhandle.Kinds
}

func newIndexContext(ctx context.Context, pipeline *Pipeline, scopes []moduleScope) (*indexContext, error) {
	generation, err := pipeline.generation(ctx)
	if err != nil {
		return nil, err
	}
	index := &indexContext{
		database: pipeline.database, cache: pipeline.cache, scopes: make([]indexScope, 0, len(scopes)), rootKeys: map[string]int{},
		decoded: map[uuid.UUID]decodedDocument{}, symbols: pipeline.cache.symbolFacts(), kinds: generation.kinds,
	}
	for position, scope := range scopes {
		facts, err := pipeline.cache.snapshot(ctx, pipeline.database, scope.snapshot.ID)
		if err != nil {
			return nil, err
		}
		if err := index.addScope(position, scope, facts); err != nil {
			return nil, err
		}
	}
	for position := range index.roots {
		index.roots[position].ordinals = slices.Sorted(maps.Keys(index.roots[position].documents))
	}
	return index, nil
}

// addScope adds one scope and its active documents to the documents of its root.
func (index *indexContext) addScope(position int, scope moduleScope, facts *snapshotFacts) error {
	root, found := index.rootKeys[scope.root.RootKey]
	if !found {
		root, index.rootKeys[scope.root.RootKey] = len(index.roots), len(index.roots)
		index.roots = append(index.roots, rootDocuments{ordinal: scope.root.Ordinal, key: scope.root.RootKey, documents: map[int64]rootDocument{}})
	}
	index.roots[root].scopes = append(index.roots[root].scopes, position)
	for _, document := range facts.documents {
		entry := index.roots[root].documents[document.Document.Ordinal]
		if entry.scopes != nil && entry.id != document.Document.ID {
			return fmt.Errorf("documents %s and %s of root %q share ordinal %d", entry.id, document.Document.ID, scope.root.RootKey, document.Document.Ordinal)
		}
		entry.id, entry.path, entry.scopes = document.Document.ID, document.Document.PathKey, append(entry.scopes, position)
		index.roots[root].documents[document.Document.Ordinal] = entry
	}
	index.scopes = append(index.scopes, indexScope{moduleScope: scope, documents: facts.documents, facts: facts})
	return nil
}

// document decodes one active document of a scope, validating it against its source revision. A loop
// over many postings prefetches them first, so their content is read in batches.
func (index *indexContext) document(ctx context.Context, posting scopedPosting) (decodedDocument, error) {
	if decoded, found := index.decoded[posting.document]; found {
		return decoded, nil
	}
	if err := index.prefetch(ctx, []scopedPosting{posting}); err != nil {
		return decodedDocument{}, err
	}
	return index.decoded[posting.document], nil
}

// prefetch decodes the documents of the postings that this query has not decoded yet: from the
// pipeline's cache when it kept them, and otherwise by reading their content in IN batches.
func (index *indexContext) prefetch(ctx context.Context, postings []scopedPosting) error {
	pending := map[uuid.UUID]storage.ActiveDocument{}
	var missing []uuid.UUID
	for _, posting := range postings {
		if _, found := index.decoded[posting.document]; found {
			continue
		}
		if _, found := pending[posting.document]; found {
			continue
		}
		if cached, found := index.cache.documents.get(posting.document); found {
			index.decoded[posting.document] = cached
			continue
		}
		active, found := index.scopes[posting.scope].documents[posting.document]
		if !found {
			return fmt.Errorf("document %s is not active in snapshot %s", posting.document, index.scopes[posting.scope].snapshot.ID)
		}
		pending[posting.document] = active
		missing = append(missing, posting.document)
	}
	for start := 0; start < len(missing); start += lookupBatch {
		batch := missing[start:min(start+lookupBatch, len(missing))]
		contents, err := storage.DocumentContents(ctx, index.database, batch)
		if err != nil {
			return err
		}
		for _, id := range batch {
			active := pending[id]
			active.Document.Content = contents[id]
			decoded, err := decodeActive(active, index.kinds)
			if err != nil {
				return err
			}
			index.decoded[id] = decoded
			index.cache.documents.put(id, decoded)
		}
	}
	return nil
}

// decodeActive decodes an active document whose content was read.
func decodeActive(active storage.ActiveDocument, kinds symbolhandle.Kinds) (decodedDocument, error) {
	content, err := storage.DecodeDocument(active.Document, active.Source, storage.WithKinds(kinds))
	if err != nil {
		return decodedDocument{}, err
	}
	entries := make(map[string]storage.DocumentSymbol, len(content.Symbols))
	for _, symbol := range content.Symbols {
		if symbol.ID != nil {
			entries[*symbol.ID] = symbol
		}
	}
	return decodedDocument{path: active.Document.PathKey, coverage: active.Document.Coverage, content: content, entries: entries}, nil
}

// scopeDocumentPostings stands for every active document of every scope, so a reader of all of them
// can prefetch them together.
func (index *indexContext) scopeDocumentPostings() []scopedPosting {
	var postings []scopedPosting
	for position, scope := range index.scopes {
		for id, document := range scope.documents {
			postings = append(postings, scopedPosting{scope: position, path: document.Document.PathKey, document: id})
		}
	}
	return postings
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
	if len(entry.Payload) > 0 {
		// The entry belongs to the pipeline's cached document; the caller gets its own copy.
		match.Payload = json.RawMessage(bytes.Clone(entry.Payload))
	}
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
