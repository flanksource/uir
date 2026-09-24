package indexer

import (
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"sort"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/flanksource/uir/storage"
)

// declarationFacts are the diff inputs a syntax document records for one declared symbol.
type declarationFacts struct {
	Kind       string
	Visibility string
	Shape      string
	BodyHash   string
	Name       storage.ByteSpan
	Extent     storage.ByteSpan
}

func (indexed *fileIndex) declaration(kind string, exported bool, shape string, name ast.Node, start, end token.Pos) (declarationFacts, error) {
	facts := declarationFacts{
		Kind: kind, Visibility: "internal", Shape: shape,
		Name: indexed.span(name.Pos(), name.End()), Extent: indexed.span(start, end),
	}
	if exported {
		facts.Visibility = "exported"
	}
	bodyHash, err := tokenStreamHash(indexed.content[facts.Extent[0]:facts.Extent[1]])
	if err != nil {
		return declarationFacts{}, fmt.Errorf("hash %s %s: %w", kind, shape, err)
	}
	facts.BodyHash = bodyHash
	return facts, nil
}

func (indexed *fileIndex) span(start, end token.Pos) storage.ByteSpan {
	return storage.ByteSpan{indexed.fileSet.Position(start).Offset, indexed.fileSet.Position(end).Offset}
}

func documentStart(doc *ast.CommentGroup, start token.Pos) token.Pos {
	if doc != nil {
		return doc.Pos()
	}
	return start
}

// tokenStreamHash digests a declaration's tokens with comments and whitespace removed; the
// semicolons the scanner inserts at line ends are layout, so they are dropped too.
func tokenStreamHash(source []byte) (string, error) {
	fileSet := token.NewFileSet()
	var errors scanner.ErrorList
	var tokens scanner.Scanner
	tokens.Init(fileSet.AddFile("", fileSet.Base(), len(source)), source, errors.Add, 0)
	digest := newCanonicalHash(bodyHashVersion)
	for {
		_, kind, literal := tokens.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.SEMICOLON && literal == "\n" {
			continue
		}
		digest.text(kind.String())
		digest.text(literal)
	}
	if err := errors.Err(); err != nil {
		return "", err
	}
	return digest.sum(), nil
}

// lineIndex converts zero-based byte offsets into one-based lines and UTF-16 columns.
type lineIndex struct {
	content []byte
	starts  []int
}

func newLineIndex(content []byte) lineIndex {
	starts := []int{0}
	for offset, value := range content {
		if value == '\n' {
			starts = append(starts, offset+1)
		}
	}
	return lineIndex{content: content, starts: starts}
}

func (index lineIndex) position(offset int) (int, int) {
	line := sort.Search(len(index.starts), func(i int) bool { return index.starts[i] > offset })
	column := 1
	for rest := index.content[index.starts[line-1]:offset]; len(rest) > 0; {
		value, size := utf8.DecodeRune(rest)
		column += max(1, utf16.RuneLen(value))
		rest = rest[size:]
	}
	return line, column
}

func (index lineIndex) rangeOf(span storage.ByteSpan) storage.Range {
	startLine, startColumn := index.position(span[0])
	endLine, endColumn := index.position(span[1])
	return storage.Range{startLine, startColumn, endLine, endColumn}
}

// syntaxDocument renders the AST extraction of one file in the document format, symbols sorted by
// extent start and occurrences by start byte; ties keep extraction order.
func (indexed *fileIndex) syntaxDocument() storage.DocumentContent {
	lines := newLineIndex(indexed.content)
	content := storage.DocumentContent{
		Version: storage.DocumentFormatVersion, PackagePath: indexed.PackagePath,
		Symbols:     make([]storage.DocumentSymbol, 0, len(indexed.Nodes)),
		Occurrences: make([]storage.DocumentOccurrence, 0, len(indexed.Calls)),
		Diagnostics: []storage.DocumentDiagnostic{},
	}
	for _, node := range indexed.Nodes {
		content.Symbols = append(content.Symbols, storage.DocumentSymbol{
			Key: node.Identifier.IdentityKey(), Kind: node.Kind, Visibility: node.Visibility, Shape: node.Shape,
			BodyHash: node.BodyHash, Name: lines.rangeOf(node.Name), NameBytes: node.Name,
			Extent: lines.rangeOf(node.Extent), ExtentBytes: node.Extent, Identifier: node.Identifier,
			ParentKey: node.ParentIdentity, ChildSlot: node.ChildSlot, Ordinal: node.Ordinal,
			Payload: node.Payload, SemanticHash: node.SemanticHash, Field: node.Field,
		})
	}
	for _, call := range indexed.Calls {
		target := call.ToIdentifier
		content.Occurrences = append(content.Occurrences, storage.DocumentOccurrence{
			Role: "call", Range: lines.rangeOf(call.Span), Bytes: call.Span, EnclosingKey: call.FromIdentity,
			Target: &target, Resolvable: call.Resolvable, LocalRoot: call.LocalRoot,
			StatementPath: call.StatementPath, Text: call.Text,
		})
	}
	sort.SliceStable(content.Symbols, func(i, j int) bool {
		left, right := content.Symbols[i].ExtentBytes, content.Symbols[j].ExtentBytes
		return left[0] < right[0] || (left[0] == right[0] && left[1] > right[1])
	})
	sort.SliceStable(content.Occurrences, func(i, j int) bool {
		return content.Occurrences[i].Bytes[0] < content.Occurrences[j].Bytes[0]
	})
	return content
}
