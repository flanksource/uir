package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage/symbolhandle"
)

// DocumentFormatVersion versions documents.content. Format 9 gives every symbol of a document its own
// key (each Go func init is keyed by its discriminator); format 8 adds resolved variable types;
// format 6 moved implements into snapshot-level facts (EffectiveImplements).
const DocumentFormatVersion = 9

// uniqueKeysFormat is the first document format whose symbol keys are unique within a document.
const uniqueKeysFormat = 9

// snapshotImplementsFormat is the first document format whose symbols carry no implements entries.
const snapshotImplementsFormat = 6

// Range is [start_line, start_utf16_column, end_line, end_utf16_column], one-based.
type Range [4]int

// ByteSpan is [start, end) in zero-based byte offsets.
type ByteSpan [2]int

// DocumentContent is the JSON object stored in documents.content; see docs/symbol-index-storage.md.
// Excluded states why an excluded document holds no facts.
type DocumentContent struct {
	Relationships []DocumentRelationship `json:"relationships,omitempty"`
	Version       int                    `json:"version"`
	PackagePath   string                 `json:"package_path"`
	Symbols       []DocumentSymbol       `json:"symbols"`
	Occurrences   []DocumentOccurrence   `json:"occurrences"`
	Diagnostics   []DocumentDiagnostic   `json:"diagnostics"`
	Excluded      string                 `json:"excluded,omitempty"`
}

// DocumentSymbol is one declared symbol. Identifier through Field carry the explorer and query
// projection; a typed-only declaration (a variable, constant, interface method, or field of a literal
// struct) has an identifier but no child slot or payload, and the explorer does not list it. Shape is the
// private shape text (the full declared structure) and PublicShape the part a consumer outside the
// symbol's visibility boundary observes; PublicHash, PrivateHash, and ContentHash are the three layered
// hashes (docs/symbol-index-storage.md#hashes-and-identities). Implements is only present in documents
// older than format 6; no reader consults it, since implements facts are snapshot-level now.
type DocumentSymbol struct {
	DeclaredType *string          `json:"declared_type,omitempty"`
	Metrics      *CallableMetrics `json:"metrics,omitempty"`
	Props        map[string]any   `json:"props,omitempty"`
	ID           *string          `json:"id"`
	Key          string           `json:"key"`
	Kind         string           `json:"kind"`
	Visibility   string           `json:"visibility"`
	Shape        string           `json:"shape"`
	PublicShape  string           `json:"public_shape,omitempty"`
	TypeForm     string           `json:"type_form,omitempty"`
	PublicHash   string           `json:"public_hash,omitempty"`
	PrivateHash  string           `json:"private_hash,omitempty"`
	ContentHash  string           `json:"content_hash"`
	Name         Range            `json:"name"`
	NameBytes    ByteSpan         `json:"name_bytes"`
	Extent       Range            `json:"extent"`
	ExtentBytes  ByteSpan         `json:"extent_bytes"`
	Implements   []string         `json:"implements,omitempty"`
	Embeds       []string         `json:"embeds,omitempty"`
	Identifier   uir.Identifier   `json:"identifier"`
	ParentKey    string           `json:"parent_key,omitempty"`
	ChildSlot    string           `json:"child_slot,omitempty"`
	Ordinal      int              `json:"ordinal"`
	Payload      JSON             `json:"payload,omitempty"`
	SemanticHash string           `json:"semantic_hash,omitempty"`
	Field        *Field           `json:"field,omitempty"`
}

// Explorable reports whether the symbol carries the explorer and nodes projection.
func (symbol DocumentSymbol) Explorable() bool { return symbol.ChildSlot != "" }

// Fingerprints are the symbol's three layered hashes as the fingerprints a symbol delta stores. Only a
// declaration with a canonical id has public and private hashes, so any other is an error.
func (symbol DocumentSymbol) Fingerprints() (SymbolFingerprints, error) {
	var fingerprints SymbolFingerprints
	for _, layer := range []struct {
		name, hash string
		into       *int64
	}{{"public", symbol.PublicHash, &fingerprints.Public}, {"private", symbol.PrivateHash, &fingerprints.Private}, {"content", symbol.ContentHash, &fingerprints.Content}} {
		value, err := SymbolFingerprint(layer.hash)
		if err != nil {
			return SymbolFingerprints{}, fmt.Errorf("%s %s fingerprint: %w", symbol.Key, layer.name, err)
		}
		*layer.into = value
	}
	return fingerprints, nil
}

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

var visibilities = map[string]bool{"exported": true, "internal": true}

// decodeOptions are the registries a document is validated against.
type decodeOptions struct {
	kinds symbolhandle.Kinds
}

// DecodeOption configures DecodeDocument.
type DecodeOption func(*decodeOptions)

