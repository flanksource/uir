package symboldiff

import (
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var sideNames = [2]string{"old", "new"}

// definitionSites are the changed documents that define one symbol handle on each side, and how many
// declarations they hold. More than one declaration on a side happens only for the func init
// declarations of a package, which share one canonical id (gavel TODO 34ea4d17).
type definitionSites struct {
	files       [2][]*fileSide
	occurrences [2]int
}

func (sites *definitionSites) multiple() bool {
	return sites.occurrences[0] > 1 || sites.occurrences[1] > 1
}

func (sites *definitionSites) defines(side int, document *fileSide) bool {
	return slices.Contains(sites.files[side], document)
}

// class compares a singly declared symbol's effective fingerprints on the two sides, and its defining
// path when they are equal.
func (sites *definitionSites) class(before, after storage.EffectiveSymbol) Class {
	switch {
	case len(sites.files[0]) == 0:
		return ClassAdded
	case len(sites.files[1]) == 0:
		return ClassRemoved
	case *before.ShapeFP != *after.ShapeFP:
		return ClassSignature
	case *before.BodyFP != *after.BodyFP:
		return ClassBody
	case sites.files[0][0].path != sites.files[1][0].path:
		return ClassMoved
	}
	return ""
}

// symbolPlan classifies, from the two snapshots' effective symbols, every symbol that the changed
// documents of packages with canonical ids define, and chooses the documents the output must read.
// Keyed packages, syntax or partial on either side, keep key matching over their decoded documents.
type symbolPlan struct {
	keyed     map[string]bool
	effective [2]map[int64]storage.EffectiveSymbol
	sites     map[int64]*definitionSites
	classes   map[int64]Class
	read      map[*fileSide]bool
}

func planSymbols(ctx context.Context, database *gorm.DB, snapshots [2]uuid.UUID, files []changedFile, visibility Visibility, stat bool) (*symbolPlan, error) {
	plan := &symbolPlan{keyed: keyedPackages(files), sites: map[int64]*definitionSites{}, classes: map[int64]Class{}, read: map[*fileSide]bool{}}
	for side, snapshot := range snapshots {
		symbols, err := storage.EffectiveSymbols(ctx, database, snapshot)
		if err != nil {
			return nil, err
		}
		plan.effective[side] = make(map[int64]storage.EffectiveSymbol, len(symbols))
		for _, symbol := range symbols {
			plan.effective[side][symbol.Handle] = symbol
		}
	}
	if err := plan.loadSites(ctx, database, files); err != nil {
		return nil, err
	}
	if err := plan.classify(); err != nil {
		return nil, err
	}
	return plan, plan.chooseReads(files, visibility, stat)
}

// keyedPackages are the package paths with a syntax or partial changed document on either side.
func keyedPackages(files []changedFile) map[string]bool {
	keyed := map[string]bool{}
	for _, file := range files {
		for _, side := range file.sides() {
			if side != nil && (side.coverage() == storage.CoverageSyntax || side.coverage() == storage.CoveragePartial) {
				keyed[side.active.Source.PackagePath] = true
			}
		}
	}
	return keyed
}

func (plan *symbolPlan) keyedFile(file changedFile) bool {
	for _, side := range file.sides() {
		if side != nil && plan.keyed[side.active.Source.PackagePath] {
			return true
		}
	}
	return false
}

// loadSites reads the definition postings of each side's changed typed documents outside keyed packages.
func (plan *symbolPlan) loadSites(ctx context.Context, database *gorm.DB, files []changedFile) error {
	for side := range 2 {
		byOrdinal := map[int64]*fileSide{}
		for _, file := range files {
			document := file.sides()[side]
			if document != nil && !plan.keyedFile(file) && document.coverage() == storage.CoverageIndexed {
				byOrdinal[document.active.Document.Ordinal] = document
			}
		}
		ordinals := make([]int64, 0, len(byOrdinal))
		for ordinal := range byOrdinal {
			ordinals = append(ordinals, ordinal)
		}
		slices.Sort(ordinals)
		postings, err := storage.DefinitionPostings(ctx, database, ordinals)
		if err != nil {
			return err
		}
		for _, posting := range postings {
			sites := plan.sites[posting.SymbolHandle]
			if sites == nil {
				sites = &definitionSites{}
				plan.sites[posting.SymbolHandle] = sites
			}
			sites.files[side] = append(sites.files[side], byOrdinal[posting.DocumentOrdinal])
			sites.occurrences[side] += posting.OccurrenceCount
		}
	}
	return nil
}

// classify checks that each side's changed documents define a symbol exactly when that side's
// effective symbols contain it, and classifies every singly declared one.
func (plan *symbolPlan) classify() error {
	for handle, sites := range plan.sites {
		for side := range 2 {
			_, effective := plan.effective[side][handle]
			if declared := len(sites.files[side]) > 0; effective != declared {
				return fmt.Errorf("symbol handle %d: the %s snapshot's effective symbols contain it (%t) but its changed documents define it (%t)", handle, sideNames[side], effective, declared)
			}
		}
		if !sites.multiple() {
			plan.classes[handle] = sites.class(plan.effective[0][handle], plan.effective[1][handle])
		}
	}
	return nil
}

// chooseReads marks the documents the output needs: every changed document for line counts; keyed
// and excluded documents; both sides of a symbol declared more than once; and, for a visible row, the
// new side of an added symbol, the old side of a removed one, and both sides of a signature change.
func (plan *symbolPlan) chooseReads(files []changedFile, visibility Visibility, stat bool) error {
	for _, file := range files {
		for _, document := range file.sides() {
			if document != nil && (stat || plan.keyedFile(file) || document.coverage() == storage.CoverageExcluded) {
				plan.read[document] = true
			}
		}
	}
	for handle, sites := range plan.sites {
		visible, err := visibleHandle(handle, visibility)
		if err != nil {
			return err
		}
		class, single := plan.classes[handle]
		read := [2]bool{!single, !single}
		if single && visible {
			read = [2]bool{class == ClassRemoved || class == ClassSignature, class == ClassAdded || class == ClassSignature}
		}
		for side := range 2 {
			for _, document := range sites.files[side] {
				if read[side] {
					plan.read[document] = true
				}
			}
		}
	}
	return nil
}

func visibleHandle(handle int64, visibility Visibility) (bool, error) {
	if visibility == VisibilityAll {
		return true, nil
	}
	fields, err := symbolhandle.Unpack(handle)
	return fields.Visibility.String() == string(visibility), err
}

// verifyCovered fails when a symbol whose effective state differs between the snapshots is declared
// by neither the changed documents' definition postings nor a keyed document.
func (plan *symbolPlan) verifyCovered(keyed map[int64]bool) error {
	for side := range 2 {
		for handle, symbol := range plan.effective[side] {
			other, found := plan.effective[1-side][handle]
			if found && *other.ShapeFP == *symbol.ShapeFP && *other.BodyFP == *symbol.BodyFP {
				continue
			}
			if plan.sites[handle] == nil && !keyed[handle] {
				return fmt.Errorf("symbol handle %d changed between the snapshots, but no changed document declares it", handle)
			}
		}
	}
	return nil
}
