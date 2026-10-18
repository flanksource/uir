package symboldiff

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
)

// The commits a patch diff names its sides by: a patch records blobs, not commits.
const (
	PatchOld      = "patch:old"
	PatchNew      = "patch:new"
	PatchWorktree = "worktree"
)

// NotGoSource is the exclusion reason of a patched file that is not Go source.
const NotGoSource = "not Go source"

// BlobRequest is one side of a patched file: its path and the blob id the patch's index line names.
type BlobRequest struct {
	Path string
	ID   string
	New  bool
}

// Blob is the bytes of one side; Worktree reports that they were read from the worktree, not a blob.
type Blob struct {
	Content  []byte
	Worktree bool
}

// SourceLocator is where a patch's sides are read from and what names their packages and the diff's root.
type SourceLocator interface {
	// Read returns the bytes of every request, in request order.
	Read(ctx context.Context, requests []BlobRequest) ([]Blob, error)
	// PackagePath is the import path of the package holding file.
	PackagePath(file string) (string, error)
	// RootKey names the diff's root.
	RootKey() (string, error)
}

// PatchOptions names a git patch, the locator its sides are read from, and what the diff reports.
type PatchOptions struct {
	Patch io.Reader
	// From is the commit the patch's old side was taken from, when known; empty names it patch:old.
	From        string
	Locator     SourceLocator
	Visibility  Visibility
	ChangeTypes ChangeTypes
	Stat        bool
}

// DiffPatch compares the two sides of a git patch symbol by symbol, without an index: each side's bytes
// are what the locator reads for the blob its index line names. Go files are extracted from syntax
// alone, so every row is marked syntax; the root is the locator's root key.
func DiffPatch(ctx context.Context, options PatchOptions) (Result, error) {
	visibility, err := ParseVisibility(string(options.Visibility))
	if err != nil {
		return Result{}, err
	}
	changeTypes, err := ParseChangeTypes(options.ChangeTypes)
	if err != nil {
		return Result{}, err
	}
	if options.Locator == nil {
		return Result{}, fmt.Errorf("a patch diff needs a source locator to read its sides from")
	}
	patch, err := ParsePatch(options.Patch)
	if err != nil {
		return Result{}, err
	}
	rootKey, err := options.Locator.RootKey()
	if err != nil {
		return Result{}, err
	}
	requests, err := patchRequests(patch)
	if err != nil {
		return Result{}, err
	}
	blobs, err := options.Locator.Read(ctx, requests)
	if err != nil {
		return Result{}, err
	}
	files, worktree, err := patchedFiles(options.Locator, requests, blobs)
	if err != nil {
		return Result{}, err
	}
	result := Result{RootKey: rootKey, From: Side{Commit: PatchOld}, To: Side{Commit: PatchNew}, Visibility: visibility, ChangeTypes: changeTypes, Stat: options.Stat}
	if options.From != "" {
		result.From.Commit = options.From
	}
	if worktree {
		result.To.Commit = PatchWorktree
	}
	pairings, err := patchPairings(files)
	if err != nil {
		return Result{}, err
	}
	markers := patchMarkers(files)
	var attribution *lineAttribution
	if options.Stat {
		inline := blobSource{inline: true}
		lines := attributeLines(ctx, files, pairings, inline, inline)
		attribution = &lines
	}
	rows, err := buildRows(pairings, attribution, markers, rowFilter{visibility: visibility, changeTypes: changeTypes}, TokenizerGo)
	if err != nil {
		return Result{}, err
	}
	result.Packages = assemble(files, rows, attribution, markers, nil, rootKey, len(changeTypes) > 0)
	return result, nil
}

// patchRequests is every present side of every patched file, the old side of a file before its new one.
func patchRequests(patch []PatchFile) ([]BlobRequest, error) {
	var requests []BlobRequest
	for _, file := range patch {
		if file.OldBlob == "" && file.NewBlob == "" {
			return nil, fmt.Errorf("patch section of %s has no index line with blob ids; pipe the output of git diff", strings.TrimSpace(file.OldPath+" "+file.NewPath))
		}
		for _, request := range []BlobRequest{{Path: file.OldPath, ID: file.OldBlob}, {Path: file.NewPath, ID: file.NewBlob, New: true}} {
			switch {
			case request.Path == "":
				continue
			case request.ID == "":
				return nil, fmt.Errorf("%s side of %s has no blob id in the patch's index line", sideLabel(request), request.Path)
			}
			requests = append(requests, request)
		}
	}
	return requests, nil
}

