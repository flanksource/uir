package indexer

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"

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
	bindings, err := typedBindings(source.file, source.tokens, source.pkg.TypesInfo)
	if err != nil {
		return nil, err
	}
	for _, declared := range fileDeclarations(source.file, source.pkg.TypesInfo) {
		name, extent := source.span(declared.name.Pos(), declared.name.End()), source.span(declared.start, declared.end)
		contentHash, err := declarationContentHash(source.content, extent, bindings)
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
		symbol.ContentHash, symbol.NameBytes, symbol.ExtentBytes = contentHash, name, extent
		symbol.Name, symbol.Extent = source.lines.rangeOf(name), source.lines.rangeOf(extent)
		if projected {
			symbol.Identifier, symbol.ParentKey, symbol.ChildSlot = projection.Identifier, projection.ParentIdentity, projection.ChildSlot
			symbol.Ordinal, symbol.Payload, symbol.SemanticHash, symbol.Field = projection.Ordinal, projection.Payload, projection.SemanticHash, projection.Field
		} else if symbol.Identifier, err = builder.typedIdentifier(*symbol.ID, source.packagePath); err != nil {
			return nil, err
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
	shapes, err := builder.resolver.shapes(declared.object)
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
	embeds, err := builder.embeds(declared.object)
	if err != nil {
		return storage.DocumentSymbol{}, false, err
	}
	row, id := builder.resolver.rows[resolved.ID], resolved.ID
	public, private := layerHashes(shapes)
	return storage.DocumentSymbol{
		ID: &id, Kind: row.Kind, Visibility: row.Visibility, Shape: shapes.Private, PublicShape: shapes.Public, TypeForm: typeFormOf(declared.object),
		PublicHash: public, PrivateHash: private, Implements: implements, Embeds: embeds,
	}, true, nil
}

// typeFormOf is a type declaration's form (alias, struct, interface, or other), "" for any other object.
func typeFormOf(object types.Object) string {
	typeName, ok := object.(*types.TypeName)
	if !ok {
		return ""
	}
	if typeName.IsAlias() {
		return "alias"
	}
	switch typeName.Type().Underlying().(type) {
	case *types.Struct:
		return "struct"
	case *types.Interface:
		return "interface"
	}
	return "other"
}

func (builder *documentBuilder) embeds(object types.Object) ([]string, error) {
	name, ok := object.(*types.TypeName)
	if !ok || name.IsAlias() {
		return nil, nil
	}
	named, ok := name.Type().(*types.Named)
	if !ok {
		return nil, nil
	}
	var embedded []types.Type
	switch underlying := named.Underlying().(type) {
	case *types.Struct:
		for i := 0; i < underlying.NumFields(); i++ {
			if underlying.Field(i).Embedded() {
				embedded = append(embedded, underlying.Field(i).Type())
			}
		}
	case *types.Interface:
		for i := 0; i < underlying.NumEmbeddeds(); i++ {
			embedded = append(embedded, underlying.EmbeddedType(i))
		}
	}
	ids := map[string]bool{}
	for _, target := range embedded {
		target = types.Unalias(target)
		if pointer, ok := target.(*types.Pointer); ok {
			target = types.Unalias(pointer.Elem())
		}
		base, ok := target.(*types.Named)
		if !ok || base.Obj().Pkg() == nil {
			continue
		}
		resolved, err := builder.resolver.resolve(base.Origin().Obj())
		if err != nil {
			return nil, err
		}
		if resolved.ID == "" {
			return nil, fmt.Errorf("embedded type %s has no canonical symbol: %s", base.Obj().Name(), resolved.Note)
		}
		ids[resolved.ID] = true
	}
	return sortedKeys(ids), nil
}

// typedIdentifier is the structured identifier of a declaration the AST extractor does not project:
// variables, constants, interface methods, and fields of literal struct types. The rule is
// storage.DeclarationIdentifier, which diff readers apply to the stored rows.
func (builder *documentBuilder) typedIdentifier(id, packagePath string) (uir.Identifier, error) {
	row := builder.resolver.rows[id]
	var owners []string
	for owner := row.OwnerID; owner != nil; owner = builder.resolver.rows[*owner].OwnerID {
		owners = append([]string{builder.resolver.rows[*owner].Name}, owners...)
	}
	return storage.DeclarationIdentifier(packagePath, row, owners)
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

// typedBindings is syntaxBindings read from the type checker: every identifier whose object is a
// receiver, parameter, result, local variable, or a local constant or type, mapped to the object's
// position. Identifier keys of composite literals stay unresolved, as syntaxBindings leaves them.
func typedBindings(file *ast.File, tokens *token.File, info *types.Info) (localBindings, error) {
	bindings, keys := localBindings{}, map[*ast.Ident]bool{}
	var err error
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CompositeLit:
			for _, element := range node.Elts {
				if keyed, ok := element.(*ast.KeyValueExpr); ok {
					if key, bare := keyed.Key.(*ast.Ident); bare {
						keys[key] = true
					}
				}
			}
		case *ast.TypeSwitchStmt:
			if guard, ok := node.Assign.(*ast.AssignStmt); ok {
				if ident, ok := guard.Lhs[0].(*ast.Ident); ok && ident.Name != "_" {
					bindings[tokens.Offset(ident.Pos())] = tokens.Offset(ident.Pos())
				}
			}
		case *ast.Ident:
			object := info.Uses[node]
			if object == nil {
				object = info.Defs[node]
			}
			if keys[node] || node.Name == "_" || !localObject(object) {
				return true
			}
			if position := object.Pos(); int(position) < tokens.Base() || int(position) > tokens.Base()+tokens.Size() {
				err = fmt.Errorf("local %s at %s is bound outside %s", node.Name, tokens.Position(node.Pos()), tokens.Name())
				return false
			}
			bindings[tokens.Offset(node.Pos())] = tokens.Offset(object.Pos())
		}
		return err == nil
	})
	return bindings, err
}

func localObject(object types.Object) bool {
	switch object := object.(type) {
	case *types.Var:
		kind := object.Kind()
		return kind == types.LocalVar || kind == types.ParamVar || kind == types.RecvVar || kind == types.ResultVar
	case *types.Const, *types.TypeName:
		return object.Pkg() != nil && object.Parent() != object.Pkg().Scope()
	}
	return false
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
