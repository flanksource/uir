package indexer

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
)

// DocumentError is a failure confined to one file's document. It is the only failure indexing
// degrades instead of failing the module: the file is published with a degraded document, its package
// is at most partial, and the failure is recorded as a uir/index diagnostic of that package.
type DocumentError struct {
	Path string
	Err  error
}

func (failure *DocumentError) Error() string {
	return fmt.Sprintf("document %q: %v", failure.Path, failure.Err)
}

func (failure *DocumentError) Unwrap() error { return failure.Err }

// asDocumentError is err's DocumentError, or nil when err is not confined to one document.
func asDocumentError(err error) *DocumentError {
	var failure *DocumentError
	if errors.As(err, &failure) {
		return failure
	}
	return nil
}

// degradedDocument is the document a file whose own document failed is published with instead, and
// what the uir/index diagnostic says it is.
type degradedDocument struct {
	document extractedDocument
	note     string
}

// degradeDocument picks the document a file whose own document, failed, did not pass is published
// with: its syntax extraction when failed was not already one and it renders and passes check, else an
// empty document, which must pass check. Both are syntax coverage, carrying no canonical symbols, so
// they need no symbol rows or postings. Only a DocumentError from check moves on to the empty
// document; any other error is returned.
func degradeDocument(file discoveredFile, failed extractedDocument, diagnostics []storage.DocumentDiagnostic, check func(extractedDocument) error) (degradedDocument, error) {
	note := "indexed as an empty document"
	if failed.coverage != storage.CoverageSyntax {
		degraded, reason, err := syntaxFallback(file, failed.inputHash, diagnostics, check)
		if err != nil || reason == "" {
			return degraded, err
		}
		note += reason
	}
	empty, err := syntaxDocument(storage.DocumentContent{
		Version: storage.DocumentFormatVersion, PackagePath: file.PackagePath, Symbols: []storage.DocumentSymbol{},
		Occurrences: []storage.DocumentOccurrence{}, Diagnostics: diagnostics,
	}, failed.inputHash)
	if err != nil {
		return degradedDocument{}, fmt.Errorf("encode empty document for %q: %w", file.PathKey, err)
	}
	if err := check(empty); err != nil {
		return degradedDocument{}, fmt.Errorf("the empty document for %q fails its checks: %v", file.PathKey, err)
	}
	return degradedDocument{document: empty, note: note}, nil
}

// syntaxFallback is a file's syntax extraction when it renders and passes check, or else the reason
// it cannot be the degraded document; an error from check that is not a DocumentError is returned.
func syntaxFallback(file discoveredFile, inputHash string, diagnostics []storage.DocumentDiagnostic, check func(extractedDocument) error) (degradedDocument, string, error) {
	content, _, err := renderDocument(nil, packageState{coverage: storage.CoverageSyntax}, file, diagnostics)
	if err != nil {
		return degradedDocument{}, fmt.Sprintf(", since its syntax extraction failed too: %v", err), nil
	}
	candidate, err := syntaxDocument(content, inputHash)
	if err != nil {
		return degradedDocument{}, "", fmt.Errorf("encode degraded document for %q: %w", file.PathKey, err)
	}
	err = check(candidate)
	if failure := asDocumentError(err); failure != nil {
		return degradedDocument{}, fmt.Sprintf(", since its syntax document failed too: %v", failure.Err), nil
	}
	return degradedDocument{document: candidate, note: "indexed from syntax only"}, "", err
}

func syntaxDocument(content storage.DocumentContent, inputHash string) (extractedDocument, error) {
	encoded, err := json.Marshal(content)
	return extractedDocument{
		inputHash: inputHash, coverage: storage.CoverageSyntax, content: encoded,
		symbolCount: len(content.Symbols), occurrenceCount: len(content.Occurrences),
	}, err
}

// degrade records a document failure on its package: the package is at most partial, so it keeps no
// export shape, and gains a uir/index diagnostic naming the file.
func (extracted *extractedPackage) degrade(failure *DocumentError, note string) {
	if coverageStrength[extracted.coverage] > coverageStrength[storage.CoveragePartial] {
		extracted.coverage, extracted.exportShapeHash = storage.CoveragePartial, nil
	}
	extracted.diagnostics = slices.Concat(extracted.diagnostics, []packageDiagnostic{{
		Path: failure.Path, Rule: RuleIndex, Message: fmt.Sprintf("%v; %s", failure.Err, note),
	}})
}
