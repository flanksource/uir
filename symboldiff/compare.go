package symboldiff

import (
	"fmt"
	"path"
	"regexp"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
)

// entry is one symbol declared by one side of a changed file. index is its position in the decoded
// document, or -1 for a lightEntry described from the symbols table because its document was not read.
type entry struct {
	file   *fileSide
	symbol storage.DocumentSymbol
	index  int
}

func (entry *entry) owner() bool   { return entry.decoded() && entry.file.owners[entry.index] }
func (entry *entry) decoded() bool { return entry.index >= 0 }

// pairing is a symbol matched across the two sides; class is empty when it is unchanged.
type pairing struct {
	before, after *entry
	class         Class
}

// shapesRead reports whether every shape the pairing's row shows was decoded: the new shape of an
// added symbol, the old one of a removed symbol, and both of a signature or structure change.
func (pair *pairing) shapesRead() bool {
	switch pair.class {
	case ClassAdded:
		return pair.after.decoded()
	case ClassRemoved:
		return pair.before.decoded()
	case ClassSignature, ClassStructure:
		return pair.before.decoded() && pair.after.decoded()
	}
	return true
}

// matchSymbols pairs decoded declarations that symbol deltas cannot classify, those of keyed
// packages and of symbols declared more than once: by id first, so a moved symbol is matched by
// identity, then by key where either side has no id (a syntax document or an unproven partial
// declaration). Unmatched entries are removed or added; classes come from the documents' hashes.
func matchSymbols(before, after []*entry) []*pairing {
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

// moveKey groups the removed and added symbols that could be one symbol moved between packages: the
// identity a move keeps, which is everything but the package.
func moveKey(symbol storage.DocumentSymbol) string {
	owner, name := ownerAndName(symbol.Identifier)
	return symbol.Kind + "|" + owner + "|" + name
}

// moveCandidates are the removed and added pairings, by moveKey, whose key names exactly one removed
// and one added symbol in different packages. Any other count is ambiguous and is never guessed.
func moveCandidates(pairings []*pairing) [][2]*pairing {
	removed, added := map[string][]*pairing{}, map[string][]*pairing{}
	var keys []string
	for _, pair := range pairings {
		switch pair.class {
		case ClassRemoved:
			key := moveKey(pair.before.symbol)
			if len(removed[key]) == 0 && len(added[key]) == 0 {
				keys = append(keys, key)
			}
			removed[key] = append(removed[key], pair)
		case ClassAdded:
			key := moveKey(pair.after.symbol)
			if len(removed[key]) == 0 && len(added[key]) == 0 {
				keys = append(keys, key)
			}
			added[key] = append(added[key], pair)
		}
	}
	var candidates [][2]*pairing
	for _, key := range keys {
		if len(removed[key]) != 1 || len(added[key]) != 1 {
			continue
		}
		old, current := removed[key][0], added[key][0]
		if old.before.file.active.Source.PackagePath != current.after.file.active.Source.PackagePath {
			candidates = append(candidates, [2]*pairing{old, current})
		}
	}
	return candidates
}

// pairAcrossPackages pairs each move candidate whose shapes are equal but for the qualifiers of the two
// packages, since a move turns models.Item into Item and back. The pair is classified by its layer
// hashes like any other: moved when nothing but its file changed, else its changed layer. Both sides
// must be decoded, for their shapes and hashes.
func pairAcrossPackages(pairings []*pairing) ([]*pairing, error) {
	merged := map[*pairing]bool{}
	var result []*pairing
	for _, candidate := range moveCandidates(pairings) {
		old, current := candidate[0].before, candidate[1].after
		if !old.decoded() || !current.decoded() {
			return nil, fmt.Errorf("symbol %s moved from %q to %q, but its documents were not read to compare their shapes", moveKey(old.symbol), old.file.path, current.file.path)
		}
		if !movedShapeEqual(old.symbol.Shape, current.symbol.Shape, old.file.active.Source.PackagePath, current.file.active.Source.PackagePath) {
			continue
		}
		pair := &pairing{before: old, after: current}
		pair.class = classify(pair)
		merged[candidate[0]], merged[candidate[1]] = true, true
		result = append(result, pair)
	}
	for _, pair := range pairings {
		if !merged[pair] {
			result = append(result, pair)
		}
	}
	return result, nil
}

// movedShapeEqual reports whether two shapes are equal once the qualifiers naming either package, by
// its last import path element, are removed.
func movedShapeEqual(before, after, fromPackage, toPackage string) bool {
	qualifiers := regexp.MustCompile(`\b(?:` + regexp.QuoteMeta(path.Base(fromPackage)) + `|` + regexp.QuoteMeta(path.Base(toPackage)) + `)\.`)
	return qualifiers.ReplaceAllString(before, "") == qualifiers.ReplaceAllString(after, "")
}

// classify compares the documents' layer hashes. The public and private hashes are comparable only
// when both sides have an id; a key-matched pair can only change in body or path.
func classify(pair *pairing) Class {
	switch {
	case pair.before == nil:
		return ClassAdded
	case pair.after == nil:
		return ClassRemoved
	}
	before, after := pair.before.symbol, pair.after.symbol
	identified := before.ID != nil && after.ID != nil
	return changedClass(identified && before.PublicHash != after.PublicHash, identified && before.PrivateHash != after.PrivateHash,
		before.ContentHash != after.ContentHash, pair.before.file.path != pair.after.file.path)
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

func (pair *pairing) visibleAs(visibility Visibility, class Class) bool {
	for _, side := range []*entry{pair.before, pair.after} {
		if side != nil && visibility.shows(class, side.symbol.Visibility) {
			return true
		}
	}
	return false
}

// shownShape is the shape of the layer a row of the class shows: the public shape of a signature
// change, the private (full) shape otherwise.
func shownShape(symbol storage.DocumentSymbol, class Class) string {
	if class == ClassSignature {
		return symbol.PublicShape
	}
	return symbol.Shape
}

// newRow describes a pairing. Shapes are included only where they differ, and a signature or structure
// row diffs its layer's shapes.
func newRow(pair *pairing, class Class, coverage []string, tokenizer Tokenizer) (Row, error) {
	reported := pair.reported()
	owner, name := ownerAndName(reported.symbol.Identifier)
	if name == "" {
		return Row{}, fmt.Errorf("symbol %q in %q has no name in its identifier", reported.symbol.Key, reported.file.path)
	}
	row := Row{Class: class, Kind: reported.symbol.Kind, Owner: owner, Name: name, Visibility: reported.symbol.Visibility, Coverage: coverage}
	var before, after string
	if pair.before != nil {
		row.PathBefore, before = pair.before.file.path, shownShape(pair.before.symbol, class)
	}
	if pair.after != nil {
		row.PathAfter, after = pair.after.file.path, shownShape(pair.after.symbol, class)
	}
	if pair.before == nil || pair.after == nil || before != after {
		row.ShapeBefore, row.ShapeAfter = before, after
	}
	if class == ClassSignature || class == ClassStructure {
		if before == after && pair.before.decoded() && pair.after.decoded() {
			return Row{}, fmt.Errorf("the %s row of %s in %q shows equal shapes on both sides of the layer whose hash differs", class, row.DisplayName(), reported.file.path)
		}
		row.ShapeDiff = DiffShapes(before, after, tokenizer)
	}
	return row, nil
}
