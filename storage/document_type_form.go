package storage

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

func TypeForm(version int, symbol DocumentSymbol) (string, error) {
	if symbol.Kind != "type" {
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
	if version == DocumentFormatVersion {
		switch symbol.TypeForm {
		case "struct", "interface", "other", "alias":
			actual, err := classifyTypeShape(symbol)
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
	return classifyTypeShape(symbol)
}

func classifyTypeShape(symbol DocumentSymbol) (string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "shape.go", "package p\n"+symbol.Shape, 0)
	if err != nil || len(file.Decls) != 1 {
		return "", fmt.Errorf("classify v1 type %s shape %q: %v", symbol.Key, symbol.Shape, err)
	}
	declaration, ok := file.Decls[0].(*ast.GenDecl)
	if !ok || len(declaration.Specs) != 1 {
		return "", fmt.Errorf("classify v1 type %s: shape is not one type declaration", symbol.Key)
	}
	typeSpec, ok := declaration.Specs[0].(*ast.TypeSpec)
	if !ok || declaration.Tok != token.TYPE {
		return "", fmt.Errorf("classify v1 type %s: shape is not a type declaration", symbol.Key)
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
