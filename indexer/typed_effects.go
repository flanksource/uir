package indexer

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"github.com/flanksource/uir/effects"
	"github.com/flanksource/uir/indexer/syntax"
	"github.com/flanksource/uir/storage"
	"golang.org/x/tools/go/types/typeutil"
)

// effectSite is one place in a file that has an effect, with the source text that shows it, or that
// calls a method implicitly: callee is then the method's symbol id and effect is empty.
type effectSite struct {
	effect effects.Effect
	callee string
	start  int
	text   string
}

// recordEffects sets on each symbol the effects its own declaration has that no call or access
// occurrence shows: starting a goroutine, a channel operation, ranging over a map, a call the type
// checker cannot pin to one function, such as a call through an interface, a function value or a type
// parameter, and having no body. It also sets the methods a library function the effects catalog
// knows may call back on the values the declaration passes it (ImplicitCalls). A site belongs to the
// innermost declaration enclosing it; each symbol keeps the first site of each effect. The effects of
// what a declaration calls, reads and writes are the query's to combine.
func (builder *documentBuilder) recordEffects(source typedSource, symbols []storage.DocumentSymbol) error {
	sites, err := builder.findEffectSites(source)
	if err != nil {
		return err
	}
	for _, site := range sites {
		innermost := -1
		for j, symbol := range symbols {
			if symbol.ExtentBytes[0] > site.start {
				break
			}
			if site.start < symbol.ExtentBytes[1] {
				innermost = j
			}
		}
		if innermost < 0 {
			continue
		}
		symbol := &symbols[innermost]
		if site.callee != "" {
			if !slices.Contains(symbol.ImplicitCalls, site.callee) {
				symbol.ImplicitCalls = append(symbol.ImplicitCalls, site.callee)
				slices.Sort(symbol.ImplicitCalls)
			}
			continue
		}
		if symbol.Effects == nil {
			symbol.Effects = map[effects.Effect]string{}
		}
		if _, found := symbol.Effects[site.effect]; !found {
			symbol.Effects[site.effect] = site.text
		}
	}
	return nil
}

func (builder *documentBuilder) findEffectSites(source typedSource) ([]effectSite, error) {
	info := source.pkg.TypesInfo
	var sites []effectSite
	var walkErr error
	format := func(node ast.Node) string {
		text, err := syntax.FormatNode(source.pkg.Fset, node)
		if err != nil && walkErr == nil {
			walkErr = fmt.Errorf("format effect site: %w", err)
		}
		return text
	}
	add := func(effect effects.Effect, at ast.Node, text string) {
		sites = append(sites, effectSite{effect: effect, start: source.tokens.Offset(at.Pos()), text: text})
	}
	ast.Inspect(source.file, func(node ast.Node) bool {
		if walkErr != nil {
			return false
		}
		switch node := node.(type) {
		case *ast.FuncDecl:
			if node.Body == nil {
				add(effects.Unknown, node.Name, "func "+node.Name.Name+" has no body")
			}
		case *ast.GoStmt:
			add(effects.Goroutine, node, "go "+format(node.Call.Fun))
		case *ast.SendStmt:
			add(effects.Channel, node, format(node))
		case *ast.UnaryExpr:
			if node.Op == token.ARROW {
				add(effects.Channel, node, format(node))
			}
		case *ast.SelectStmt:
			add(effects.Channel, node, "select")
		case *ast.RangeStmt:
			ranged := info.TypeOf(node.X)
			if ranged == nil {
				break
			}
			switch ranged.Underlying().(type) {
			case *types.Chan:
				add(effects.Channel, node, "range "+format(node.X))
			case *types.Map:
				add(effects.MapOrder, node, "range "+format(node.X))
			}
		case *ast.CallExpr:
			if effect, dynamic := callEffect(node, info); dynamic {
				add(effect, node, format(node.Fun))
			}
			callbacks, err := builder.libraryCallbacks(node, info, format)
			if err != nil {
				walkErr = err
				return false
			}
			for _, callback := range callbacks {
				callback.start = source.tokens.Offset(node.Pos())
				sites = append(sites, callback)
			}
		}
		return true
	})
	return sites, walkErr
}

