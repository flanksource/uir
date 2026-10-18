package indexer

import (
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
)

// declarationFacts are the diff inputs a syntax document records for one declared symbol.
type declarationFacts struct {
	Kind        string
	Visibility  string
	Shape       string
	ContentHash string
	Name        storage.ByteSpan
	Extent      storage.ByteSpan
}

func (indexed *fileIndex) declaration(kind string, exported bool, shape string, name ast.Node, start, end token.Pos) (declarationFacts, error) {
	facts := declarationFacts{
		Kind: kind, Visibility: "internal", Shape: shape,
		Name: indexed.span(name.Pos(), name.End()), Extent: indexed.span(start, end),
	}
	if exported {
		facts.Visibility = "exported"
	}
	contentHash, err := declarationContentHash(indexed.content, facts.Extent, indexed.bindings)
	if err != nil {
		return declarationFacts{}, fmt.Errorf("hash %s %s: %w", kind, shape, err)
	}
	facts.ContentHash = contentHash
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

// localBindings maps the file offset of every identifier that names a binding local to a
// declaration (a receiver, parameter, result, type parameter, or local variable, constant, or type)
// to the file offset of the identifier that binds it.
type localBindings map[int]int

// declarationContentHash digests the tokens of a declaration's extent with comments, whitespace, and
// layout tokens removed, and each identifier bound inside the extent renamed to its binding's
// position, so renaming a local, parameter, or receiver keeps the hash.
func declarationContentHash(content []byte, extent storage.ByteSpan, bindings localBindings) (string, error) {
	scanned, err := scanDeclaration(content[extent[0]:extent[1]])
	if err != nil {
		return "", err
	}
	digest := newCanonicalHash(contentHashVersion)
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
		digest.text(scannedToken.kind.String())
		digest.text(literal)
	}
	return digest.sum(), nil
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

// bindingScope is one block of the syntax binding walk: each name it binds, by its binding's offset.
type bindingScope struct {
	outer *bindingScope
	names map[string]int
}

// bindingWalker resolves, from syntax alone and by Go's block scoping, every identifier that names a
// binding local to its top-level declaration. Selectors, labels, struct field and interface method
// names, and identifier keys of composite literals are never resolved: syntax cannot tell a field key
// from a map key, so typedBindings leaves those keys alone too, and both walks agree.
type bindingWalker struct {
	tokens   *token.File
	scope    *bindingScope
	bindings localBindings
}

func syntaxBindings(file *ast.File, tokens *token.File) localBindings {
	walker := &bindingWalker{tokens: tokens, bindings: localBindings{}}
	for _, declared := range file.Decls {
		ast.Walk(walker, declared)
	}
	return walker.bindings
}

func (walker *bindingWalker) Visit(node ast.Node) ast.Visitor {
	switch node := node.(type) {
	case *ast.Ident:
		walker.resolve(node)
	case *ast.SelectorExpr:
		walker.walk(node.X)
	case *ast.CompositeLit:
		walker.compositeLiteral(node)
	case *ast.StructType:
		walker.fieldTypes(node.Fields)
	case *ast.InterfaceType:
		walker.fieldTypes(node.Methods)
	case *ast.FuncType:
		walker.scoped(func() { walker.signature(nil, node) })
	case *ast.FuncLit:
		walker.scoped(func() { walker.signature(nil, node.Type); walker.statements(node.Body) })
	case *ast.FuncDecl:
		walker.scoped(func() {
			walker.receiverTypeParameters(node.Recv)
			walker.signature(node.Recv, node.Type)
			walker.statements(node.Body)
		})
	case *ast.GenDecl:
		walker.declarations(node)
	case *ast.AssignStmt:
		walker.assign(node)
	case *ast.RangeStmt:
		walker.rangeStatement(node)
	case *ast.TypeSwitchStmt:
		walker.typeSwitch(node)
	case *ast.LabeledStmt:
		ast.Walk(walker, node.Stmt)
	case *ast.BranchStmt:
	case *ast.BlockStmt, *ast.IfStmt, *ast.ForStmt, *ast.SwitchStmt, *ast.CaseClause, *ast.CommClause:
		walker.scoped(func() { ast.Walk(childWalk{walker}, node) })
	default:
		return walker
	}
	return nil
}

// childWalk walks a node's children with the walker, without visiting the node itself again.
type childWalk struct{ walker *bindingWalker }

func (child childWalk) Visit(ast.Node) ast.Visitor { return child.walker }

func (walker *bindingWalker) scoped(walk func()) {
	walker.scope = &bindingScope{outer: walker.scope, names: map[string]int{}}
	walk()
	walker.scope = walker.scope.outer
}

func (walker *bindingWalker) walk(nodes ...ast.Expr) {
	for _, node := range nodes {
		if node != nil {
			ast.Walk(walker, node)
		}
	}
}

func (walker *bindingWalker) statements(block *ast.BlockStmt) {
	if block == nil {
		return
	}
	for _, statement := range block.List {
		ast.Walk(walker, statement)
	}
}

func (walker *bindingWalker) bind(ident *ast.Ident) {
	if ident.Name == "_" {
		return
	}
	offset := walker.tokens.Offset(ident.Pos())
	walker.scope.names[ident.Name] = offset
	walker.bindings[offset] = offset
}

func (walker *bindingWalker) resolve(ident *ast.Ident) {
	for scope := walker.scope; scope != nil && ident.Name != "_"; scope = scope.outer {
		if offset, found := scope.names[ident.Name]; found {
			walker.bindings[walker.tokens.Offset(ident.Pos())] = offset
			return
		}
	}
}

func (walker *bindingWalker) fieldTypes(list *ast.FieldList) {
	if list == nil {
		return
	}
	for _, field := range list.List {
		walker.walk(field.Type)
	}
}

func (walker *bindingWalker) bindNames(list *ast.FieldList) {
	if list == nil {
		return
	}
	for _, field := range list.List {
		for _, name := range field.Names {
			walker.bind(name)
		}
	}
}

// signature binds a function's type parameters before their constraints and its receiver,
// parameters, and results after every type they mention, in the current scope.
func (walker *bindingWalker) signature(receiver *ast.FieldList, function *ast.FuncType) {
	walker.bindNames(function.TypeParams)
	walker.fieldTypes(function.TypeParams)
	for _, list := range []*ast.FieldList{receiver, function.Params, function.Results} {
		walker.fieldTypes(list)
	}
	for _, list := range []*ast.FieldList{receiver, function.Params, function.Results} {
		walker.bindNames(list)
	}
}

// receiverTypeParameters binds the type parameters a method's receiver type declares, as in (l *List[T]).
func (walker *bindingWalker) receiverTypeParameters(receiver *ast.FieldList) {
	if receiver == nil || len(receiver.List) == 0 {
		return
	}
	receiverType := receiver.List[0].Type
	if pointer, ok := receiverType.(*ast.StarExpr); ok {
		receiverType = pointer.X
	}
	var parameters []ast.Expr
	switch receiverType := receiverType.(type) {
	case *ast.IndexExpr:
		parameters = []ast.Expr{receiverType.Index}
	case *ast.IndexListExpr:
		parameters = receiverType.Indices
	}
	for _, parameter := range parameters {
		if ident, ok := parameter.(*ast.Ident); ok {
			walker.bind(ident)
		}
	}
}

func (walker *bindingWalker) compositeLiteral(literal *ast.CompositeLit) {
	walker.walk(literal.Type)
	for _, element := range literal.Elts {
		keyed, ok := element.(*ast.KeyValueExpr)
		if !ok {
			walker.walk(element)
			continue
		}
		if _, bare := keyed.Key.(*ast.Ident); !bare {
			walker.walk(keyed.Key)
		}
		walker.walk(keyed.Value)
	}
}

// declarations binds the names of a declaration inside a function, a value's after its type and
// values and a type's before its definition; package-level names are never local.
func (walker *bindingWalker) declarations(declared *ast.GenDecl) {
	local := walker.scope != nil
	for _, spec := range declared.Specs {
		switch spec := spec.(type) {
		case *ast.ValueSpec:
			walker.walk(spec.Type)
			walker.walk(spec.Values...)
			for _, name := range spec.Names {
				if local {
					walker.bind(name)
				}
			}
		case *ast.TypeSpec:
			if local {
				walker.bind(spec.Name)
			}
			walker.scoped(func() {
				walker.bindNames(spec.TypeParams)
				walker.fieldTypes(spec.TypeParams)
				walker.walk(spec.Type)
			})
		}
	}
}

// assign binds the new names of a short variable declaration; one already bound in the same block is
// redeclared, so it resolves to its existing binding.
func (walker *bindingWalker) assign(assignment *ast.AssignStmt) {
	walker.walk(assignment.Rhs...)
	if assignment.Tok != token.DEFINE {
		walker.walk(assignment.Lhs...)
		return
	}
	for _, target := range assignment.Lhs {
		ident, ok := target.(*ast.Ident)
		if !ok {
			walker.walk(target)
			continue
		}
		if _, redeclared := walker.scope.names[ident.Name]; redeclared {
			walker.resolve(ident)
		} else {
			walker.bind(ident)
		}
	}
}

func (walker *bindingWalker) rangeStatement(statement *ast.RangeStmt) {
	walker.walk(statement.X)
	walker.scoped(func() {
		for _, target := range []ast.Expr{statement.Key, statement.Value} {
			if ident, ok := target.(*ast.Ident); ok && statement.Tok == token.DEFINE {
				walker.bind(ident)
			} else {
				walker.walk(target)
			}
		}
		ast.Walk(walker, statement.Body)
	})
}

// typeSwitch binds the guard of switch v := x.(type) once: every clause's implicit v resolves to it.
func (walker *bindingWalker) typeSwitch(statement *ast.TypeSwitchStmt) {
	walker.scoped(func() {
		if statement.Init != nil {
			ast.Walk(walker, statement.Init)
		}
		if guard, ok := statement.Assign.(*ast.AssignStmt); ok {
			walker.walk(guard.Rhs...)
			for _, target := range guard.Lhs {
				if ident, ok := target.(*ast.Ident); ok {
					walker.bind(ident)
				}
			}
		} else {
			ast.Walk(walker, statement.Assign)
		}
		ast.Walk(walker, statement.Body)
	})
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

// SyntaxDocument extracts one Go file from its syntax alone, outside any index, and fills each symbol's
// public and private layer hashes and shapes from its extracted node, so two syntax documents of a file
// classify a change by layer. It reads no type information, so no symbol has a canonical id.
func SyntaxDocument(pathKey, packagePath string, content []byte) (storage.DocumentContent, error) {
	indexed, err := extractGoFile(pathKey, packagePath, content)
	if err != nil {
		return storage.DocumentContent{}, err
	}
	nodes := make(map[storage.ByteSpan]uir.Node, len(indexed.Nodes))
	for _, node := range indexed.Nodes {
		nodes[node.Name] = node.node
	}
	if len(nodes) != len(indexed.Nodes) {
		return storage.DocumentContent{}, fmt.Errorf("%q declares %d symbols over %d name spans", pathKey, len(indexed.Nodes), len(nodes))
	}
	document := indexed.syntaxDocument()
	for index := range document.Symbols {
		symbol := &document.Symbols[index]
		node := nodes[symbol.NameBytes]
		prints := uir.Fingerprint(node)
		symbol.PublicHash, symbol.PrivateHash = prints.Public, prints.Private
		symbol.Shape, symbol.PublicShape = uir.Shape(node, uir.LayerPrivate), uir.Shape(node, uir.LayerPublic)
	}
	return document, nil
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
			ContentHash: node.ContentHash, Name: lines.rangeOf(node.Name), NameBytes: node.Name,
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