// WithKinds validates symbol kinds against a database's registry (LoadSymbolKinds), so a document may
// declare that database's custom kinds. Without it only the builtin kinds are valid.
func WithKinds(kinds symbolhandle.Kinds) DecodeOption {
	return func(options *decodeOptions) { options.kinds = kinds }
}

// DecodeDocument decodes and validates a document against the source revision it describes, by the
// rules of its coverage: syntax, typed (indexed or partial), or excluded.
func DecodeDocument(document Document, source SourceRevision, options ...DecodeOption) (DocumentContent, error) {
	decode := decodeOptions{kinds: symbolhandle.Builtins()}
	for _, option := range options {
		option(&decode)
	}
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
	if err := validateDocumentContent(document, content, decode.kinds); err != nil {
		return DocumentContent{}, fmt.Errorf("%s: %w", subject, err)
	}
	return content, nil
}

func validateDocumentContent(document Document, content DocumentContent, kinds symbolhandle.Kinds) error {
	switch {
	case content.Version < 1 || content.Version > DocumentFormatVersion:
		return fmt.Errorf("format version %d, expected 1 through %d", content.Version, DocumentFormatVersion)
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
	declared := make(map[string]int, len(content.Symbols))
	for index, symbol := range content.Symbols {
		if first, found := declared[symbol.Key]; found && content.Version >= uniqueKeysFormat {
			return fmt.Errorf("symbol %d: key is already declared by symbol %d (%s)", index, first, symbol.Key)
		}
		declared[symbol.Key] = index
		if symbol.DeclaredType != nil && (content.Version < 8 || symbol.Kind != "var" || symbol.ID == nil || *symbol.DeclaredType == "" || document.Coverage == CoverageSyntax) {
			return fmt.Errorf("symbol %s: declared_type requires a proven variable, a nonempty canonical type and document format 8 or newer", symbol.Key)
		}
	}
	if content.Version < 7 {
		for _, symbol := range content.Symbols {
			if symbol.Metrics != nil {
				return fmt.Errorf("callable metrics require document format 7 or newer")
			}
		}
	}
	if err := validateRelationships(content); err != nil {
		return err
	}
	switch document.Coverage {
	case CoverageSyntax:
		return validateSyntaxContent(content, kinds)
	case CoverageIndexed, CoveragePartial:
		return validateTypedContent(content, document.Coverage == CoveragePartial, kinds)
	case CoverageExcluded:
		if len(content.Symbols) > 0 || len(content.Occurrences) > 0 || len(content.Relationships) > 0 {
			return errors.New("an excluded document has no symbols or occurrences")
		}
		return nil
	}
	return fmt.Errorf("unknown coverage %q", document.Coverage)
}

func validateSyntaxContent(content DocumentContent, kinds symbolhandle.Kinds) error {
	for index, symbol := range content.Symbols {
		if symbol.ID != nil || symbol.PublicHash != "" || symbol.PrivateHash != "" || symbol.PublicShape != "" ||
			len(symbol.Implements) > 0 || len(symbol.Embeds) > 0 || symbol.TypeForm != "" {
			return fmt.Errorf("symbol %d (%s): a syntax symbol has no id, public_hash, private_hash, public_shape, or implements", index, symbol.Key)
		}
	}
	keys, err := validateSymbolEntries(content.Symbols, kinds)
	if err != nil {
		return err
	}
	return validateSyntaxOccurrences(content.Occurrences, keys)
}

// validateSymbolEntries checks what every symbol entry shares: key, a registered kind, visibility,
// shape, content hash, ranges, and extents sorted by start and overlapping only by nesting. It returns the
// keys.
func validateSymbolEntries(symbols []DocumentSymbol, kinds symbolhandle.Kinds) (map[string]bool, error) {
	keys := make(map[string]bool, len(symbols))
	var open []ByteSpan
	for index, symbol := range symbols {
		subject := fmt.Sprintf("symbol %d (%s)", index, symbol.Key)
		if _, err := kinds.Lookup(symbol.Kind); err != nil {
			return nil, fmt.Errorf("%s: %w", subject, err)
		}
		if symbol.Metrics != nil {
			kind, err := kinds.Lookup(symbol.Kind)
			if err != nil {
				return nil, err
			}
			if err := symbol.Metrics.Validate(kind.Category); err != nil {
				return nil, fmt.Errorf("%s: %w", subject, err)
			}
		}
		switch {
		case symbol.Key == "" || symbol.Key != symbol.Identifier.IdentityKey():
			return nil, fmt.Errorf("%s: key does not match identifier key %q", subject, symbol.Identifier.IdentityKey())
		case !visibilities[symbol.Visibility]:
			return nil, fmt.Errorf("%s: invalid visibility %q", subject, symbol.Visibility)
		case len(symbol.ContentHash) != 64 || symbol.Shape == "":
			return nil, fmt.Errorf("%s: shape and a 64-character content_hash are required", subject)
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