// callbackMethods are the methods a library package calls on the values passed to it as interfaces;
// a reflective package also calls them on the exported fields, elements and keys it walks.
var callbackMethods = map[string]struct {
	names      []string
	reflective bool
}{
	"fmt":            {[]string{"Error", "Format", "GoString", "Scan", "String"}, true},
	"encoding/json":  {[]string{"MarshalJSON", "MarshalText", "UnmarshalJSON", "UnmarshalText"}, true},
	"encoding/xml":   {[]string{"MarshalText", "MarshalXML", "MarshalXMLAttr", "UnmarshalText", "UnmarshalXML", "UnmarshalXMLAttr"}, true},
	"sort":           {[]string{"Len", "Less", "Swap"}, false},
	"container/heap": {[]string{"Len", "Less", "Pop", "Push", "Swap"}, false},
	"errors":         {[]string{"As", "Is", "Unwrap"}, false},
}

// libraryCallbacks are what a call of a library function the effects catalog knows may run of the
// caller's code, which the catalog cannot vouch for: a site calling each method the library calls back
// on an argument it takes as an interface, and Unknown for an argument whose methods it cannot know (a
// value of interface or type parameter type) or a function value that is not a literal or a named
// function or concrete method.
func (builder *documentBuilder) libraryCallbacks(call *ast.CallExpr, info *types.Info, format func(ast.Node) string) ([]effectSite, error) {
	callee := typeutil.StaticCallee(info, call)
	if callee == nil || callee.Pkg() == nil {
		return nil, nil
	}
	owner := ""
	if receiver := callee.Signature().Recv(); receiver != nil {
		owner = receiverTypeName(receiver.Type())
	}
	if _, known := effects.Lookup(callee.Pkg().Path(), owner, callee.Name()); !known {
		return nil, nil
	}
	signature, ok := info.TypeOf(call.Fun).(*types.Signature)
	if !ok {
		return nil, nil
	}
	methods := callbackMethods[callee.Pkg().Path()]
	var sites []effectSite
	unknown := func(argument ast.Expr) {
		sites = append(sites, effectSite{effect: effects.Unknown, text: format(call.Fun) + " calls back into " + format(argument)})
	}
	for i, argument := range call.Args {
		parameter := parameterType(signature, i, call.Ellipsis.IsValid())
		argumentType := info.TypeOf(argument)
		if parameter == nil || argumentType == nil {
			continue
		}
		if _, function := parameter.Underlying().(*types.Signature); function {
			if !staticFunction(argument, info) {
				unknown(argument)
			}
			continue
		}
		if !types.IsInterface(parameter) || len(methods.names) == 0 {
			continue
		}
		walk := callbackWalk{names: methods.names, reflective: methods.reflective, seen: &typeutil.Map{}}
		walk.visit(argumentType)
		if walk.dynamic {
			unknown(argument)
		}
		for _, method := range walk.methods {
			resolved, err := builder.resolver.resolve(method)
			if err != nil {
				return nil, err
			}
			if resolved.ID == "" {
				unknown(argument)
				continue
			}
			sites = append(sites, effectSite{callee: resolved.ID})
		}
	}
	return sites, nil
}

// parameterType is the type the i-th argument of a call is passed as: the element type of a variadic
// parameter unless the call spreads a slice into it.
func parameterType(signature *types.Signature, i int, spread bool) types.Type {
	parameters := signature.Params()
	if parameters.Len() == 0 {
		return nil
	}
	if signature.Variadic() && i >= parameters.Len()-1 {
		last := parameters.At(parameters.Len() - 1).Type()
		if spread {
			return last
		}
		if slice, ok := last.(*types.Slice); ok {
			return slice.Elem()
		}
		return last
	}
	if i >= parameters.Len() {
		return nil
	}
	return parameters.At(i).Type()
}

// staticFunction reports whether a function-valued expression names what runs: a function literal,
// whose body the enclosing declaration's effects cover, or a named function or method of a concrete
// type, which the declaration references.
func staticFunction(expression ast.Expr, info *types.Info) bool {
	switch expression := ast.Unparen(expression).(type) {
	case *ast.FuncLit:
		return true
	case *ast.Ident:
		_, function := info.Uses[expression].(*types.Func)
		return function
	case *ast.SelectorExpr:
		if selection, found := info.Selections[expression]; found {
			return selection.Kind() != types.FieldVal && !types.IsInterface(selection.Recv())
		}
		_, function := info.Uses[expression.Sel].(*types.Func)
		return function
	case *ast.IndexExpr:
		return staticFunction(expression.X, info)
	case *ast.IndexListExpr:
		return staticFunction(expression.X, info)
	}
	return false
}

