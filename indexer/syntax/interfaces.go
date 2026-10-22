package syntax

import (
	"fmt"
	"go/ast"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
)

type interfaceDeclaration struct {
	path       string
	owner      string
	expression ast.Expr
}

func (indexed *File) addInterfaceDeclarations(path string, declaration *ast.GenDecl) error {
	for _, raw := range declaration.Specs {
		member := interfaceDeclaration{path: path}
		switch spec := raw.(type) {
		case *ast.TypeSpec:
			member.owner, member.expression = spec.Name.Name, spec.Type
		case *ast.ValueSpec:
			member.expression = spec.Type
		default:
			continue
		}
		if err := indexed.addInterfaceMembers(member); err != nil {
			return err
		}
	}
	return nil
}

func (indexed *File) addInterfaceMembers(member interfaceDeclaration) error {
	switch literal := member.expression.(type) {
	case *ast.StructType:
		for _, field := range literal.Fields.List {
			if err := indexed.addInterfaceMembers(interfaceDeclaration{path: member.path, expression: field.Type}); err != nil {
				return err
			}
		}
	case *ast.InterfaceType:
		for _, field := range literal.Methods.List {
			if _, callable := field.Type.(*ast.FuncType); !callable || len(field.Names) == 0 {
				continue
			}
			if err := indexed.addInterfaceMethod(member, field); err != nil {
				return err
			}
		}
	}
	return nil
}

func (indexed *File) addInterfaceMethod(member interfaceDeclaration, field *ast.Field) error {
	if len(field.Names) != 1 {
		return fmt.Errorf("interface method in %s has %d names", member.path, len(field.Names))
	}
	function := field.Type.(*ast.FuncType)
	params, err := goFields(indexed.fileSet, function.Params, "_")
	if err != nil {
		return err
	}
	returns, err := goFields(indexed.fileSet, function.Results, "")
	if err != nil {
		return err
	}
	signature, err := FormatNode(indexed.fileSet, function)
	if err != nil {
		return err
	}
	signature = strings.TrimPrefix(signature, "func")
	name := field.Names[0]
	identifier := uir.Identifier{Package: indexed.PackagePath, Type: member.owner, Method: name.Name, Signature: signature, NodeType: uir.NodeTypeMethod}
	facts, err := indexed.declaration("method", ast.IsExported(name.Name) && (member.owner == "" || ast.IsExported(member.owner)), name.Name+signature, name, DocumentStart(field.Doc, field.Pos()), field.End())
	if err != nil {
		return err
	}
	facts.Metrics = &storage.CallableMetrics{Version: storage.MetricsVersion, ParamCount: len(params)}
	start, end := indexed.lines(field)
	node := uir.NewMethod(name.Name, identifier).WithPackage(indexed.PackagePath).WithVisibility(goVisibility(name.Name)).WithParams(params...).WithReturns(returns...).WithSource(member.path, start, end).Build()
	parent := uir.Identifier{Package: indexed.PackagePath, Type: member.owner, NodeType: uir.NodeTypeType}
	indexed.Nodes = append(indexed.Nodes, newNode(node, "", len(indexed.Nodes), parent.IdentityKey(), facts))
	return nil
}
