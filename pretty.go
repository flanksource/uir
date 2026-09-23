package uir

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
)

func (p ParamsDef) Pretty() api.Text {
	if len(p) == 0 {
		return api.Text{Content: "()", Style: "text-gray-600"}
	}
	t := api.Text{Content: "(", Style: "text-gray-600"}
	for i, param := range p {
		if len(p) > 5 {
			t = t.NewLine().Indent(2)
		}
		if i > 0 {
			t = t.Append(", ", "text-gray-600")
		}

		t = t.Append(param.Field, "text-green-600").Append(": ", "text-gray-600").Add(param.FieldType.Pretty())
		if param.DefaultValue != nil {
			t = t.Append(" = ", "text-gray-400").Add(param.DefaultValue.Pretty())
		}
	}
	t = t.Append(")", "text-gray-600")
	return t
}

func (node UIRNode) ShortName() api.Text { return api.Text{Content: node.GetIdentifier().GetName()} }

func (e ASTEndpoint) Pretty() api.Text {
	t := api.Text{Content: e.String(), Style: "text-blue-500"}.Append("(", "text-gray-600")
	t = t.Add(e.Input.Pretty())
	t = t.Append(") ", "text-gray-600").Append("->", "text-gray-400").Add(e.Output.Pretty())
	return t
}

func (p PackageNode) Pretty() api.Text {
	t := api.Text{Content: "package ", Style: "text-blue-500"}.Append(p.Package, "text-green-600")

	for _, v := range p.Variables {
		t = t.NewLine().Add(v.Pretty())
	}
	for _, v := range p.InitFunctions {
		t = t.NewLine().Add(v.Pretty())
	}

	for _, child := range p.Types {
		t = t.NewLine().Add(child.Pretty())
	}
	for _, child := range p.Functions {
		t = t.NewLine().Add(child.Pretty())
	}
	return t
}

func (t TypedNode) Pretty() api.Text {
	p := api.Text{Content: ""}
	p = p.Append("class ", "text-blue-500").Append(t.Type, "text-green-600").Append("{", "text-gray-600").NewLine()

	for _, field := range t.Variables {
		p = p.Append("  ").Add(field.Pretty()).Append(";", "text-gray-600").NewLine()
	}
	for _, method := range t.Methods {
		p = p.NewLine().Add(method.Pretty().Indent(2)).NewLine()
	}
	for _, child := range t.Types {
		p = p.NewLine().Add(child.Pretty().Indent(2)).NewLine()
	}
	p = p.NewLine().Append("}", "text-gray-600")

	return p
}

func (r ASTRecord) Pretty() api.Text {
	p := api.Text{Content: string(r.RecordType), Style: "text-blue-500"}.Append(r.Type, "text-green-600").Append("{", "text-gray-600").NewLine()

	for _, field := range r.Fields {
		p = p.Append("  ").Add(field.Pretty()).Append(";", "text-gray-600").NewLine()
	}
	p = p.Append("}", "text-gray-600")

	return p
}

func (f RecordField) Pretty() api.Text {
	p := api.Text{Content: f.Field, Style: "text-green-600"}.Append(": ", "text-gray-600").Add(f.FieldType.Pretty())
	if f.DefaultValue != nil {
		p = p.Append(" = ", "text-gray-400").Add(f.DefaultValue.Pretty())
	}
	return p
}

func (n MethodNode) Pretty() api.Text {
	t := api.Text{Content: "", Style: ""}
	t = t.Append("function ", "text-blue-500").Append(n.Method, "text-green-600").
		Add(n.Params.Pretty())

	if len(n.Returns) > 0 {
		t = t.Append(" : ", "text-gray-600")
		t = t.Add(n.Returns.Pretty())
	}

	if n.Body != nil {
		t = t.Add(n.Body.Pretty())
	}

	return t
}

func (s IfStmt) Pretty() api.Text {
	t := api.Text{Content: "if ", Style: "text-blue-500"}.Add(s.Condition.Pretty()).NewLine().Add(s.Then.Pretty().Indent(2))

	if s.Else != nil {
		t = t.Append(" else ", "muted").NewLine().Add(s.Else.Pretty().Indent(2))
	}
	return t
}

