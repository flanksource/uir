// Package golang lowers Go syntax into UIR. It reads only the syntax tree: no
// type checking, so an identifier is a name and nothing is resolved.
package golang

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"github.com/flanksource/uir"
)

// LowerExpr turns a Go expression into a UIR expression. Identifiers, selector
// chains, literals, binary, unary, parenthesised and call expressions are
// structured; every other kind, and an operator UIR has no counterpart for, is
// an expression with no variant that keeps its source text. The result always
// carries the source text of expr in Content. src is the file fset positions
// expr in.
func LowerExpr(fset *token.FileSet, src []byte, expr ast.Expr) uir.ExprStmt {
	if expr == nil {
		panic("golang: LowerExpr was given a nil expression")
	}
	l := lowerer{fset: fset, src: src}
	lowered := l.expr(expr)
	lowered.Content = new(l.text(expr))
	return lowered
}

// SourceExpr is node as an expression UIR does not model: no variant, and the
// node's exact source text in Content.
func SourceExpr(fset *token.FileSet, src []byte, node ast.Node) uir.ExprStmt {
	if node == nil {
		panic("golang: SourceExpr was given a nil node")
	}
	return lowerer{fset: fset, src: src}.source(node)
}

type lowerer struct {
	fset *token.FileSet
	src  []byte
}

func (l lowerer) text(node ast.Node) string {
	file := l.fset.File(node.Pos())
	if file == nil {
		panic(fmt.Sprintf("golang: %T is not positioned in the file set", node))
	}
	start, end := file.Offset(node.Pos()), file.Offset(node.End())
	if start > end || end > len(l.src) {
		panic(fmt.Sprintf("golang: %T spans bytes %d to %d of a %d byte source", node, start, end, len(l.src)))
	}
	return string(l.src[start:end])
}

func (l lowerer) source(node ast.Node) uir.ExprStmt {
	expr := uir.ExprStmt{}
	expr.Type, expr.Content = uir.ASTStatementTypeExpression, new(l.text(node))
	return expr
}

func (l lowerer) expr(expr ast.Expr) uir.ExprStmt {
	switch x := expr.(type) {
	case *ast.Ident:
		if x.Name == "true" || x.Name == "false" {
			return uir.LitExpr(x.Name, uir.RecordFieldTypeBoolean)
		}
		return uir.VarExpr(x.Name)
	case *ast.SelectorExpr:
		if name, ok := dottedName(x); ok {
			return uir.VarExpr(name)
		}
	case *ast.BasicLit:
		return l.literal(x)
	case *ast.ParenExpr:
		return l.expr(x.X)
	case *ast.BinaryExpr:
		if op, ok := binaryOps[x.Op]; ok {
			return uir.BinaryExpr(l.operand(x.X), op, l.operand(x.Y))
		}
	case *ast.UnaryExpr:
		if op, ok := unaryOps[x.Op]; ok {
			return uir.UnaryExpr(op, l.operand(x.X))
		}
	case *ast.CallExpr:
		if call, ok := l.call(x); ok {
			return uir.ExprStmt{MethodCall: &call}
		}
	}
	return l.source(expr)
}

// operand lowers the operand of an operator. A binary expression kept as source
// text is parenthesised: the renderer adds parentheses by the precedence of a
// structured operand and cannot see inside text.
func (l lowerer) operand(expr ast.Expr) uir.ExprStmt {
	lowered := l.expr(expr)
	if _, binary := ast.Unparen(expr).(*ast.BinaryExpr); binary && lowered.Value() == nil {
		lowered.Content = new("(" + *lowered.Content + ")")
	}
	return lowered
}

// dottedName is a selector chain of identifiers as written, a.b.c.
func dottedName(expr ast.Expr) (string, bool) {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Name, true
	case *ast.SelectorExpr:
		if base, ok := dottedName(x.X); ok {
			return base + "." + x.Sel.Name, true
		}
	}
	return "", false
}

func (l lowerer) literal(lit *ast.BasicLit) uir.ExprStmt {
	switch lit.Kind {
	case token.STRING, token.CHAR:
		if value, err := strconv.Unquote(lit.Value); err == nil {
			return uir.LitExpr(value, uir.RecordFieldTypeString)
		}
	case token.INT:
		return uir.LitExpr(lit.Value, uir.RecordFieldTypeInt)
	case token.FLOAT:
		return uir.LitExpr(lit.Value, uir.RecordFieldTypeFloat)
	}
	return l.source(lit)
}

// call lowers a call of a named function or of a selector. The callee is named,
// not resolved: a selector's receiver is kept as written in the identifier's
// Type, so s.Save and strings.HasPrefix both read back as the source does.
func (l lowerer) call(call *ast.CallExpr) (uir.MethodCallStmt, bool) {
	if call.Ellipsis.IsValid() {
		return uir.MethodCallStmt{}, false
	}
	var builder *uir.MethodCallBuilder
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		builder = uir.NewMethodCall(fun.Name, uir.Identifier{NodeType: uir.NodeTypeMethod})
	case *ast.SelectorExpr:
		builder = uir.NewMethodCall(fun.Sel.Name, uir.Identifier{Type: strings.TrimSpace(l.text(fun.X)), NodeType: uir.NodeTypeMethod}).
			WithReceiver(l.expr(fun.X))
	default:
		return uir.MethodCallStmt{}, false
	}
	for _, arg := range call.Args {
		builder.WithPositionalArg(l.expr(arg))
	}
	return builder.Build(), true
}

var binaryOps = map[token.Token]uir.BinaryOp{
	token.LAND: uir.BinaryOpAnd,
	token.LOR:  uir.BinaryOpOr,
	token.EQL:  uir.BinaryOpEqual,
	token.NEQ:  uir.BinaryOpNotEqual,
	token.LSS:  uir.BinaryOpLess,
	token.LEQ:  uir.BinaryOpLessEqual,
	token.GTR:  uir.BinaryOpGreater,
	token.GEQ:  uir.BinaryOpGreaterEqual,
	token.ADD:  uir.BinaryOpAdd,
	token.SUB:  uir.BinaryOpSubtract,
	token.MUL:  uir.BinaryOpMultiply,
	token.QUO:  uir.BinaryOpDivide,
	token.REM:  uir.BinaryOpModulus,
	token.AND:  uir.BinaryOpBitwiseAnd,
	token.OR:   uir.BinaryOpBitwiseOr,
	token.XOR:  uir.BinaryOpBitwiseXor,
	token.SHL:  uir.BinaryOpLeftShift,
	token.SHR:  uir.BinaryOpRightShift,
}

var unaryOps = map[token.Token]uir.UnaryOp{
	token.NOT: uir.UnaryOpNot,
	token.SUB: uir.UnaryOpNeg,
	token.ADD: uir.UnaryOpPlus,
}
