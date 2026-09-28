package symboldiff

import (
	"fmt"

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
// added symbol, the old one of a removed symbol, and both of a signature change.
func (pair *pairing) shapesRead() bool {
	switch pair.class {
	case ClassAdded:
		return pair.after.decoded()
	case ClassRemoved:
		return pair.before.decoded()
	case ClassSignature:
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