func (s IfStmt) String() string {
	return s.Pretty().ANSI()
}

func (s SwitchStmt) Pretty() api.Text {
	t := api.Text{Content: "switch", Style: "text-blue-500"}.Add(s.Value.Pretty())

	for _, c := range s.Cases {
		t = t.NewLine().Append("case ", "muted").Add(c.Condition.Pretty()).Append(": ", "muted").Add(c.Body.Pretty())
	}

	return t
}

func (s SwitchStmt) String() string {
	return s.Pretty().ANSI()
}

func (s ForStmt) Pretty() api.Text {
	return api.Text{Content: "for", Style: "text-blue-500"}.Add(s.Init.Pretty()).Append("; ", "text-gray-500").Add(s.Cond.Pretty()).Append("; ", "text-gray-500").Add(s.Update.Pretty()).
		NewLine().Add(s.Body.Pretty().Indent(2))
}

func (s ForStmt) String() string {
	return s.Pretty().ANSI()
}

func (s AssignmentStmt) Pretty() api.Text {
	op := string(s.Op)
	switch s.Op {
	case "":
		op = "="
	case AssignmentOpAppend:
		op = "<<"
	}
	return s.Target.Pretty().Append(" "+op+" ", "text-orange-500").Add(s.Value.Pretty())
}

func (s AssignmentStmt) String() string {
	return s.Pretty().ANSI()
}

func (s UnaryStmt) Pretty() api.Text {
	return api.Text{Content: string(s.Operator), Style: "text-orange-500"}.Add(s.Operand.Pretty())
}

func (s UnaryStmt) String() string {
	return s.Pretty().ANSI()
}

func (s LiteralStmt) Pretty() api.Text {
	value := ""
	style := "text-green-600"
	if s.Value != nil {
		value = *s.Value
	}
	if s.FieldType != RecordFieldTypeNumber &&
		s.FieldType != RecordFieldTypeFloat &&
		s.FieldType != RecordFieldTypeInt &&
		s.FieldType != RecordFieldTypeBoolean {
		value = fmt.Sprintf("\"%s\"", value)
	}
	return s.FieldType.Pretty().Append(" ", "").Append(value, style)
}

func (s LiteralStmt) String() string {
	return s.Pretty().ANSI()
}

func (s BinaryStmt) Pretty() api.Text {
	return s.Left.Pretty().Append(" "+string(s.Operator)+" ", "text-orange-600").Add(s.Right.Pretty())
}

func (s BinaryStmt) String() string {
	return s.Pretty().ANSI()
}

func (s CastStmt) Pretty() api.Text {
	return api.Text{Content: "(", Style: "text-gray-600"}.Append(s.TargetType, "text-blue-500").Append(")", "text-gray-600").Add(s.Expr.Pretty())
}

func (s CastStmt) String() string {
	return s.Pretty().ANSI()
}

func (s WhileStmt) Pretty() api.Text {
	return api.Text{Content: "while", Style: "text-blue-500"}.Add(s.Condition.Pretty()).Add(s.Body.Pretty())
}

func (s WhileStmt) String() string {
	return s.Pretty().ANSI()
}

func (t TypeDeclStmt) Pretty() api.Text {
	p := api.Text{Content: "type ", Style: "text-blue-500"}.Append(t.Name, "text-green-600")
	return p
}

func (t TypeDeclStmt) String() string {
	return t.Pretty().ANSI()
}

func (s ConstDeclStmt) Pretty() api.Text {
	p := api.Text{Content: "const ", Style: "text-blue-500"}.Append(s.Field, "text-green-600")
	if s.FieldType != "" {
		p = p.Append(" ", "text-muted").Append(string(s.FieldType))
	}
	if s.DefaultValue != nil {
		p = p.Append(" = ", "text-muted").Add(s.DefaultValue.Pretty())
	}
	return p
}

func (s ConstDeclStmt) String() string {
	return s.Pretty().ANSI()
}

