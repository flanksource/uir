package indexer

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strconv"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
)

// occurrenceSites marks the identifiers a file calls through and the ones it assigns to.
type occurrenceSites struct {
	calls  map[*ast.Ident]*ast.CallExpr
	writes map[*ast.Ident]bool
}

func findOccurrenceSites(file *ast.File, info *types.Info) occurrenceSites {
	sites := occurrenceSites{calls: map[*ast.Ident]*ast.CallExpr{}, writes: map[*ast.Ident]bool{}}
	written := func(expression ast.Expr) {
		switch target := ast.Unparen(expression).(type) {
		case *ast.Ident:
			sites.writes[target] = true
		case *ast.SelectorExpr:
			sites.writes[target.Sel] = true
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			if callee := calleeIdent(node.Fun, info); callee != nil {
				sites.calls[callee] = node
			}
		case *ast.AssignStmt:
			for _, target := range node.Lhs {
				written(target)
			}
		case *ast.IncDecStmt:
			written(node.X)
		case *ast.RangeStmt:
			if node.Tok == token.ASSIGN {
				for _, target := range []ast.Expr{node.Key, node.Value} {
					if target != nil {
						written(target)
					}
				}
			}
		}
		return true
	})
	return sites
}

// calleeIdent is the identifier a call expression names: through parentheses, a selector, and the
// index expression of a generic instantiation, but not through an indexed value.
func calleeIdent(expression ast.Expr, info *types.Info) *ast.Ident {
	for {
		switch callee := ast.Unparen(expression).(type) {
		case *ast.Ident:
			return callee
		case *ast.SelectorExpr:
			return callee.Sel
		case *ast.IndexExpr:
			expression = callee.X
		case *ast.IndexListExpr:
			expression = callee.X
		default:
			return nil
		}
		if ident := calleeIdent(expression, info); ident == nil || !isInstantiated(ident, info) {
			return nil
		}
	}
}

func isInstantiated(ident *ast.Ident, info *types.Info) bool {
	_, instantiated := info.Instances[ident]
	return instantiated
}

