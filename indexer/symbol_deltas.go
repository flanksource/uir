package indexer

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// definition is one declaration of a canonical symbol with its fingerprints.
type definition struct {
	id           string
	fingerprints storage.SymbolFingerprints
}

// appendDefinition adds a proven declaration (one with a canonical id) to definitions; an unproven or
// syntax declaration has no id and is skipped, since it has no handle.
func appendDefinition(definitions []definition, symbol storage.DocumentSymbol) ([]definition, error) {
	if symbol.ID == nil {
		return definitions, nil
	}
	fingerprints, err := symbol.Fingerprints()
	if err != nil {
		return nil, fmt.Errorf("symbol %s: %w", *symbol.ID, err)
	}
	return append(definitions, definition{id: *symbol.ID, fingerprints: fingerprints}), nil
}

// declaredContent is the part of a stored document's content that symbol deltas read.
type declaredContent struct {
	Symbols []struct {
		ID          *string `json:"id"`
		PublicHash  string  `json:"public_hash"`
		PrivateHash string  `json:"private_hash"`
		ContentHash string  `json:"content_hash"`
	} `json:"symbols"`
}

// declarations collects the definitions of changed documents per symbol id, remembering the package
// directory that declared each id.
type declarations struct {
	fingerprints map[string][]storage.SymbolFingerprints
	packages     map[string]string
}

func newDeclarations() declarations {
	return declarations{fingerprints: map[string][]storage.SymbolFingerprints{}, packages: map[string]string{}}
}

// add records a document's definitions. A symbol's canonical id includes its Go package, whose files
// all lie in one directory, so every declaration of an id comes from documents of one package
// directory, which share one input hash and are replaced together. That is what lets symbol deltas
// read only changed documents; a violation is an error, not a silently wrong delta.
func (collected declarations) add(packagePath string, definitions []definition, other declarations) error {
	for _, declared := range definitions {
		for _, seen := range []declarations{collected, other} {
			if prior, found := seen.packages[declared.id]; found && prior != packagePath {
				return fmt.Errorf("symbol %s is declared in package directories %q and %q", declared.id, prior, packagePath)
			}
		}
		collected.packages[declared.id] = packagePath
		collected.fingerprints[declared.id] = append(collected.fingerprints[declared.id], declared.fingerprints)
	}
	return nil
}

func (collected declarations) folded() map[string]storage.SymbolFingerprints {
	folded := make(map[string]storage.SymbolFingerprints, len(collected.fingerprints))
	for id, fingerprints := range collected.fingerprints {
		folded[id] = storage.FoldDeclarations(fingerprints)
	}
	return folded
}

// createSymbolDeltas records how the snapshot's defined-symbol set differs from its base's. Only the
// packages with a path whose bound document differs between base and snapshot (added, removed, or
// rebound) are compared; documents of every other package are never read. A changed package is compared
// whole, every file on both sides, because one id (each func init) may be declared in several of its
// files, and its fingerprints fold across all of them. The base side decodes its stored documents, the
// snapshot side uses the extraction. A symbol declared on both sides with equal fingerprints, including
// one that moved between two files, gets no row; a new or changed one a set row, and one only the base
// declared a delete row. Without a base every declared symbol is a set.
func createSymbolDeltas(ctx context.Context, database *gorm.DB, snapshot storage.ModuleSnapshot, publication snapshotPublication, membership snapshotMembership, known map[string]int64) error {
	changed := changedPackages(membership)
	extraction := publication.extraction
	after := newDeclarations()
	for _, file := range extraction.root.Files {
		if !changed[file.PackagePath] {
			continue
		}
		if extraction.documents[file.PathKey].reused {
			return fmt.Errorf("document for %q was reused from the base, but package %q changed in snapshot %s", file.PathKey, file.PackagePath, snapshot.ID)
		}
		if err := after.add(file.PackagePath, extraction.documents[file.PathKey].definitions, declarations{}); err != nil {
			return err
		}
	}
	before, err := baseDeclarations(ctx, database, membership.base, changed, after)
	if err != nil {
		return err
	}
	key := storage.SymbolDelta{SnapshotOrdinal: snapshot.Ordinal, RootOrdinal: publication.root.Ordinal}
	rows, err := deltaRows(ctx, database, key, before.folded(), after.folded(), known)
	if err != nil || len(rows) == 0 {
		return err
	}
	if err := database.WithContext(ctx).CreateInBatches(rows, publicationBatch).Error; err != nil {
		return fmt.Errorf("create %d symbol deltas for snapshot %s: %w", len(rows), snapshot.ID, err)
	}
	return nil
}