func (s BlockStmt) Pretty() api.Text {
	if len(s.Children) == 1 && len(s.Variables) == 0 {
		return s.Children[0].Pretty()
	}
	t := api.Text{Content: "{", Style: "text-gray-600"}.NewLine()
	for _, v := range s.Variables {
		t = t.NewLine().Add(v.Pretty()).Append(";", "text-gray-600").NewLine()
	}
	for _, child := range s.Children {
		t = t.NewLine().Add(child.Pretty().Indent(2))
	}
	t = t.NewLine().Append("}", "text-gray-600")
	return t
}

func (s BlockStmt) String() string {
	return s.Pretty().ANSI()
}

func (s TupleStmt) Pretty() api.Text {
	t := api.Text{Content: "(", Style: "text-gray-600"}
	for i, elem := range s.Elements {
		if i > 0 {
			t = t.Append(", ", "text-gray-600")
		}
		t = t.Add(elem.Pretty())
	}
	t = t.Append(")", "text-gray-600")
	return t
}

func (s TupleStmt) String() string {
	return s.Pretty().ANSI()
}

func (s ObjectLiteralStmt) Pretty() api.Text {
	t := api.Text{Content: "{ ", Style: "text-gray-600"}
	for i, entry := range s.Entries {
		if i > 0 {
			t = t.Append(", ", "text-gray-600")
		}
		t = t.Append(entry.Key, "text-cyan-600").Append(": ", "text-gray-600").Add(entry.Value.Pretty())
	}
	t = t.Append(" }", "text-gray-600")
	return t
}

func (s ObjectLiteralStmt) String() string {
	return s.Pretty().ANSI()
}

func (s VariableStmt) Pretty() api.Text { return api.Text{Content: s.Name, Style: "text-green-600"} }

func (s VariableStmt) String() string {
	return s.Pretty().ANSI()
}

func (s MethodCallStmt) Pretty() api.Text {
	t := api.Text{Content: ""}
	if s.Method != nil {
		t = t.Add(s.Method.Pretty())
	}
	return t.Add(s.Arguments.Pretty().Wrap("(", ")", "text-gray-600"))
}

func (s MethodCallStmt) String() string {
	return s.Pretty().ANSI()
}

func (r RecordType) Pretty() api.Text {
	return api.Text{Content: ""}.Append(string(r), "text-green-600")
}

func (r RecordIdentifier) Pretty() api.Text {
	t := api.Text{Content: ""}.Add(r.RecordType.Pretty()).Append("://", "text-gray-400")
	if r.EnvironmentIdentifier.String() != "" {
		t = t.Add(r.EnvironmentIdentifier.Pretty()).Append("/", "text-gray-400")
	}
	t = t.Append(r.ID, "text-green-600")
	return t
}

func (s RecordReadStmt) String() string {
	return s.Pretty().ANSI()
}

func (s RecordReadStmt) Pretty() api.Text {
	if s.Expression.Expression != "" {
		if s.ExpressionType == ExpressionTypeSQL {
			return api.Text{Content: "db.Query", Style: "text-green-500"}.Add(api.Text{Content: ""}.Add(api.Code{Language: "sql", Content: s.Expression.Expression}).Wrap("(\"", "\")", "text-gray-600"))
		}
		return api.Text{Content: ""}.Add(api.Code{Language: string(s.ExpressionType), Content: s.Expression.Expression})
	}
	if s.Record == nil {
		return api.Text{Content: "read", Style: "text-green-600"}.Add(s.Arguments.Pretty().Wrap("(", ")", "text-gray-600"))
	}
	return api.Text{Content: ""}.Add(s.Record.Pretty()).Append(".read", "text-green-600").Add(s.Arguments.Pretty().Wrap("(", ")", "text-gray-600"))
}

func (s RecordWriteStmt) Pretty() api.Text {
	if s.Content != nil {
		if s.ExpressionType == ExpressionTypeSQL {
			return api.Text{Content: "db.Update", Style: "text-green-500"}.Add(api.Text{Content: ""}.Add(api.Code{Language: "sql", Content: *s.Expression.Content}).Wrap("(\"", "\")", "text-gray-600"))
		}
		return api.Text{Content: ""}.Add(api.Code{Language: string(s.ExpressionType), Content: *s.Content})
	}
	if s.Record == nil {
		return api.Text{Content: "write", Style: "text-green-600"}.Add(s.Arguments.Pretty().Wrap("(", ")", "text-gray-600"))
	}
	return api.Text{Content: ""}.Add(s.Record.Pretty()).Append(".write", "text-green-600").Add(s.Arguments.Pretty().Wrap("(", ")", "text-gray-600"))
}