// callbackWalk collects the methods named names that a value of a type may have called on it, through
// its pointer's method set, and for a reflective library through its exported fields, elements and
// keys; dynamic marks a part whose methods depend on the value, such as an interface.
type callbackWalk struct {
	names      []string
	reflective bool
	seen       *typeutil.Map
	methods    []*types.Func
	dynamic    bool
}

func (walk *callbackWalk) visit(t types.Type) {
	if walk.seen.At(t) != nil {
		return
	}
	walk.seen.Set(t, true)
	if _, parameter := t.(*types.TypeParam); parameter || types.IsInterface(t) {
		walk.dynamic = true
		return
	}
	addressed := t
	if _, pointer := t.Underlying().(*types.Pointer); !pointer {
		addressed = types.NewPointer(t)
	}
	methods := types.NewMethodSet(addressed)
	for i := range methods.Len() {
		method, ok := methods.At(i).Obj().(*types.Func)
		if !ok || !slices.Contains(walk.names, method.Name()) {
			continue
		}
		if types.IsInterface(method.Signature().Recv().Type()) {
			walk.dynamic = true
			continue
		}
		walk.methods = append(walk.methods, method)
	}
	if !walk.reflective {
		return
	}
	switch underlying := t.Underlying().(type) {
	case *types.Pointer:
		walk.visit(underlying.Elem())
	case *types.Slice:
		walk.visit(underlying.Elem())
	case *types.Array:
		walk.visit(underlying.Elem())
	case *types.Map:
		walk.visit(underlying.Key())
		walk.visit(underlying.Elem())
	case *types.Struct:
		for i := range underlying.NumFields() {
			if field := underlying.Field(i); field.Exported() || field.Embedded() {
				walk.visit(field.Type())
			}
		}
	}
}

// callEffect is the effect a call has by itself: Channel for closing a channel, Unknown for a call
// whose callee is not one function the type checker proved. A call of a function, a method of a
// concrete type, a function literal, any other builtin, or a conversion has none of its own.
func callEffect(call *ast.CallExpr, info *types.Info) (effects.Effect, bool) {
	if _, literal := ast.Unparen(call.Fun).(*ast.FuncLit); literal {
		return "", false
	}
	callee := info.Types[call.Fun]
	switch {
	case callee.IsType():
		return "", false
	case callee.IsBuiltin():
		if builtin, ok := typeutil.Callee(info, call).(*types.Builtin); ok && builtin.Name() == "close" {
			return effects.Channel, true
		}
		return "", false
	case typeutil.StaticCallee(info, call) == nil:
		return effects.Unknown, true
	}
	return "", false
}

// packageVarRoot is the package-level variable an expression stores through: the variable, or the
// variable a field, element, slice or pointee of it belongs to. Any other expression has none.
func packageVarRoot(expression ast.Expr, info *types.Info) *ast.Ident {
	for {
		switch current := ast.Unparen(expression).(type) {
		case *ast.Ident:
			if isPackageVar(info.Uses[current]) {
				return current
			}
			return nil
		case *ast.SelectorExpr:
			if selection, found := info.Selections[current]; found {
				if selection.Kind() != types.FieldVal {
					return nil
				}
				expression = current.X
				continue
			}
			if isPackageVar(info.Uses[current.Sel]) {
				return current.Sel
			}
			return nil
		case *ast.IndexExpr:
			expression = current.X
		case *ast.SliceExpr:
			expression = current.X
		case *ast.StarExpr:
			expression = current.X
		default:
			return nil
		}
	}
}

func isPackageVar(object types.Object) bool {
	variable, ok := object.(*types.Var)
	return ok && !variable.IsField() && variable.Pkg() != nil && variable.Parent() == variable.Pkg().Scope()
}

// mutatingMethod reports whether selecting a method may change the value it is selected on: a method
// with a pointer receiver, other than one of a library type the effects catalog knows leaves its
// receiver alone. A call of an interface method is Unknown by itself, so it is no write.
func mutatingMethod(selection *types.Selection) bool {
	if selection.Kind() != types.MethodVal {
		return false
	}
	method, ok := selection.Obj().(*types.Func)
	if !ok {
		return false
	}
	receiver := method.Signature().Recv().Type()
	if _, pointer := receiver.(*types.Pointer); !pointer {
		return false
	}
	return method.Pkg() == nil || effects.MutatesReceiver(method.Pkg().Path(), receiverTypeName(receiver))
}
