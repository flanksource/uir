package indexer

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"golang.org/x/tools/go/packages"
)

// documentBuilder renders typed documents from one load, sharing the resolver across the module.
type documentBuilder struct {
	resolver   *symbolResolver
	interfaces map[string][]*types.TypeName
}

func newDocumentBuilder(resolver *symbolResolver) *documentBuilder {
	return &documentBuilder{resolver: resolver, interfaces: map[string][]*types.TypeName{}}
}

// typedSource is one root file with its syntax tree, token file, and bytes.
type typedSource struct {
	pkg         *packages.Package
	file        *ast.File
	tokens      *token.File
	content     []byte
	lines       lineIndex
	packagePath string
}

func (source typedSource) span(start, end token.Pos) storage.ByteSpan {
	return storage.ByteSpan{source.tokens.Offset(start), source.tokens.Offset(end)}
}

// declaration is one symbol a file declares: its object, name identifier, and extent.
type declaration struct {
	object     types.Object
	name       *ast.Ident
	start, end token.Pos
}

// typedDocument renders the typed facts of one file. The AST extraction of the same bytes supplies
// the explorer projection of every declaration it knows, matched by name span. In a partial package
// a declaration whose type is invalid keeps only its syntax facts and a null id.
func (builder *documentBuilder) typedDocument(file discoveredFile, typed typedFile, diagnostics []storage.DocumentDiagnostic, partial bool) (storage.DocumentContent, error) {
	syntax, err := extractGoFile(file.PathKey, file.PackagePath, file.Content)
	if err != nil {
		return storage.DocumentContent{}, err
	}
	tokens := typed.pkg.Fset.File(typed.file.Pos())
	if tokens.Size() != len(file.Content) {
		return storage.DocumentContent{}, fmt.Errorf("typed syntax of %q covers %d bytes, the file has %d", file.PathKey, tokens.Size(), len(file.Content))
	}
	source := typedSource{
		pkg: typed.pkg, file: typed.file, tokens: tokens, content: file.Content,
		lines: newLineIndex(file.Content), packagePath: syntax.PackagePath,
	}
	projections := make(map[storage.ByteSpan]nodeSpec, len(syntax.Nodes))
	for _, node := range syntax.Nodes {
		projections[node.Name] = node
	}
	symbols, err := builder.symbols(source, projections, partial)
	if err != nil {
		return storage.DocumentContent{}, fmt.Errorf("describe symbols of %q: %w", file.PathKey, err)
	}
	occurrences, err := builder.occurrences(source)
	if err != nil {
		return storage.DocumentContent{}, fmt.Errorf("describe occurrences in %q: %w", file.PathKey, err)
	}
	assignEnclosing(symbols, occurrences)
	return storage.DocumentContent{
		Version: storage.DocumentFormatVersion, PackagePath: syntax.PackagePath,
		Symbols: symbols, Occurrences: occurrences, Diagnostics: diagnostics,
	}, nil
}

func (builder *documentBuilder) symbols(source typedSource, projections map[storage.ByteSpan]nodeSpec, partial bool) ([]storage.DocumentSymbol, error) {
	symbols := []storage.DocumentSymbol{}
	for _, declared := range fileDeclarations(source.file, source.pkg.TypesInfo) {
		name, extent := source.span(declared.name.Pos(), declared.name.End()), source.span(declared.start, declared.end)
		bodyHash, err := tokenStreamHash(source.content[extent[0]:extent[1]])
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", declared.name.Name, err)
		}
		projection, projected := projections[name]
		symbol, proven, err := builder.typedSymbol(declared, source.pkg)
		if err != nil {
			return nil, err
		}
		if !proven {
			if !partial {
				return nil, fmt.Errorf("%s has no proven identity or shape in a package without errors", declared.name.Name)
			}
			if !projected {
				continue
			}
			symbol = storage.DocumentSymbol{Kind: projection.Kind, Visibility: projection.Visibility, Shape: projection.Shape}
		}
		symbol.BodyHash, symbol.NameBytes, symbol.ExtentBytes = bodyHash, name, extent
		symbol.Name, symbol.Extent = source.lines.rangeOf(name), source.lines.rangeOf(extent)
		if projected {
			symbol.Identifier, symbol.ParentKey, symbol.ChildSlot = projection.Identifier, projection.ParentIdentity, projection.ChildSlot
			symbol.Ordinal, symbol.Payload, symbol.SemanticHash, symbol.Field = projection.Ordinal, projection.Payload, projection.SemanticHash, projection.Field
		} else {
			symbol.Identifier = builder.typedIdentifier(*symbol.ID, source.packagePath)
		}
		symbol.Key = symbol.Identifier.IdentityKey()
		symbols = append(symbols, symbol)
	}
	sort.SliceStable(symbols, func(i, j int) bool {
		left, right := symbols[i].ExtentBytes, symbols[j].ExtentBytes
		return left[0] < right[0] || (left[0] == right[0] && left[1] > right[1])
	})
	return symbols, nil
}

