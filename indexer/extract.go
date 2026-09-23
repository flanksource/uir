package indexer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
)

func extractGoFile(pathKey, packagePath string, content []byte) (fileIndex, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, pathKey, content, parser.AllErrors|parser.ParseComments)
	if err != nil {
		return fileIndex{}, fmt.Errorf("parse Go source %q: %w", pathKey, err)
	}
	if strings.HasSuffix(file.Name.Name, "_test") {
		packagePath += "_test"
	}
	indexed := fileIndex{PackagePath: packagePath, PackageName: file.Name.Name}
	imports, err := goImports(file)
	if err != nil {
		return fileIndex{}, fmt.Errorf("read imports from %q: %w", pathKey, err)
	}
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.GenDecl:
			if err := indexed.addTypes(fileSet, pathKey, declaration); err != nil {
				return fileIndex{}, err
			}
		case *ast.FuncDecl:
			if err := indexed.addFunction(fileSet, pathKey, declaration, imports); err != nil {
				return fileIndex{}, err
			}
		}
	}
	return indexed, nil
}

func goImports(file *ast.File) (map[string]string, error) {
	imports := make(map[string]string, len(file.Imports))
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, err
		}
		alias := path.Base(importPath)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias != "_" && alias != "." {
			imports[alias] = importPath
		}
	}
	return imports, nil
}

func (indexed *fileIndex) addTypes(fileSet *token.FileSet, pathKey string, declaration *ast.GenDecl) error {
	if declaration.Tok != token.TYPE {
		return nil
	}
	for _, rawSpec := range declaration.Specs {
		spec, ok := rawSpec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		identifier := uir.Identifier{Package: indexed.PackagePath, Type: spec.Name.Name, NodeType: uir.NodeTypeType}
		builder := uir.NewType(spec.Name.Name).
			WithPackage(indexed.PackagePath).
			WithVisibility(goVisibility(spec.Name.Name))
		start, end, column := sourceRange(fileSet, spec)
		builder = builder.WithSource(pathKey, *start, *end)
		fieldStart := len(indexed.Nodes)
		if structType, ok := spec.Type.(*ast.StructType); ok {
			fields, err := indexed.addStructFields(fileSet, pathKey, identifier, structType)
			if err != nil {
				return err
			}
			builder.WithVariables(fields...)
		}
		node := builder.Build()
		fieldNodes := append([]nodeSpec(nil), indexed.Nodes[fieldStart:]...)
		indexed.Nodes = indexed.Nodes[:fieldStart]
		indexed.Nodes = append(indexed.Nodes, newNodeSpec(node, "types", len(indexed.Nodes), "", start, end, column))
		indexed.Nodes = append(indexed.Nodes, fieldNodes...)
	}
	return nil
}

func (indexed *fileIndex) addStructFields(fileSet *token.FileSet, pathKey string, parent uir.Identifier, structType *ast.StructType) (uir.ParamsDef, error) {
	fields := make(uir.ParamsDef, 0, len(structType.Fields.List))
	for _, field := range structType.Fields.List {
		nativeType, err := formatNode(fileSet, field.Type)
		if err != nil {
			return nil, fmt.Errorf("format field in %q: %w", pathKey, err)
		}
		names := fieldNames(field, nativeType)
		for _, name := range names {
			identifier := parent
			identifier.Field = name
			identifier.NodeType = uir.NodeTypeField
			fieldNode := uir.NewRecordField(name, uir.RecordFieldType(nativeType), identifier).Build()
			fieldNode.Visibility = goVisibility(name)
			start, end, column := sourceRange(fileSet, field)
			projection := storage.Field{
				Role: "field", Label: name, FieldType: nativeType, NativeType: nativeType,
				EnumValues: storage.JSON(`[]`), TypeRef: storage.JSON(`{}`), DefaultValue: storage.JSON(`null`),
				Validation: storage.JSON(`{}`), Visibility: string(fieldNode.Visibility),
			}
			indexed.Nodes = append(indexed.Nodes, newNodeSpec(fieldNode, "fields", len(indexed.Nodes), parent.IdentityKey(), start, end, column))
			indexed.Nodes[len(indexed.Nodes)-1].Field = &projection
			fields = append(fields, fieldNode)
		}
	}
	return fields, nil
}

func (indexed *fileIndex) addFunction(fileSet *token.FileSet, pathKey string, declaration *ast.FuncDecl, imports map[string]string) error {
	identifier := uir.Identifier{
		Package: indexed.PackagePath, Method: declaration.Name.Name,
		Signature: functionSignature(fileSet, declaration.Type), NodeType: uir.NodeTypeMethod,
	}
	if declaration.Recv != nil && len(declaration.Recv.List) > 0 {
		identifier.Type = receiverName(declaration.Recv.List[0].Type)
		if identifier.Type == "" {
			return fmt.Errorf("determine receiver for %s in %q", declaration.Name.Name, pathKey)
		}
	}
	builder := uir.NewMethod(declaration.Name.Name, identifier).
		WithPackage(indexed.PackagePath).
		WithVisibility(goVisibility(declaration.Name.Name))
	params, err := goFields(fileSet, declaration.Type.Params, "_")
	if err != nil {
		return fmt.Errorf("extract parameters for %s in %q: %w", declaration.Name.Name, pathKey, err)
	}
	returns, err := goFields(fileSet, declaration.Type.Results, "")
	if err != nil {
		return fmt.Errorf("extract returns for %s in %q: %w", declaration.Name.Name, pathKey, err)
	}
	builder.WithParams(params...).WithReturns(returns...)
	if identifier.Type != "" {
		builder.WithType(identifier.Type)
	}
	start, end, column := sourceRange(fileSet, declaration)
	builder = builder.WithSource(pathKey, *start, *end)
	parentIdentity := uir.Identifier{Package: indexed.PackagePath, NodeType: uir.NodeTypePackage}.IdentityKey()
	if identifier.Type != "" {
		parentIdentity = uir.Identifier{Package: indexed.PackagePath, Type: identifier.Type, NodeType: uir.NodeTypeType}.IdentityKey()
	}
	node := builder.Build()
	indexed.Nodes = append(indexed.Nodes, newNodeSpec(node, "methods", len(indexed.Nodes), parentIdentity, start, end, column))
	indexed.addCalls(fileSet, declaration, identifier, imports)
	return nil
}

