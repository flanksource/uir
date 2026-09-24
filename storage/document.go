package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flanksource/uir"
)

// DocumentFormatVersion is the version of DocumentContent written to documents.content.
const DocumentFormatVersion = 2

// Range is [start_line, start_utf16_column, end_line, end_utf16_column], one-based.
type Range [4]int

// ByteSpan is [start, end) in zero-based byte offsets.
type ByteSpan [2]int

// DocumentContent is the JSON object stored in documents.content; see docs/symbol-index-storage.md.
// Excluded states why an excluded document holds no facts.
type DocumentContent struct {
	Version     int                  `json:"version"`
	PackagePath string               `json:"package_path"`
	Symbols     []DocumentSymbol     `json:"symbols"`
	Occurrences []DocumentOccurrence `json:"occurrences"`
	Diagnostics []DocumentDiagnostic `json:"diagnostics"`
	Excluded    string               `json:"excluded,omitempty"`
}

// DocumentSymbol is one declared symbol. Identifier through Field carry the explorer and query
// projection; a typed-only declaration (a variable, constant, interface method, or field of a literal
// struct) has an identifier but no child slot or payload, and the explorer does not list it.
type DocumentSymbol struct {
	ID           *string        `json:"id"`
	Key          string         `json:"key"`
	Kind         string         `json:"kind"`
	Visibility   string         `json:"visibility"`
	Shape        string         `json:"shape"`
	TypeForm     string         `json:"type_form,omitempty"`
	ShapeHash    string         `json:"shape_hash,omitempty"`
	BodyHash     string         `json:"body_hash"`
	Name         Range          `json:"name"`
	NameBytes    ByteSpan       `json:"name_bytes"`
	Extent       Range          `json:"extent"`
	ExtentBytes  ByteSpan       `json:"extent_bytes"`
	Implements   []string       `json:"implements,omitempty"`
	Identifier   uir.Identifier `json:"identifier"`
	ParentKey    string         `json:"parent_key,omitempty"`
	ChildSlot    string         `json:"child_slot,omitempty"`
	Ordinal      int            `json:"ordinal"`
	Payload      JSON           `json:"payload,omitempty"`
	SemanticHash string         `json:"semantic_hash,omitempty"`
	Field        *Field         `json:"field,omitempty"`
}

// Explorable reports whether the symbol carries the explorer and nodes projection.
func (symbol DocumentSymbol) Explorable() bool { return symbol.ChildSlot != "" }

// IsCall reports whether the occurrence is a call carrying the call locator.
func (occurrence DocumentOccurrence) IsCall() bool {
	return occurrence.Role == "call" && occurrence.Target != nil
}

// DocumentOccurrence is one identifier use. Target through Text are the syntax extractor's call locator.
type DocumentOccurrence struct {
	Symbol        *string         `json:"symbol"`
	Role          string          `json:"role"`
	Range         Range           `json:"range"`
	Bytes         ByteSpan        `json:"bytes"`
	Enclosing     *string         `json:"enclosing"`
	EnclosingKey  string          `json:"enclosing_key,omitempty"`
	Note          string          `json:"note,omitempty"`
	Target        *uir.Identifier `json:"target,omitempty"`
	Resolvable    bool            `json:"resolvable,omitempty"`
	LocalRoot     bool            `json:"local_root,omitempty"`
	StatementPath string          `json:"statement_path,omitempty"`
	Text          string          `json:"text,omitempty"`
}

type DocumentDiagnostic struct {
	Range   Range  `json:"range"`
	Message string `json:"message"`
}

var (
	documentSymbolKinds = map[string]bool{"package": true, "type": true, "func": true, "method": true, "field": true, "var": true, "const": true, "builtin": true}
	visibilities        = map[string]bool{"exported": true, "internal": true}
)

// DecodeDocument decodes and validates a document against the source revision it describes, by the
// rules of its coverage: syntax, typed (indexed or partial), or excluded.
func DecodeDocument(document Document, source SourceRevision) (DocumentContent, error) {
	subject := fmt.Sprintf("document %s for %q", document.ID, document.PathKey)
	switch {
	case document.RootID != source.RootID || document.PathKey != source.PathKey || document.SourceRevisionID != source.ID:
		return DocumentContent{}, fmt.Errorf("%s does not belong to source revision %s of %q", subject, source.ID, source.PathKey)
	case document.PackagePath != source.PackagePath:
		return DocumentContent{}, fmt.Errorf("%s has package %q, source revision has %q", subject, document.PackagePath, source.PackagePath)
	case len(document.InputHash) != 64:
		return DocumentContent{}, fmt.Errorf("%s has input hash of length %d, expected 64", subject, len(document.InputHash))
	}
	decoder := json.NewDecoder(bytes.NewReader(document.Content))
	decoder.DisallowUnknownFields()
	var content DocumentContent
	if err := decoder.Decode(&content); err != nil {
		return DocumentContent{}, fmt.Errorf("decode %s: %w", subject, err)
	}
	if err := validateDocumentContent(document, content); err != nil {
		return DocumentContent{}, fmt.Errorf("%s: %w", subject, err)
	}
	return content, nil
}

