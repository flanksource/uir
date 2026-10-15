package storage

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage/symbolhandle"
)

// TypeForm is the form (struct, interface, alias, or other) of a type declaration, classified from its
// shape by ClassifyTypeShape; from format 2 the document also declares it, and the two must agree. It
// applies only to the builtin type kind; every other kind, including the other kinds of the type
// category (interface, record, table, and custom kinds), has no type form.
func TypeForm(version int, symbol DocumentSymbol) (string, error) {
	if symbol.Kind != symbolhandle.KindType.String() {
		if symbol.TypeForm != "" {
			return "", fmt.Errorf("%s symbol has type_form %q", symbol.Kind, symbol.TypeForm)
		}
		return "", nil
	}
	if symbol.ID == nil {
		if symbol.TypeForm != "" {
			return "", fmt.Errorf("unproven type has type_form %q", symbol.TypeForm)
		}
		return "", nil
	}
	if version >= 2 && version <= DocumentFormatVersion {
		switch symbol.TypeForm {
		case "struct", "interface", "other", "alias":
			actual, err := ClassifyTypeShape(symbol)
			if err != nil {
				return "", err
			}
			if actual != symbol.TypeForm {
				return "", fmt.Errorf("type %s has type_form %q but shape is %q", symbol.Key, symbol.TypeForm, actual)
			}
			return actual, nil
		}
		return "", fmt.Errorf("type %s has invalid type_form %q", symbol.Key, symbol.TypeForm)
	}
	if version != 1 {
		return "", fmt.Errorf("unsupported document format version %d", version)
	}
	if symbol.TypeForm != "" {
		return "", fmt.Errorf("v1 type %s has unexpected type_form", symbol.Key)
	}
	return ClassifyTypeShape(symbol)
}

// ClassifyTypeShape is the form a type symbol's shape declares. The shape is the single source of the
// form, in one of the two shape languages a type symbol has: the Go indexer's Go type declaration,
// classified by go/parser, or the uir.Shape text of a UIR class published from nodes, which opens with
// the class kind and is struct-like, since UIR models neither aliases nor defined non-struct types (a
// UIR interface is the interface kind, which has no type form). Any other shape is an error.
func ClassifyTypeShape(symbol DocumentSymbol) (string, error) {
	if keyword, _, _ := strings.Cut(symbol.Shape, " "); keyword == string(uir.NodeTypeType) {
		return "struct", nil
	}
	file, err := parser.ParseFile(token.NewFileSet(), "shape.go", "package p\n"+symbol.Shape, 0)
	if err != nil || len(file.Decls) != 1 {
		return "", fmt.Errorf("classify type %s shape %q: it is neither a UIR class nor one Go type declaration: %v", symbol.Key, symbol.Shape, err)
	}
	declaration, ok := file.Decls[0].(*ast.GenDecl)
	if !ok || len(declaration.Specs) != 1 {
		return "", fmt.Errorf("classify type %s: shape is not one type declaration", symbol.Key)
	}
	typeSpec, ok := declaration.Specs[0].(*ast.TypeSpec)
	if !ok || declaration.Tok != token.TYPE {
		return "", fmt.Errorf("classify type %s: shape is not a type declaration", symbol.Key)
	}
	if typeSpec.Assign.IsValid() {
		return "alias", nil
	}
	switch typeSpec.Type.(type) {
	case *ast.StructType:
		return "struct", nil
	case *ast.InterfaceType:
		return "interface", nil
	default:
		return "other", nil
	}
}
