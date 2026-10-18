package syntax

import (
	"go/ast"
	"go/token"
)

// bindingScope is one block of the syntax binding walk: each name it binds, by its binding's offset.
type bindingScope struct {
	outer *bindingScope
	names map[string]int
}

// bindingWalker resolves, from syntax alone and by Go's block scoping, every identifier that names a
// binding local to its top-level declaration. Selectors, labels, struct field and interface method
// names, and identifier keys of composite literals are never resolved: syntax cannot tell a field key
// from a map key, so the indexer's typed bindings leave those keys alone too, and both walks agree.
type bindingWalker struct {
	tokens   *token.File
	scope    *bindingScope
	bindings Bindings
}

func syntaxBindings(file *ast.File, tokens *token.File) Bindings {
	walker := &bindingWalker{tokens: tokens, bindings: Bindings{}}
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