func (s RecordWriteStmt) String() string {
	return s.Pretty().ANSI()
}

func (e EnvironmentIdentifier) Pretty() api.Text {
	return api.Text{Content: e.Environment, Style: "text-blue-600"}.Append("/", "text-gray-400").Append(e.URN, "text-green-600")
}

func (e EndpointType) Pretty() api.Text {
	return api.Text{Content: e.String(), Style: "text-purple-600"}
}

func (a Arguments) Pretty() api.Text {
	if len(a) == 0 {
		return api.Text{Content: "", Style: "text-gray-600"}
	}
	t := api.Text{Content: ""}
	for i, arg := range a {
		if i > 0 {
			t = t.Append(", ", "text-gray-600")
		}
		if arg.Name != nil {
			t = t.Append(*arg.Name, "text-yellow-600").Append(": ", "text-gray-600")
		}
		t = t.Add(arg.Value.Pretty())
	}
	return t
}

func (s EndpointCallStmt) Pretty() api.Text {
	t := api.Text{Content: ""}
	if s.Endpoint != nil {
		t = t.Add(s.Endpoint.Pretty())
	}
	t = t.Add(s.Arguments.Pretty().Wrap(" (", ")", "text-gray-600"))
	return t
}

func (s EndpointCallStmt) String() string {
	return s.Pretty().ANSI()
}

func (v ScopedVariableRef) Pretty() api.Text {
	t := api.Text{Content: ""}
	id := v.Identifier.Pretty()
	if id.String() != "" {
		t = t.Add(id).Append(".", "muted")
	}
	return t.Append(v.Name, NodeTypeFunctionVariable.Color())
}

func (s ExprStmt) Pretty() api.Text {
	if s.Literal != nil {
		return s.Literal.Pretty()
	}
	if s.Variable != nil {
		return s.Variable.Pretty()
	}
	if s.Binary != nil {
		return s.Binary.Pretty()
	}
	if s.Cast != nil {
		return s.Cast.Pretty()
	}
	if s.Unary != nil {
		return s.Unary.Pretty()
	}
	if s.MethodCall != nil {
		return s.MethodCall.Pretty()
	}
	if s.EndpointCall != nil {
		return s.EndpointCall.Pretty()
	}
	if s.RecordRead != nil {
		return s.RecordRead.Pretty()
	}
	if s.Tuple != nil {
		return s.Tuple.Pretty()
	}
	if s.ObjectLiteral != nil {
		return s.ObjectLiteral.Pretty()
	}
	return api.Text{Content: "expr", Style: "text-gray-500"}
}

func (s ExprStmt) String() string {
	return s.Pretty().ANSI()
}

func (s ReturnStmt) Pretty() api.Text {
	return api.Text{Content: "return ", Style: "text-red-500"}.Add(s.Value.Pretty())
}

func (s ReturnStmt) String() string {
	return s.Pretty().ANSI()
}

func (s BreakStmt) Pretty() api.Text { return api.Text{Content: "break", Style: "text-red-500"} }

func (s BreakStmt) String() string {
	return s.Pretty().ANSI()
}

func (s ContinueStmt) Pretty() api.Text {
	return api.Text{Content: "continue", Style: "text-yellow-500"}
}

func (s ContinueStmt) String() string {
	return s.Pretty().ANSI()
}

func (s RawStmt) Pretty() api.Text {
	lang := s.Language
	if lang == "" {
		lang = "text"
	}
	return api.Text{Content: ""}.Add(api.CodeBlock(lang, s.Source))
}

func (s RawStmt) String() string {
	return s.Source
}

func (s ThrowStmt) Pretty() api.Text {
	return api.Text{Content: "throw", Style: "text-red-600"}.Add(s.Exception.Pretty())
}

