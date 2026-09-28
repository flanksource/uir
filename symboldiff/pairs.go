package symboldiff

import (
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

// pairSymbols pairs the changed files' symbols after readDocuments. A singly declared symbol outside
// keyed packages is paired by handle and keeps its delta class; it is described by its decoded
// declarations when every side's document was read, and from the symbols table otherwise. Without line
// counts an unchanged one is left out. Keyed declarations and those of a symbol declared more than
// once go through matchSymbols.
func pairSymbols(ctx context.Context, database *gorm.DB, files []changedFile, plan *symbolPlan, stat bool) ([]*pairing, error) {
	handles, err := declarationHandles(ctx, database, files)
	if err != nil {
		return nil, err
	}
	var matched [2][]*entry
	decoded := map[*fileSide]map[int64]*entry{}
	matchedHandles := map[int64]bool{}
	for _, file := range files {
		for side, document := range file.sides() {
			if document == nil || !document.decoded {
				continue
			}
			decoded[document] = map[int64]*entry{}
			for index, symbol := range document.content.Symbols {
				declared := &entry{file: document, symbol: symbol, index: index}
				single, err := plan.singlyDeclared(file, side, declared, handles)
				switch {
				case err != nil:
					return nil, err
				case single:
					decoded[document][handles[*symbol.ID]] = declared
				default:
					matched[side] = append(matched[side], declared)
					if symbol.ID != nil {
						matchedHandles[handles[*symbol.ID]] = true
					}
				}
			}
		}
	}
	if err := plan.verifyCovered(matchedHandles); err != nil {
		return nil, err
	}
	singles, err := plan.singlePairings(ctx, database, decoded, stat)
	if err != nil {
		return nil, err
	}
	return append(singles, matchSymbols(matched[0], matched[1])...), nil
}

// singlyDeclared reports whether a decoded declaration is paired by handle: it lies outside keyed
// packages and its symbol has one declaration per side. Such a declaration must have an id and a
// definition posting in its document.
func (plan *symbolPlan) singlyDeclared(file changedFile, side int, declared *entry, handles map[string]int64) (bool, error) {
	if plan.keyedFile(file) {
		return false, nil
	}
	document, symbol := declared.file, declared.symbol
	if symbol.ID == nil {
		return false, fmt.Errorf("%s document %s for %q declares %s without a canonical id", document.coverage(), document.active.Document.ID, document.path, symbol.Key)
	}
	sites := plan.sites[handles[*symbol.ID]]
	if sites == nil || !sites.defines(side, document) {
		return false, fmt.Errorf("document %s for %q declares symbol %s without a definition posting", document.active.Document.ID, document.path, *symbol.ID)
	}
	return !sites.multiple(), nil
}

// singlePairings pairs each singly declared symbol by handle, in handle order.
func (plan *symbolPlan) singlePairings(ctx context.Context, database *gorm.DB, decoded map[*fileSide]map[int64]*entry, stat bool) ([]*pairing, error) {
	var reported, light []int64
	for handle, class := range plan.classes {
		if class == "" && !stat {
			continue
		}
		reported = append(reported, handle)
		for _, documents := range plan.sites[handle].files {
			if len(documents) > 0 && !documents[0].decoded {
				light = append(light, handle)
				break
			}
		}
	}
	slices.Sort(reported)
	slices.Sort(light)
	owned, err := storage.OwnedSymbols(ctx, database, light)
	if err != nil {
		return nil, err
	}
	pairings := make([]*pairing, 0, len(reported))
	for _, handle := range reported {
		pair := &pairing{class: plan.classes[handle]}
		ends := [2]**entry{&pair.before, &pair.after}
		for side, documents := range plan.sites[handle].files {
			if len(documents) == 0 {
				continue
			}
			if symbol, found := owned[handle]; found {
				if *ends[side], err = lightEntry(documents[0], symbol); err != nil {
					return nil, err
				}
				continue
			}
			if *ends[side] = decoded[documents[0]][handle]; *ends[side] == nil {
				return nil, fmt.Errorf("decoded document %s for %q has no declaration of symbol handle %d", documents[0].active.Document.ID, documents[0].path, handle)
			}
		}
		pairings = append(pairings, pair)
	}
	return pairings, nil
}
