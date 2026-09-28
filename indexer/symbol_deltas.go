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
func appendDefinition(definitions []definition, id *string, shapeHash, bodyHash string) ([]definition, error) {
	if id == nil {
		return definitions, nil
	}
	shape, err := storage.SymbolFingerprint(shapeHash)
	if err != nil {
		return nil, fmt.Errorf("shape of symbol %s: %w", *id, err)
	}
	body, err := storage.SymbolFingerprint(bodyHash)
	if err != nil {
		return nil, fmt.Errorf("body of symbol %s: %w", *id, err)
	}
	return append(definitions, definition{id: *id, fingerprints: storage.SymbolFingerprints{Shape: shape, Body: body}}), nil
}

// declaredContent is the part of a stored document's content that symbol deltas read.
type declaredContent struct {
	Symbols []struct {
		ID        *string `json:"id"`
		ShapeHash string  `json:"shape_hash"`
		BodyHash  string  `json:"body_hash"`
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

// createSymbolDeltas records how the snapshot's defined-symbol set differs from its base's. A path's
// active document is keyed by its package input hash, so only paths whose key differs between base
// and snapshot (added, removed, or re-extracted) are compared; unchanged documents are never read. The
// base side decodes the changed paths' stored documents, the snapshot side uses the extraction. A
// symbol declared on both sides with equal fingerprints, including one that moved between two changed
// files, gets no row; a new or changed one a set row, and one only the base declared a delete row.
// Without a base every declared symbol is a set.
func createSymbolDeltas(ctx context.Context, database *gorm.DB, snapshot storage.ModuleSnapshot, publication snapshotPublication, previous map[string]storage.SourceRevision, known map[string]int64) error {
	baseKeys, err := baseDocumentKeys(ctx, database, publication.base.snapshot, previous)
	if err != nil {
		return err
	}
	extraction := publication.extraction
	currentKeys, packages := map[string]string{}, map[string]string{}
	for _, file := range extraction.root.Files {
		currentKeys[file.PathKey], packages[file.PathKey] = extraction.documents[file.PathKey].inputHash, file.PackagePath
	}
	after := newDeclarations()
	for path, key := range currentKeys {
		if baseKeys[path] != key {
			if err := after.add(packages[path], extraction.documents[path].definitions, declarations{}); err != nil {
				return err
			}
		}
	}
	before, err := baseDeclarations(ctx, database, snapshot.RootID, changedKeys(baseKeys, currentKeys), after)
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

// baseDocumentKeys maps each path of the base snapshot to the input hash of its active document.
func baseDocumentKeys(ctx context.Context, database *gorm.DB, base storage.ModuleSnapshot, previous map[string]storage.SourceRevision) (map[string]string, error) {
	keys := map[string]string{}
	if base.ID == uuid.Nil {
		return keys, nil
	}
	var coverage []storage.PackageCoverage
	if err := database.WithContext(ctx).Select("package_path", "input_hash").Where("snapshot_id = ?", base.ID).Find(&coverage).Error; err != nil {
		return nil, fmt.Errorf("load package input hashes of base snapshot %s: %w", base.ID, err)
	}
	hashes := make(map[string]string, len(coverage))
	for _, row := range coverage {
		hashes[row.PackagePath] = row.InputHash
	}
	for path, revision := range previous {
		hash, found := hashes[revision.PackagePath]
		if !found {
			return nil, fmt.Errorf("base snapshot %s has no package coverage for %q of %q", base.ID, revision.PackagePath, path)
		}
		keys[path] = hash
	}
	return keys, nil
}

// changedKeys is the base's path → input hash for the paths whose active document the snapshot replaces
// or removes.
func changedKeys(baseKeys, currentKeys map[string]string) map[string]string {
	changed := map[string]string{}
	for path, key := range baseKeys {
		if currentKeys[path] != key {
			changed[path] = key
		}
	}
	return changed
}

// baseDeclarations reads the definitions of the base's changed documents in batches.
func baseDeclarations(ctx context.Context, database *gorm.DB, rootID uuid.UUID, changed map[string]string, after declarations) (declarations, error) {
	before := newDeclarations()
	paths := sortedKeys(changed)
	found := 0
	for start := 0; start < len(paths); start += publicationBatch {
		batch := paths[start:min(start+publicationBatch, len(paths))]
		hashes := make([]string, len(batch))
		for i, path := range batch {
			hashes[i] = changed[path]
		}
		var documents []storage.Document
		if err := database.WithContext(ctx).Select("path_key", "input_hash", "package_path", "content").
			Where("root_id = ? AND path_key IN ? AND input_hash IN ?", rootID, batch, hashes).Find(&documents).Error; err != nil {
			return declarations{}, fmt.Errorf("load changed base documents: %w", err)
		}
		for _, document := range documents {
			if changed[document.PathKey] != document.InputHash {
				continue
			}
			found++
			if err := before.addStored(document, after); err != nil {
				return declarations{}, err
			}
		}
	}
	if found != len(paths) {
		return declarations{}, fmt.Errorf("base has %d changed documents, found %d", len(paths), found)
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
		if definitions, err = appendDefinition(definitions, symbol.ID, symbol.ShapeHash, symbol.BodyHash); err != nil {
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
			set.SymbolHandle, set.Operation, set.ShapeFP, set.BodyFP = handleOf(id), storage.SourceSet, &fingerprints.Shape, &fingerprints.Body
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