func (s ThrowStmt) String() string {
	return s.Pretty().ANSI()
}

func (s TryStmt) Pretty() api.Text {
	return api.Text{Content: "try", Style: "text-blue-600"}.
		Add(s.Body.Pretty().Indent(2)).Append("catch", "text-red-600").Add(s.Catch.Pretty().Indent(2))
}

func (s TryStmt) String() string {
	return s.Pretty().ANSI()
}

func (s ConditionStmt) Pretty() api.Text {
	return s.Expr.Pretty()
}

func (s ConditionStmt) String() string {
	return s.Pretty().ANSI()
}

func (r RecordFieldType) Pretty() api.Text {
	switch r {
	case RecordFieldTypeString:
		return api.Text{Content: "string", Style: "text-green-600"}
	case RecordFieldTypeNumber, RecordFieldTypeFloat, RecordFieldTypeInt:
		return api.Text{Content: string(r), Style: "text-blue-600"}
	case RecordFieldTypeBoolean:
		return api.Text{Content: "boolean", Style: "text-yellow-600"}
	case RecordFieldTypeArray:
		return api.Text{Content: "array", Style: "text-purple-600"}
	case RecordFieldTypeObject, RecordFieldTypeMap:
		return api.Text{Content: "object", Style: "text-cyan-600"}
	case RecordFieldTypeEnum:
		return api.Text{Content: "enum", Style: "text-red-600"}
	case RecordFieldTypeDate:
		return api.Text{Content: "date", Style: "text-indigo-600"}
	case RecordFieldTypeIP, RecordFieldTypeCIDR:
		return api.Text{Content: string(r), Style: "text-teal-600"}
	default:
		return api.Text{Content: string(r), Style: "text-orange-600"}
	}
}

func (l Location) Pretty() api.Text {
	var result api.Text

	if l.Path != "" {
		fileName := filepath.Base(l.Path)
		result = api.Text{Content: fileName, Style: "text-blue-500 font-medium"}
	}

	if l.EndLine != nil && l.StartLine != nil && *l.EndLine != *l.StartLine {
		if l.Path != "" {
			result = result.Append(":", "text-gray-400")
		}
		result = result.Append("L", "text-gray-500 text-xs")
		result = result.Append(fmt.Sprintf("%d", l.StartLine), "text-purple-600 font-mono")
		result = result.Append("-", "text-gray-400")
		result = result.Append(fmt.Sprintf("%d", l.EndLine), "text-purple-600 font-mono")
	} else if l.StartLine != nil {
		if l.Path != "" {
			result = result.Append(":", "text-gray-400")
		}
		result = result.Append("L", "text-gray-500 text-xs")
		result = result.Append(fmt.Sprintf("%d", l.StartLine), "text-purple-600 font-mono")
	}

	return result
}

func (r RelationshipType) Pretty() api.Text {
	switch r {
	case RelationshipTypeImport:
		return api.Text{Content: ""}.Add(icons.ArrowDown).Append(" import", "text-blue-600")
	case RelationshipTypeCall:
		return api.Text{Content: ""}.Add(icons.ArrowRight).Append(" call", "text-green-600")
	case RelationshipTypeInheritance:
		return api.Text{Content: ""}.Add(icons.ArrowRight).Append(" extends", "text-purple-600")
	case RelationshipTypeImplements:
		return api.Text{Content: ""}.Add(icons.ArrowRight).Append(" implements", "text-indigo-600")
	case RelationshipTypeIncludes:
		return api.Text{Content: ""}.Add(icons.ArrowRight).Append(" includes", "text-pink-600")
	case RelationshipTypeForeignKey:
		return api.Text{Content: ""}.Add(icons.ArrowRight).Append(" foreign key", "text-red-600")
	default:
		return api.Text{Content: ""}.Add(icons.ArrowRight).Append(" reference", "text-yellow-600")
	}
}

