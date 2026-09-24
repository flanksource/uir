package symboldiff

import (
	"fmt"
	"sort"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
)

// fileSide is one side's decoded document for a changed path. owners marks the symbols whose extent
// is outermost: each owns one line segment of the file.
type fileSide struct {
	path    string
	active  storage.ActiveDocument
	content storage.DocumentContent
	owners  map[int]bool
}

func (side *fileSide) coverage() storage.Coverage { return side.active.Document.Coverage }

type changedFile struct {
	path          string
	before, after *fileSide
}

// entry is one symbol declared by one side of a changed file.
type entry struct {
	file   *fileSide
	symbol storage.DocumentSymbol
	index  int
}

func (entry *entry) owner() bool { return entry.file.owners[entry.index] }

// pairing is a symbol matched across the two sides; class is empty when it is unchanged.
type pairing struct {
	before, after *entry
	class         Class
}

// changedFiles lists the paths whose document differs between the two sides, sorted, with each side
// decoded. Paths with equal document ids on both sides are skipped without reading them.
func changedFiles(before, after map[string]storage.ActiveDocument) ([]changedFile, error) {
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
		var err error
		if file.before, err = decodeSide(path, old, oldFound); err != nil {
			return nil, err
		}
		if file.after, err = decodeSide(path, current, currentFound); err != nil {
			return nil, err
		}
		if file.excluded() {
			file.before.clearOwners()
			file.after.clearOwners()
		}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, nil
}

func decodeSide(path string, active storage.ActiveDocument, found bool) (*fileSide, error) {
	if !found {
		return nil, nil
	}
	content, err := storage.DecodeDocument(active.Document, active.Source)
	if err != nil {
		return nil, err
	}
	side := &fileSide{path: path, active: active, content: content, owners: map[int]bool{}}
	openEnd := -1
	for index, symbol := range content.Symbols {
		if symbol.ExtentBytes[0] >= openEnd {
			side.owners[index] = true
			openEnd = symbol.ExtentBytes[1]
		}
	}
	return side, nil
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

func entriesOf(files []changedFile, side func(changedFile) *fileSide) []*entry {
	var entries []*entry
	for _, file := range files {
		document := side(file)
		if document == nil {
			continue
		}
		for index, symbol := range document.content.Symbols {
			entries = append(entries, &entry{file: document, symbol: symbol, index: index})
		}
	}
	return entries
}

// matchSymbols pairs symbol entries across the whole diff: by id first, so a moved symbol is matched
// by identity, then by key where either side has no id (a syntax document or an unproven partial
// declaration). Unmatched entries are removed or added.
func matchSymbols(files []changedFile) []*pairing {
	before := entriesOf(files, func(file changedFile) *fileSide { return file.before })
	after := entriesOf(files, func(file changedFile) *fileSide { return file.after })
	matched := map[*entry]bool{}
	pairings := matchByID(before, after, matched)
	pairings = append(pairings, matchByKey(before, after, matched)...)
	for _, old := range before {
		if !matched[old] {
			pairings = append(pairings, &pairing{before: old})
		}
	}
	for _, current := range after {
		if !matched[current] {
			pairings = append(pairings, &pairing{after: current})
		}
	}
	for _, pair := range pairings {
		pair.class = classify(pair)
	}
	return pairings
}

func matchByID(before, after []*entry, matched map[*entry]bool) []*pairing {
	var pairings []*pairing
	byID := map[string][]*entry{}
	for _, candidate := range after {
		if candidate.symbol.ID != nil {
			byID[*candidate.symbol.ID] = append(byID[*candidate.symbol.ID], candidate)
		}
	}
	for _, old := range before {
		if old.symbol.ID == nil || len(byID[*old.symbol.ID]) == 0 {
			continue
		}
		current := byID[*old.symbol.ID][0]
		byID[*old.symbol.ID] = byID[*old.symbol.ID][1:]
		matched[old], matched[current] = true, true
		pairings = append(pairings, &pairing{before: old, after: current})
	}
	return pairings
}

func matchByKey(before, after []*entry, matched map[*entry]bool) []*pairing {
	var pairings []*pairing
	byKey := map[string][]*entry{}
	for _, candidate := range after {
		if !matched[candidate] {
			byKey[candidate.symbol.Key] = append(byKey[candidate.symbol.Key], candidate)
		}
	}
	for _, old := range before {
		if matched[old] {
			continue
		}
		candidates := byKey[old.symbol.Key]
		for index, current := range candidates {
			if old.symbol.ID == nil || current.symbol.ID == nil {
				byKey[old.symbol.Key] = append(candidates[:index:index], candidates[index+1:]...)
				matched[old], matched[current] = true, true
				pairings = append(pairings, &pairing{before: old, after: current})
				break
			}
		}
	}
	return pairings
}

func classify(pair *pairing) Class {
	switch {
	case pair.before == nil:
		return ClassAdded
	case pair.after == nil:
		return ClassRemoved
	case pair.before.symbol.ID != nil && pair.after.symbol.ID != nil && pair.before.symbol.ShapeHash != pair.after.symbol.ShapeHash:
		return ClassSignature
	case pair.before.symbol.BodyHash != pair.after.symbol.BodyHash:
		return ClassBody
	case pair.before.file.path != pair.after.file.path:
		return ClassMoved
	}
	return ""
}

// ownerAndName reads a declaration's owner and name from its structured identifier.
func ownerAndName(identifier uir.Identifier) (string, string) {
	switch {
	case identifier.Field != "":
		return identifier.Type, identifier.Field
	case identifier.Method != "":
		return identifier.Type, identifier.Method
	}
	return "", identifier.Type
}

// reported is the entry a row describes: the new side unless the symbol was removed.
func (pair *pairing) reported() *entry {
	if pair.after != nil {
		return pair.after
	}
	return pair.before
}

func (pair *pairing) visibleAs(visibility Visibility) bool {
	if visibility == VisibilityAll {
		return true
	}
	for _, side := range []*entry{pair.before, pair.after} {
		if side != nil && side.symbol.Visibility == string(visibility) {
			return true
		}
	}
	return false
}

// newRow describes a pairing. Shapes are included only where they differ.
func newRow(pair *pairing, class Class, coverage []string) (Row, error) {
	reported := pair.reported()
	owner, name := ownerAndName(reported.symbol.Identifier)
	if name == "" {
		return Row{}, fmt.Errorf("symbol %q in %q has no name in its identifier", reported.symbol.Key, reported.file.path)
	}
	row := Row{Class: class, Kind: reported.symbol.Kind, Owner: owner, Name: name, Visibility: reported.symbol.Visibility, Coverage: coverage}
	if pair.before != nil {
		row.PathBefore = pair.before.file.path
		if pair.after == nil || pair.before.symbol.Shape != pair.after.symbol.Shape {
			row.ShapeBefore = pair.before.symbol.Shape
		}
	}
	if pair.after != nil {
		row.PathAfter = pair.after.file.path
		if pair.before == nil || pair.before.symbol.Shape != pair.after.symbol.Shape {
			row.ShapeAfter = pair.after.symbol.Shape
		}
	}
	if class == ClassSignature {
		row.ShapeDiff = DiffShapes(pair.before.symbol.Shape, pair.after.symbol.Shape)
	}
	return row, nil
}