// changedPackages are the package paths with a path whose bound document the snapshot adds, rebinds, or
// removes against its base.
func changedPackages(membership snapshotMembership) map[string]bool {
	changed := map[string]bool{}
	for path, ordinal := range membership.bindings {
		prior, found := membership.base[path]
		if !found || prior.Document.Ordinal != ordinal {
			changed[membership.current[path].PackagePath] = true
			if found {
				changed[prior.Source.PackagePath] = true
			}
		}
	}
	for path, prior := range membership.base {
		if _, served := membership.bindings[path]; !served {
			changed[prior.Source.PackagePath] = true
		}
	}
	return changed
}

// baseDeclarations reads the definitions of the base's documents of the changed packages.
func baseDeclarations(ctx context.Context, database *gorm.DB, base map[string]storage.ActiveDocument, changed map[string]bool, after declarations) (declarations, error) {
	before := newDeclarations()
	var ids []uuid.UUID
	for _, path := range sortedKeys(base) {
		if changed[base[path].Source.PackagePath] {
			ids = append(ids, base[path].Document.ID)
		}
	}
	contents, err := storage.DocumentContents(ctx, database, ids)
	if err != nil {
		return declarations{}, fmt.Errorf("load changed base documents: %w", err)
	}
	for _, path := range sortedKeys(base) {
		document := base[path].Document
		if !changed[base[path].Source.PackagePath] {
			continue
		}
		document.Content = contents[document.ID]
		if err := before.addStored(document, after); err != nil {
			return declarations{}, err
		}
	}
	return before, nil
}

func (collected declarations) addStored(document storage.Document, after declarations) error {
	var content declaredContent
	if err := json.Unmarshal(document.Content, &content); err != nil {
		return fmt.Errorf("decode the declarations of base document for %q: %w", document.PathKey, err)
	}
	var definitions []definition
	for _, symbol := range content.Symbols {
		var err error
		declared := storage.DocumentSymbol{ID: symbol.ID, PublicHash: symbol.PublicHash, PrivateHash: symbol.PrivateHash, ContentHash: symbol.ContentHash}
		if definitions, err = appendDefinition(definitions, declared); err != nil {
			return fmt.Errorf("base document for %q: %w", document.PathKey, err)
		}
	}
	return collected.add(document.PackagePath, definitions, after)
}

// deltaRows compares the changed documents' folded definitions, sorted by handle; every row carries the
// snapshot and root ordinals of key.
func deltaRows(ctx context.Context, database *gorm.DB, key storage.SymbolDelta, before, after map[string]storage.SymbolFingerprints, known map[string]int64) ([]storage.SymbolDelta, error) {
	var missing []string
	for _, side := range []map[string]storage.SymbolFingerprints{before, after} {
		for id := range side {
			if _, found := known[id]; !found {
				missing = append(missing, id)
			}
		}
	}
	handles, err := storage.SymbolHandles(ctx, database, missing)
	if err != nil {
		return nil, err
	}
	handleOf := func(id string) int64 {
		if handle, found := known[id]; found {
			return handle
		}
		return handles[id]
	}
	var rows []storage.SymbolDelta
	for id, fingerprints := range after {
		if prior, found := before[id]; !found || prior != fingerprints {
			set := key
			set.SymbolHandle, set.Operation = handleOf(id), storage.SourceSet
			set.PublicFP, set.PrivateFP, set.ContentFP = &fingerprints.Public, &fingerprints.Private, &fingerprints.Content
			rows = append(rows, set)
		}
	}
	for id := range before {
		if _, found := after[id]; !found {
			tombstone := key
			tombstone.SymbolHandle, tombstone.Operation = handleOf(id), storage.SourceDelete
			rows = append(rows, tombstone)
		}
	}
	slices.SortFunc(rows, func(left, right storage.SymbolDelta) int { return cmp.Compare(left.SymbolHandle, right.SymbolHandle) })
	return rows, nil
}
