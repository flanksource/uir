package syntax

import (
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"strconv"

	"github.com/flanksource/uir/internal/canonicalhash"
	"github.com/flanksource/uir/storage"
)

// contentHashVersion is the normalized token stream digest of the content layer.
const contentHashVersion = "uir-content-hash-v2"

// Declaration are the diff inputs a syntax document records for one declared symbol.
type Declaration struct {
	Metrics     *storage.CallableMetrics
	Kind        string
	Visibility  string
	Shape       string
	ContentHash string
	Name        storage.ByteSpan
	Extent      storage.ByteSpan
}

func (indexed *File) declaration(kind string, exported bool, shape string, name ast.Node, start, end token.Pos) (Declaration, error) {
	facts := Declaration{
		Kind: kind, Visibility: "internal", Shape: shape,
		Name: indexed.span(name.Pos(), name.End()), Extent: indexed.span(start, end),
	}
	if exported {
		facts.Visibility = "exported"
	}
	contentHash, err := DeclarationContentHash(indexed.content, facts.Extent, indexed.bindings)
	if err != nil {
		return Declaration{}, fmt.Errorf("hash %s %s: %w", kind, shape, err)
	}
	facts.ContentHash = contentHash
	return facts, nil
}

func (indexed *File) span(start, end token.Pos) storage.ByteSpan {
	return storage.ByteSpan{indexed.fileSet.Position(start).Offset, indexed.fileSet.Position(end).Offset}
}

// DocumentStart is where a declaration's extent opens: its doc comment, or start without one.
func DocumentStart(doc *ast.CommentGroup, start token.Pos) token.Pos {
	if doc != nil {
		return doc.Pos()
	}
	return start
}

// Bindings maps the file offset of every identifier that names a binding local to a declaration (a
// receiver, parameter, result, type parameter, or local variable, constant, or type) to the file
// offset of the identifier that binds it.
type Bindings map[int]int

// DeclarationContentHash digests the tokens of a declaration's extent with comments, whitespace, and
// layout tokens removed, and each identifier bound inside the extent renamed to its binding's
// position, so renaming a local, parameter, or receiver keeps the hash.
func DeclarationContentHash(content []byte, extent storage.ByteSpan, bindings Bindings) (string, error) {
	scanned, err := scanDeclaration(content[extent[0]:extent[1]])
	if err != nil {
		return "", err
	}
	digest := canonicalhash.New(contentHashVersion)
	positions := map[int]int{}
	for index, scannedToken := range scanned {
		if layoutToken(scanned, index) {
			continue
		}
		literal := scannedToken.literal
		binding, local := bindings[extent[0]+scannedToken.offset]
		switch {
		case scannedToken.kind == token.SEMICOLON:
			literal = ";"
		case scannedToken.kind == token.IDENT && local && binding >= extent[0] && binding < extent[1]:
			if _, seen := positions[binding]; !seen {
				positions[binding] = len(positions)
			}
			literal = "$" + strconv.Itoa(positions[binding])
		}
		digest.Text(scannedToken.kind.String())
		digest.Text(literal)
	}
	return digest.Sum(), nil
}

type scannedToken struct {
	kind    token.Token
	literal string
	offset  int
}

func scanDeclaration(source []byte) ([]scannedToken, error) {
	fileSet := token.NewFileSet()
	file := fileSet.AddFile("", fileSet.Base(), len(source))
	var errors scanner.ErrorList
	var tokens scanner.Scanner
	tokens.Init(file, source, errors.Add, 0)
	var scanned []scannedToken
	for {
		position, kind, literal := tokens.Scan()
		if kind == token.EOF {
			break
		}
		scanned = append(scanned, scannedToken{kind: kind, literal: literal, offset: file.Offset(position)})
	}
	return scanned, errors.Err()
}

// layoutToken reports whether token index is there for layout alone: a statement separator before a
// closing bracket, another separator, or the end; a trailing comma before ")" or "}"; or the keyword
// an ungrouped var, const, or type declaration repeats. A trailing comma before "]" stays: it is what
// makes "type A[P *C,] ..." a type parameter list rather than an array length.
func layoutToken(scanned []scannedToken, index int) bool {
	next := token.EOF
	if index+1 < len(scanned) {
		next = scanned[index+1].kind
	}
	switch scanned[index].kind {
	case token.SEMICOLON:
		return next == token.RPAREN || next == token.RBRACE || next == token.SEMICOLON || next == token.EOF
	case token.COMMA:
		return next == token.RPAREN || next == token.RBRACE
	case token.VAR, token.CONST, token.TYPE:
		return index == 0
	}
	return false
}