func validateDocumentContent(document Document, content DocumentContent) error {
	switch {
	case content.Version != 1 && content.Version != DocumentFormatVersion:
		return fmt.Errorf("format version %d, expected 1 or %d", content.Version, DocumentFormatVersion)
	case content.PackagePath != document.PackagePath && content.PackagePath != document.PackagePath+"_test":
		return fmt.Errorf("content package %q does not match package %q", content.PackagePath, document.PackagePath)
	case content.Symbols == nil || content.Occurrences == nil || content.Diagnostics == nil:
		return errors.New("symbols, occurrences, and diagnostics must all be present")
	case len(content.Symbols) != document.SymbolCount:
		return fmt.Errorf("symbol_count %d, content has %d symbols", document.SymbolCount, len(content.Symbols))
	case len(content.Occurrences) != document.OccurrenceCount:
		return fmt.Errorf("occurrence_count %d, content has %d occurrences", document.OccurrenceCount, len(content.Occurrences))
	case (document.Coverage == CoverageExcluded) != (content.Excluded != ""):
		return fmt.Errorf("%q coverage with excluded reason %q", document.Coverage, content.Excluded)
	}
	for index, diagnostic := range content.Diagnostics {
		if err := validRange(diagnostic.Range); err != nil {
			return fmt.Errorf("diagnostic %d: %w", index, err)
		}
	}
	switch document.Coverage {
	case CoverageSyntax:
		return validateSyntaxContent(content)
	case CoverageIndexed, CoveragePartial:
		return validateTypedContent(content, document.Coverage == CoveragePartial)
	case CoverageExcluded:
		if len(content.Symbols) > 0 || len(content.Occurrences) > 0 {
			return errors.New("an excluded document has no symbols or occurrences")
		}
		return nil
	}
	return fmt.Errorf("unknown coverage %q", document.Coverage)
}

func validateSyntaxContent(content DocumentContent) error {
	for index, symbol := range content.Symbols {
		if symbol.ID != nil || symbol.ShapeHash != "" || len(symbol.Implements) > 0 || symbol.TypeForm != "" {
			return fmt.Errorf("symbol %d (%s): a syntax symbol has no id, shape_hash, or implements", index, symbol.Key)
		}
	}
	keys, err := validateSymbolEntries(content.Symbols)
	if err != nil {
		return err
	}
	return validateSyntaxOccurrences(content.Occurrences, keys)
}

// validateSymbolEntries checks what every symbol entry shares: key, kind, visibility, shape, body
// hash, ranges, and extents sorted by start and overlapping only by nesting. It returns the keys.
func validateSymbolEntries(symbols []DocumentSymbol) (map[string]bool, error) {
	keys := make(map[string]bool, len(symbols))
	var open []ByteSpan
	for index, symbol := range symbols {
		subject := fmt.Sprintf("symbol %d (%s)", index, symbol.Key)
		switch {
		case symbol.Key == "" || symbol.Key != symbol.Identifier.IdentityKey():
			return nil, fmt.Errorf("%s: key does not match identifier key %q", subject, symbol.Identifier.IdentityKey())
		case !documentSymbolKinds[symbol.Kind] || !visibilities[symbol.Visibility]:
			return nil, fmt.Errorf("%s: invalid kind %q or visibility %q", subject, symbol.Kind, symbol.Visibility)
		case len(symbol.BodyHash) != 64 || symbol.Shape == "":
			return nil, fmt.Errorf("%s: shape and a 64-character body_hash are required", subject)
		}
		for _, err := range []error{validRange(symbol.Name), validRange(symbol.Extent), validSpan(symbol.NameBytes), validSpan(symbol.ExtentBytes)} {
			if err != nil {
				return nil, fmt.Errorf("%s: %w", subject, err)
			}
		}
		if symbol.NameBytes[0] < symbol.ExtentBytes[0] || symbol.NameBytes[1] > symbol.ExtentBytes[1] {
			return nil, fmt.Errorf("%s: name %v lies outside extent %v", subject, symbol.NameBytes, symbol.ExtentBytes)
		}
		if index > 0 && symbol.ExtentBytes[0] < symbols[index-1].ExtentBytes[0] {
			return nil, fmt.Errorf("%s: symbols are not sorted by extent start", subject)
		}
		for len(open) > 0 && open[len(open)-1][1] <= symbol.ExtentBytes[0] {
			open = open[:len(open)-1]
		}
		if len(open) > 0 && symbol.ExtentBytes[1] > open[len(open)-1][1] {
			return nil, fmt.Errorf("%s: extent %v overlaps %v other than by nesting", subject, symbol.ExtentBytes, open[len(open)-1])
		}
		open = append(open, symbol.ExtentBytes)
		keys[symbol.Key] = true
	}
	return keys, nil
}

func validateSyntaxOccurrences(occurrences []DocumentOccurrence, keys map[string]bool) error {
	for index, occurrence := range occurrences {
		subject := fmt.Sprintf("occurrence %d", index)
		switch {
		case occurrence.Symbol != nil || occurrence.Enclosing != nil:
			return fmt.Errorf("%s: a syntax occurrence has no symbol or enclosing id", subject)
		case occurrence.Role != "call" || occurrence.Target == nil:
			return fmt.Errorf("%s: a syntax occurrence is a call with a target, got role %q", subject, occurrence.Role)
		case occurrence.EnclosingKey != "" && !keys[occurrence.EnclosingKey]:
			return fmt.Errorf("%s: enclosing key %q is not declared in the document", subject, occurrence.EnclosingKey)
		case index > 0 && occurrence.Bytes[0] < occurrences[index-1].Bytes[0]:
			return fmt.Errorf("%s: occurrences are not sorted by start byte", subject)
		}
		if err := errors.Join(validRange(occurrence.Range), validSpan(occurrence.Bytes)); err != nil {
			return fmt.Errorf("%s: %w", subject, err)
		}
	}
	return nil
}

func validRange(value Range) error {
	if value[0] < 1 || value[1] < 1 || value[2] < value[0] || (value[2] == value[0] && value[3] <= value[1]) {
		return fmt.Errorf("range %v does not end after it starts", value)
	}
	return nil
}

func validSpan(value ByteSpan) error {
	if value[0] < 0 || value[1] <= value[0] {
		return fmt.Errorf("byte span %v does not end after it starts", value)
	}
	return nil
}
