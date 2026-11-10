package indexer

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/flanksource/uir/effects"
	"github.com/flanksource/uir/indexer/syntax"
	"github.com/flanksource/uir/storage"
	"golang.org/x/tools/go/types/typeutil"
)

// effectSite is one place in a file that has an effect, with the source text that shows it.
type effectSite struct {
	effect effects.Effect
	start  int
	text   string
}

// recordEffects sets on each symbol the effects its own declaration has that no call or access
// occurrence shows: starting a goroutine, a channel operation, ranging over a map, and a call the type
// checker cannot pin to one function, such as a call through an interface, a function value or a type
// parameter. A site belongs to the innermost declaration enclosing it; each symbol keeps the first
// site of each effect. The effects of what a declaration calls, reads and writes are the query's to
// combine.
func recordEffects(source typedSource, symbols []storage.DocumentSymbol) error {
	sites, err := findEffectSites(source)
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
		if symbol.Effects == nil {
			symbol.Effects = map[effects.Effect]string{}
		}
		if _, found := symbol.Effects[site.effect]; !found {
			symbol.Effects[site.effect] = site.text
		}
	}
	return nil
}

func findEffectSites(source typedSource) ([]effectSite, error) {
	info := source.pkg.TypesInfo
	var sites []effectSite
	var formatErr error
	add := func(effect effects.Effect, at ast.Node, prefix string, shown ast.Node) {
		text := ""
		if shown != nil {
			if text, formatErr = syntax.FormatNode(source.pkg.Fset, shown); formatErr != nil {
				formatErr = fmt.Errorf("format %s site: %w", effect, formatErr)
				return
			}
		}
		sites = append(sites, effectSite{effect: effect, start: source.tokens.Offset(at.Pos()), text: prefix + text})
	}
	ast.Inspect(source.file, func(node ast.Node) bool {
		if formatErr != nil {
			return false
		}
		switch node := node.(type) {
		case *ast.GoStmt:
			add(effects.Goroutine, node, "go ", node.Call.Fun)
		case *ast.SendStmt:
			add(effects.Channel, node, "", node)
		case *ast.UnaryExpr:
			if node.Op == token.ARROW {
				add(effects.Channel, node, "", node)
			}
		case *ast.SelectStmt:
			add(effects.Channel, node, "select", nil)
		case *ast.RangeStmt:
			ranged := info.TypeOf(node.X)
			if ranged == nil {
				break
			}
			switch ranged.Underlying().(type) {
			case *types.Chan:
				add(effects.Channel, node, "range ", node.X)
			case *types.Map:
				add(effects.MapOrder, node, "range ", node.X)
			}
		case *ast.CallExpr:
			if effect, dynamic := callEffect(node, info); dynamic {
				add(effect, node, "", node.Fun)
			}
		}
		return true
	})
	return sites, formatErr
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