func sideLabel(request BlobRequest) string {
	if request.New {
		return "new"
	}
	return "old"
}

// patchedFiles pairs the sides read for every request by path. A rename is the removal of its old path
// and the addition of its new one, as a snapshot diff lists it, so its symbols pair across the two as
// moved. worktree reports whether any new side was read from the worktree.
func patchedFiles(locator SourceLocator, requests []BlobRequest, blobs []Blob) ([]changedFile, bool, error) {
	if len(blobs) != len(requests) {
		return nil, false, fmt.Errorf("source locator returned %d blobs for %d requests", len(blobs), len(requests))
	}
	byPath := map[string]*changedFile{}
	worktree := false
	for index, request := range requests {
		side, err := patchSide(locator, request, blobs[index].Content)
		if err != nil {
			return nil, false, err
		}
		file := byPath[request.Path]
		if file == nil {
			file = &changedFile{path: request.Path}
			byPath[request.Path] = file
		}
		if request.New {
			file.after, worktree = side, worktree || blobs[index].Worktree
		} else {
			file.before = side
		}
	}
	files := make([]changedFile, 0, len(byPath))
	for _, file := range byPath {
		files = append(files, *file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, worktree, nil
}

// patchSide is one side of a patched file with its syntax document, or excluded when it is not Go source.
func patchSide(locator SourceLocator, request BlobRequest, content []byte) (*fileSide, error) {
	file, label := request.Path, sideLabel(request)
	packagePath, err := locator.PackagePath(file)
	if err != nil {
		return nil, err
	}
	side := &fileSide{path: file, owners: map[int]bool{}, source: content}
	side.active.Source.ContentHash = hashBytes(content)
	if path.Ext(file) != ".go" {
		side.active.Source.PackagePath, side.active.Document.Coverage = packagePath, storage.CoverageExcluded
		side.setContent(storage.DocumentContent{PackagePath: packagePath, Excluded: NotGoSource})
		return side, nil
	}
	document, err := indexer.SyntaxDocument(file, packagePath, content)
	if err != nil {
		return nil, fmt.Errorf("%s side of the patch: %w", label, err)
	}
	for index := range document.Symbols {
		id, err := patchSymbolID(document.Symbols[index].Identifier)
		if err != nil {
			return nil, fmt.Errorf("%s side of %s: %w", label, file, err)
		}
		document.Symbols[index].ID = &id
	}
	side.active.Source.PackagePath, side.active.Document.Coverage = document.PackagePath, storage.CoverageSyntax
	side.setContent(document)
	return side, nil
}

// patchSymbolID stands in for a canonical symbol id, which needs type information a patch does not
// carry: the identity key with a function's signature reduced to its parameter types, which are what
// a canonical id distinguishes overloads by, so renaming a parameter is a signature change.
func patchSymbolID(identifier uir.Identifier) (string, error) {
	if identifier.Signature == "" {
		return identifier.IdentityKey(), nil
	}
	file, err := parser.ParseFile(token.NewFileSet(), "", "package p\nfunc f"+identifier.Signature, parser.SkipObjectResolution)
	if err != nil {
		return "", fmt.Errorf("parse signature %q of %s: %w", identifier.Signature, identifier.Method, err)
	}
	var parameters []string
	for _, field := range file.Decls[0].(*ast.FuncDecl).Type.Params.List {
		for range max(1, len(field.Names)) {
			parameters = append(parameters, types.ExprString(field.Type))
		}
	}
	identifier.Signature = "(" + strings.Join(parameters, ", ") + ")"
	return identifier.IdentityKey(), nil
}

// patchPairings matches every declaration of the patched files by its stand-in id, then pairs symbols
// moved between packages.
func patchPairings(files []changedFile) ([]*pairing, error) {
	var entries [2][]*entry
	for _, file := range files {
		for side, document := range file.sides() {
			if document == nil {
				continue
			}
			for index, symbol := range document.content.Symbols {
				entries[side] = append(entries[side], &entry{file: document, symbol: symbol, index: index})
			}
		}
	}
	return pairAcrossPackages(matchSymbols(entries[0], entries[1]))
}

func patchMarkers(files []changedFile) coverageMarkers {
	markers := coverageMarkers{before: map[string]storage.ActiveDocument{}, after: map[string]storage.ActiveDocument{}}
	for _, file := range files {
		if file.before != nil {
			markers.before[file.path] = file.before.active
		}
		if file.after != nil {
			markers.after[file.path] = file.after.active
		}
	}
	return markers
}
