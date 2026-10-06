package symboldiff

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// decodeDocument is storage.DecodeDocument; tests count the documents a diff reads through it.
var decodeDocument = storage.DecodeDocument

// fileSide is one side of a changed path: its active document header and, once the diff needs it,
// the decoded content. owners marks the declarations whose extent is outermost: each owns one line
// segment of the file.
type fileSide struct {
	path    string
	active  storage.ActiveDocument
	content storage.DocumentContent
	decoded bool
	owners  map[int]bool
}

func (side *fileSide) coverage() storage.Coverage { return side.active.Document.Coverage }

type changedFile struct {
	path          string
	before, after *fileSide
}

func (file changedFile) sides() [2]*fileSide { return [2]*fileSide{file.before, file.after} }

// changedFiles lists the paths whose document differs between the two sides, sorted. Paths with
// equal document ids on both sides are skipped; no document is read.
func changedFiles(before, after map[string]storage.ActiveDocument) []changedFile {
	paths := map[string]bool{}
	for path := range before {
		paths[path] = true
	}
	for path := range after {
		paths[path] = true
	}
	var files []changedFile
	for path := range paths {
		old, oldFound := before[path]
		current, currentFound := after[path]
		if oldFound && currentFound && old.Document.ID == current.Document.ID {
			continue
		}
		file := changedFile{path: path}
		if oldFound {
			file.before = &fileSide{path: path, active: old, owners: map[int]bool{}}
		}
		if currentFound {
			file.after = &fileSide{path: path, active: current, owners: map[int]bool{}}
		}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files
}

// readDocuments loads and decodes the given sides' documents, then clears the segment owners of
// every file that either side excludes, whose lines all belong to file scope.
func readDocuments(ctx context.Context, database *gorm.DB, files []changedFile, read map[*fileSide]bool) error {
	byID := make(map[uuid.UUID]*fileSide, len(read))
	ids := make([]uuid.UUID, 0, len(read))
	for side := range read {
		byID[side.active.Document.ID] = side
		ids = append(ids, side.active.Document.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
	contents, err := storage.DocumentContents(ctx, database, ids)
	if err != nil {
		return err
	}
	kinds, err := storage.LoadSymbolKinds(ctx, database)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := byID[id].decode(contents[id], kinds); err != nil {
			return err
		}
	}
	for _, file := range files {
		if file.excluded() {
			file.before.clearOwners()
			file.after.clearOwners()
		}
	}
	return nil
}

// decode validates the side's document against the database's kind registry, which may hold the custom
// kinds of a non-Go producer.
func (side *fileSide) decode(content storage.JSON, kinds symbolhandle.Kinds) error {
	document := side.active.Document
	document.Content = content
	decoded, err := decodeDocument(document, side.active.Source, storage.WithKinds(kinds))
	if err != nil {
		return err
	}
	side.content, side.decoded = decoded, true
	openEnd := -1
	for index, symbol := range decoded.Symbols {
		if symbol.ExtentBytes[0] >= openEnd {
			side.owners[index] = true
			openEnd = symbol.ExtentBytes[1]
		}
	}
	return nil
}

func (side *fileSide) clearOwners() {
	if side != nil {
		side.owners = map[int]bool{}
	}
}

// excluded reports whether either side of the file was not extracted; its lines then all belong to
// file scope.
func (file changedFile) excluded() bool {
	return (file.before != nil && file.before.coverage() == storage.CoverageExcluded) ||
		(file.after != nil && file.after.coverage() == storage.CoverageExcluded)
}

// declarationHandles maps the id of every decoded declaration to its symbol handle.
func declarationHandles(ctx context.Context, database *gorm.DB, files []changedFile) (map[string]int64, error) {
	unique := map[string]bool{}
	for _, file := range files {
		for _, side := range file.sides() {
			if side == nil || !side.decoded {
				continue
			}
			for _, symbol := range side.content.Symbols {
				if symbol.ID != nil {
					unique[*symbol.ID] = true
				}
			}
		}
	}
	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return storage.SymbolHandles(ctx, database, ids)
}

// lightEntry describes a declaration whose document was not read from its symbols row: its id, kind,
// visibility, and the identifier the typed extractor gives it, but no shape, hashes, or extent.
func lightEntry(side *fileSide, symbol storage.OwnedSymbol) (*entry, error) {
	identifier, err := symbol.Identifier()
	if err != nil {
		return nil, fmt.Errorf("describe symbol declared in %q: %w", side.path, err)
	}
	id := symbol.ID
	return &entry{file: side, index: -1, symbol: storage.DocumentSymbol{ID: &id, Kind: symbol.Kind, Visibility: symbol.Visibility, Identifier: identifier}}, nil
}