func (t StatementType) Pretty() api.Text {
	c := api.Text{Content: ""}
	switch {
	case t == ASTStatementTypeIf:
		return c.Add(icons.If)
	case strings.HasPrefix(string(t), string(ASTStatementTypeLoop)):
		return c.Add(icons.Loop)
	case t == ASTStatementTypeExpression:
		return c.Add(icons.Lambda)
	case strings.HasPrefix(string(t), string(ASTStatementTypeDecl)):
		return c.Add(icons.Variable)
	case strings.HasPrefix(string(t), string(ASTStatementTypeAssignment)):
		return c.Add(icons.Equals)
	case strings.HasPrefix(string(t), string(ASTStatementTypeRemoteRead)):
		return c.Add(icons.ArrowDown)
	case strings.HasPrefix(string(t), string(ASTStatementTypeRemoteWrite)):
		return c.Add(icons.ArrowUp)
	case strings.HasPrefix(string(t), string(ASTStatementTypeCallAPI)):
		return c.Add(icons.Http)
	case strings.HasSuffix(string(t), string(ASTStatementTypeCall)):
		return c.Add(icons.ArrowRight)
	default:
		return api.Text{Content: " other", Style: "text-gray-600"}
	}
}

func (f FieldType) Pretty() api.Text {
	switch f {
	case FieldTypeString:
		return api.Text{Content: "string", Style: "text-green-600"}
	case FieldTypeNumber, FieldTypeFloat:
		return api.Text{Content: "number", Style: "text-blue-600"}
	case FieldTypeBoolean:
		return api.Text{Content: "boolean", Style: "text-red-600"}
	case FieldTypeArray:
		return api.Text{Content: "array", Style: "text-purple-600"}
	case FieldTypeObject:
		return api.Text{Content: "object", Style: "text-pink-600"}
	case FieldTypeEnum:
		return api.Text{Content: "enum", Style: "text-indigo-600"}
	case FieldTypeDate:
		return api.Text{Content: "date", Style: "text-gray-600"}
	case FieldTypeMap:
		return api.Text{Content: "map", Style: "text-teal-600"}
	case FieldTypeXPath:
		return api.Text{Content: "xpath", Style: "text-orange-600"}
	default:
		return api.Text{Content: string(f), Style: "text-yellow-600"}
	}
}

func (v Value) Pretty() api.Text {
	if v.IsEmpty() {
		return api.Text{}
	}
	p := api.Text{Content: " "}.Add(v.FieldType.Pretty())
	if !v.IsEmpty() {
		p = p.Append(" = ", "text-gray-400 font-mono").Add(v.AsText("text-orange-600"))
	}
	return p
}

func (v Value) AsText(styles ...string) api.Textable {
	if v.Text != nil && v.Text.String() != "" {
		return api.Text{Content: ""}.Add(v.Text).Styles(styles...)
	}
	return api.Text{Content: v.Value, Style: strings.Join(styles, " ")}.Append(" ")
}

func (t TypedValue) Pretty() api.Text {
	if t.IsEmpty() {
		return api.Text{Content: "null", Style: "text-gray-500"}
	}
	val := t.String()

	if t.Bool != nil {
		return api.Text{Content: val, Style: "text-green-500"}
	}
	if t.Int != nil || t.Float != nil {
		return api.Text{Content: val, Style: "text-blue-500"}
	}
	if t.Date != nil {
		return api.Text{Content: val, Style: "text-yellow-500"}
	}
	return api.Text{Content: val, Style: "text-green-500"}
}

func (v VariableDeclStmt) Pretty() api.Text {
	p := api.Text{Content: "var ", Style: "text-blue-500"}.Append(v.Field)
	if v.FieldType != "" {
		p = p.Append(" ", "text-muted").Append(string(v.FieldType))
	}
	if v.DefaultValue != nil {
		p = p.Append(" = ", "text-muted").Add(v.DefaultValue.Pretty())
	}
	return p
}

func (v VariableDeclStmt) String() string {
	if v.FieldType != "" {
		return fmt.Sprintf("%s %s", v.Field, v.FieldType)
	}
	return v.Field
}

func (e Expression) Pretty() api.Text {
	return api.Text{Content: ""}.Add(api.Code{Content: e.Expression, Language: string(e.ExpressionType)})
}