// typedSymbol is a declaration's canonical facts, or proven=false when its identity or shape
// involves an invalid type.
func (builder *documentBuilder) typedSymbol(declared declaration, variant *packages.Package) (storage.DocumentSymbol, bool, error) {
	resolved, err := builder.resolver.resolve(declared.object)
	if err != nil {
		return storage.DocumentSymbol{}, false, err
	}
	if resolved.ID == "" {
		if resolved.Note == noteUnproven {
			return storage.DocumentSymbol{}, false, nil
		}
		return storage.DocumentSymbol{}, false, fmt.Errorf("declared %s has no canonical symbol: %s", declared.name.Name, resolved.Note)
	}
	shape, err := builder.resolver.shape(declared.object)
	if errors.Is(err, errUnprovenType) {
		return storage.DocumentSymbol{}, false, nil
	}
	if err != nil {
		return storage.DocumentSymbol{}, false, err
	}
	implements, err := builder.implements(declared.object, variant)
	if err != nil {
		return storage.DocumentSymbol{}, false, err
	}
	row, id := builder.resolver.rows[resolved.ID], resolved.ID
	return storage.DocumentSymbol{
		ID: &id, Kind: row.Kind, Visibility: row.Visibility, Shape: shape, ShapeHash: shapeHash(shape), Implements: implements,
	}, true, nil
}

// typedIdentifier is the structured identifier of a declaration the AST extractor does not project:
// variables, constants, interface methods, and fields of literal struct types.
func (builder *documentBuilder) typedIdentifier(id, packagePath string) uir.Identifier {
	row := builder.resolver.rows[id]
	var owners []string
	for owner := row.OwnerID; owner != nil; owner = builder.resolver.rows[*owner].OwnerID {
		owners = append([]string{builder.resolver.rows[*owner].Name}, owners...)
	}
	identifier := uir.Identifier{Package: packagePath}
	switch row.Kind {
	case "method":
		identifier.Type, identifier.Method, identifier.NodeType = strings.Join(owners, "."), row.Name, uir.NodeTypeMethod
	case "field":
		identifier.Type, identifier.Field, identifier.NodeType = owners[0], strings.Join(append(owners[1:], row.Name), "."), uir.NodeTypeField
	case "type":
		identifier.Type, identifier.NodeType = row.Name, uir.NodeTypeType
	case "func":
		identifier.Method, identifier.NodeType = row.Name, uir.NodeTypeMethod
	default:
		identifier.Field, identifier.NodeType = row.Name, uir.NodeTypePackageVariable
	}
	return identifier
}

// implements lists the interfaces, among the variant and its transitive imports, that a declared
// non-interface, non-generic named type satisfies by value or by pointer.
func (builder *documentBuilder) implements(object types.Object, variant *packages.Package) ([]string, error) {
	typeName, ok := object.(*types.TypeName)
	if !ok || typeName.IsAlias() {
		return nil, nil
	}
	named, ok := typeName.Type().(*types.Named)
	if !ok || named.TypeParams().Len() > 0 || types.IsInterface(named) {
		return nil, nil
	}
	pointer := types.NewPointer(named)
	found := map[string]bool{}
	for _, candidate := range builder.candidateInterfaces(variant) {
		iface := candidate.Type().Underlying().(*types.Interface)
		if !types.Implements(named, iface) && !types.Implements(pointer, iface) {
			continue
		}
		resolved, err := builder.resolver.resolve(candidate)
		if err != nil {
			return nil, err
		}
		if resolved.ID != "" {
			found[resolved.ID] = true
		}
	}
	if len(found) == 0 {
		return nil, nil
	}
	return sortedKeys(found), nil
}