// occurrences lists every identifier use in the file, sorted by start byte.
func (builder *documentBuilder) occurrences(source typedSource) ([]storage.DocumentOccurrence, error) {
	info := source.pkg.TypesInfo
	sites := findOccurrenceSites(source.file, info)
	occurrences := []storage.DocumentOccurrence{}
	var walkErr error
	ast.Inspect(source.file, func(node ast.Node) bool {
		if walkErr != nil {
			return false
		}
		switch node := node.(type) {
		case *ast.ImportSpec:
			var occurrence storage.DocumentOccurrence
			occurrence, walkErr = builder.importOccurrence(source, node)
			occurrences = append(occurrences, occurrence)
			return false
		case *ast.Ident:
			var found []storage.DocumentOccurrence
			found, walkErr = builder.identOccurrences(source, sites, node)
			occurrences = append(occurrences, found...)
		}
		return true
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.SliceStable(occurrences, func(i, j int) bool { return occurrences[i].Bytes[0] < occurrences[j].Bytes[0] })
	return occurrences, nil
}

func (builder *documentBuilder) importOccurrence(source typedSource, spec *ast.ImportSpec) (storage.DocumentOccurrence, error) {
	span := source.span(spec.Path.Pos(), spec.Path.End())
	occurrence := storage.DocumentOccurrence{Role: "import", Range: source.lines.rangeOf(span), Bytes: span, Note: noteUnresolvedImport}
	path, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return storage.DocumentOccurrence{}, fmt.Errorf("read import %s: %w", spec.Path.Value, err)
	}
	imported, found := source.pkg.Imports[path]
	if !found {
		return occurrence, nil
	}
	resolved, err := builder.resolver.packageSymbol(imported.Types)
	if err != nil {
		return storage.DocumentOccurrence{}, err
	}
	return withSymbol(occurrence, resolved), nil
}

// identOccurrences is the definition an identifier makes and the use it denotes; an embedded
// field's name is both. An identifier the type checker recorded nothing for is unresolved.
func (builder *documentBuilder) identOccurrences(source typedSource, sites occurrenceSites, ident *ast.Ident) ([]storage.DocumentOccurrence, error) {
	if ident.Name == "_" {
		return nil, nil
	}
	info := source.pkg.TypesInfo
	var occurrences []storage.DocumentOccurrence
	defined, isDefined := info.Defs[ident]
	if defined != nil {
		occurrence, err := builder.occurrence(source, ident, "definition", defined, nil)
		if err != nil {
			return nil, err
		}
		occurrences = append(occurrences, occurrence)
	}
	used, isUsed := info.Uses[ident]
	if !isUsed && isDefined {
		return occurrences, nil
	}
	role, call := "reference", sites.calls[ident]
	if isUsed {
		role = useRole(used, call != nil, sites.writes[ident])
	} else if call != nil {
		role = "call"
	}
	if role != "call" {
		call = nil
	}
	occurrence, err := builder.occurrence(source, ident, role, used, call)
	if err != nil {
		return nil, err
	}
	return append(occurrences, occurrence), nil
}

func useRole(object types.Object, called, written bool) string {
	switch object.(type) {
	case *types.TypeName:
		return "type"
	case *types.Func, *types.Builtin, *types.Var:
		if called {
			return "call"
		}
	}
	if written {
		return "write"
	}
	return "reference"
}

// occurrence resolves one identifier use; a call also carries the call locator the explorer and the
// call-graph queries read, with its target spelled as a structured identifier.
func (builder *documentBuilder) occurrence(source typedSource, ident *ast.Ident, role string, object types.Object, call *ast.CallExpr) (storage.DocumentOccurrence, error) {
	span := source.span(ident.Pos(), ident.End())
	occurrence := storage.DocumentOccurrence{Role: role, Range: source.lines.rangeOf(span), Bytes: span, Note: noteUnresolved}
	if object != nil {
		resolved, err := builder.resolver.resolve(object)
		if err != nil {
			return storage.DocumentOccurrence{}, err
		}
		occurrence = withSymbol(occurrence, resolved)
	}
	if call == nil {
		return occurrence, nil
	}
	text, err := formatNode(source.pkg.Fset, call.Fun)
	if err != nil {
		return storage.DocumentOccurrence{}, fmt.Errorf("format call %s: %w", ident.Name, err)
	}
	target := typedCallTarget(object, ident, source.packagePath)
	occurrence.Target, occurrence.Text = &target, text
	occurrence.Resolvable, occurrence.LocalRoot = occurrence.Symbol != nil, target.Package == source.packagePath
	return occurrence, nil
}

func withSymbol(occurrence storage.DocumentOccurrence, resolved resolvedSymbol) storage.DocumentOccurrence {
	if resolved.ID == "" {
		occurrence.Note = resolved.Note
		return occurrence
	}
	id := resolved.ID
	occurrence.Symbol, occurrence.Note = &id, ""
	return occurrence
}

// typedCallTarget spells a call's target the way the AST extractor spells its call locators, with
// the receiver type the type checker proved for method calls.
func typedCallTarget(object types.Object, ident *ast.Ident, packagePath string) uir.Identifier {
	target := uir.Identifier{Package: packagePath, Method: ident.Name, NodeType: uir.NodeTypeMethod}
	if object == nil {
		return target
	}
	if object.Pkg() == nil {
		target.Package = ""
		return target
	}
	switch object := object.(type) {
	case *types.Func:
		target.Package = object.Pkg().Path()
		if receiver := object.Signature().Recv(); receiver != nil {
			target.Type = receiverTypeName(receiver.Type())
		}
	case *types.Builtin:
		target.Package = object.Pkg().Path()
	case *types.Var:
		if object.Parent() == object.Pkg().Scope() {
			target.Package = object.Pkg().Path()
		}
	}
	return target
}

func receiverTypeName(receiver types.Type) string {
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = pointer.Elem()
	}
	if named, ok := types.Unalias(receiver).(*types.Named); ok {
		return named.Obj().Name()
	}
	return ""
}

// assignEnclosing sets each occurrence's innermost enclosing declaration, other than the one it
// defines, and numbers each declaration's calls in byte order.
func assignEnclosing(symbols []storage.DocumentSymbol, occurrences []storage.DocumentOccurrence) {
	calls := map[string]int{}
	for i := range occurrences {
		occurrence := &occurrences[i]
		innermost := -1
		for j, symbol := range symbols {
			if symbol.ExtentBytes[0] > occurrence.Bytes[0] {
				break
			}
			if occurrence.Bytes[1] > symbol.ExtentBytes[1] || (occurrence.Role == "definition" && symbol.NameBytes == occurrence.Bytes) {
				continue
			}
			innermost = j
		}
		enclosingKey := ""
		if innermost >= 0 {
			occurrence.Enclosing, enclosingKey = symbols[innermost].ID, symbols[innermost].Key
		}
		if occurrence.Role == "call" {
			occurrence.EnclosingKey = enclosingKey
			occurrence.StatementPath = fmt.Sprintf("calls/%06d", calls[enclosingKey])
			calls[enclosingKey]++
		}
	}
}