func (indexed *fileIndex) addCalls(fileSet *token.FileSet, declaration *ast.FuncDecl, from uir.Identifier, imports map[string]string) {
	callIndex := 0
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		target, text, localRoot, resolvable, ok := callTarget(fileSet, call.Fun, indexed.PackagePath, imports)
		if !ok {
			return true
		}
		start, end, column := sourceRange(fileSet, call.Fun)
		indexed.Calls = append(indexed.Calls, callSpec{
			FromIdentity: from.IdentityKey(), ToIdentifier: target,
			LocalRoot: localRoot, Resolvable: resolvable, StatementPath: fmt.Sprintf("calls/%06d", callIndex),
			StartLine: start, EndLine: end, Column: column, Text: text,
		})
		callIndex++
		return true
	})
}

func callTarget(fileSet *token.FileSet, expression ast.Expr, packagePath string, imports map[string]string) (uir.Identifier, string, bool, bool, bool) {
	text, err := formatNode(fileSet, expression)
	if err != nil {
		return uir.Identifier{}, "", false, false, false
	}
	switch expression := expression.(type) {
	case *ast.Ident:
		return uir.Identifier{Package: packagePath, Method: expression.Name, NodeType: uir.NodeTypeMethod}, text, true, true, true
	case *ast.SelectorExpr:
		targetPackage := packagePath
		localRoot := true
		resolvable := false
		if qualifier, ok := expression.X.(*ast.Ident); ok {
			if imported, found := imports[qualifier.Name]; found {
				targetPackage = imported
				localRoot = false
				resolvable = true
			}
		}
		return uir.Identifier{Package: targetPackage, Method: expression.Sel.Name, NodeType: uir.NodeTypeMethod}, text, localRoot, resolvable, true
	default:
		return uir.Identifier{}, "", false, false, false
	}
}

func newNodeSpec(node uir.Node, slot string, ordinal int, parent string, start, end, column *int) nodeSpec {
	payload, err := json.Marshal(node)
	if err != nil {
		panic(fmt.Sprintf("marshal extracted %T: %v", node, err))
	}
	return nodeSpec{
		Identifier: node.GetIdentifier(), ParentIdentity: parent, ChildSlot: slot, Ordinal: ordinal,
		Payload: storage.JSON(payload), SemanticHash: node.Hash(), StartLine: start, EndLine: end, Column: column,
	}
}

func functionSignature(fileSet *token.FileSet, function *ast.FuncType) string {
	signature, err := formatNode(fileSet, function)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(signature, "func")
}

func goFields(fileSet *token.FileSet, list *ast.FieldList, unnamed string) (uir.ParamsDef, error) {
	if list == nil {
		return nil, nil
	}
	fields := make(uir.ParamsDef, 0, len(list.List))
	for _, field := range list.List {
		nativeType, err := formatNode(fileSet, field.Type)
		if err != nil {
			return nil, err
		}
		if len(field.Names) == 0 {
			fields = append(fields, uir.Field(unnamed, uir.RecordFieldType(nativeType)))
			continue
		}
		for _, name := range field.Names {
			fields = append(fields, uir.Field(name.Name, uir.RecordFieldType(nativeType)))
		}
	}
	return fields, nil
}

func formatNode(fileSet *token.FileSet, node any) (string, error) {
	var output bytes.Buffer
	if err := format.Node(&output, fileSet, node); err != nil {
		return "", err
	}
	return output.String(), nil
}

func sourceRange(fileSet *token.FileSet, node ast.Node) (*int, *int, *int) {
	startPosition := fileSet.Position(node.Pos())
	endPosition := fileSet.Position(node.End())
	return &startPosition.Line, &endPosition.Line, &startPosition.Column
}

func receiverName(expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name
	case *ast.StarExpr:
		return receiverName(expression.X)
	case *ast.IndexExpr:
		return receiverName(expression.X)
	case *ast.IndexListExpr:
		return receiverName(expression.X)
	case *ast.ParenExpr:
		return receiverName(expression.X)
	default:
		return ""
	}
}

func goVisibility(name string) uir.Visibility {
	if name != "" && unicode.IsUpper([]rune(name)[0]) {
		return uir.VisibilityPublic
	}
	return uir.VisibilityPrivate
}

func fieldNames(field *ast.Field, nativeType string) []string {
	if len(field.Names) > 0 {
		names := make([]string, len(field.Names))
		for i := range field.Names {
			names[i] = field.Names[i].Name
		}
		return names
	}
	return []string{strings.TrimLeft(nativeType, "*[]")}
}