// candidateInterfaces are the non-empty, non-generic, method-set interfaces declared at package
// level in the variant and its transitive imports. Restricting them to the import closure keeps a
// document a function of its package input hash.
func (builder *documentBuilder) candidateInterfaces(variant *packages.Package) []*types.TypeName {
	if candidates, found := builder.interfaces[variant.ID]; found {
		return candidates
	}
	var candidates []*types.TypeName
	visited := map[string]bool{}
	var visit func(*packages.Package)
	visit = func(pkg *packages.Package) {
		if visited[pkg.ID] {
			return
		}
		visited[pkg.ID] = true
		for _, path := range sortedKeys(pkg.Imports) {
			visit(pkg.Imports[path])
		}
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			typeName, ok := scope.Lookup(name).(*types.TypeName)
			if !ok || typeName.IsAlias() {
				continue
			}
			named, ok := typeName.Type().(*types.Named)
			if !ok || named.TypeParams().Len() > 0 {
				continue
			}
			if iface, ok := named.Underlying().(*types.Interface); ok && iface.IsMethodSet() && iface.NumMethods() > 0 {
				candidates = append(candidates, typeName)
			}
		}
	}
	visit(variant)
	builder.interfaces[variant.ID] = candidates
	return candidates
}

// fileDeclarations lists a file's package-level declarations and the members of their literal
// struct and interface types, skipping blank names.
func fileDeclarations(file *ast.File, info *types.Info) []declaration {
	var declarations []declaration
	add := func(name *ast.Ident, start, end token.Pos) {
		if object := info.Defs[name]; object != nil && name.Name != "_" {
			declarations = append(declarations, declaration{object: object, name: name, start: start, end: end})
		}
	}
	for _, declared := range file.Decls {
		switch declared := declared.(type) {
		case *ast.FuncDecl:
			add(declared.Name, documentStart(declared.Doc, declared.Pos()), declared.End())
		case *ast.GenDecl:
			for _, spec := range declared.Specs {
				start, end := specExtent(declared, spec)
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					add(spec.Name, start, end)
					addMembers(add, spec.Type)
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						add(name, start, end)
					}
					addMembers(add, spec.Type)
				}
			}
		}
	}
	return declarations
}

func specExtent(declared *ast.GenDecl, spec ast.Spec) (token.Pos, token.Pos) {
	if !declared.Lparen.IsValid() {
		return documentStart(declared.Doc, declared.Pos()), declared.End()
	}
	switch spec := spec.(type) {
	case *ast.TypeSpec:
		return documentStart(spec.Doc, spec.Pos()), spec.End()
	case *ast.ValueSpec:
		return documentStart(spec.Doc, spec.Pos()), spec.End()
	}
	return spec.Pos(), spec.End()
}

func addMembers(add func(*ast.Ident, token.Pos, token.Pos), expression ast.Expr) {
	switch literal := expression.(type) {
	case *ast.StructType:
		for _, field := range literal.Fields.List {
			start, end := documentStart(field.Doc, field.Pos()), field.End()
			names := field.Names
			if len(names) == 0 {
				if embedded := typeNameIdent(field.Type); embedded != nil {
					names = []*ast.Ident{embedded}
				}
			}
			for _, name := range names {
				add(name, start, end)
			}
			addMembers(add, field.Type)
		}
	case *ast.InterfaceType:
		for _, method := range literal.Methods.List {
			for _, name := range method.Names {
				add(name, documentStart(method.Doc, method.Pos()), method.End())
			}
		}
	}
}
