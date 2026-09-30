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
		return api.Text{Content: "()", Style: StylePunctuation}
	}
	t := api.Text{Content: "(", Style: StylePunctuation}
	for i, param := range p {
		if len(p) > 5 {
			t = t.NewLine().Indent(2)
		}
		if i > 0 {
			t = t.Append(", ", StylePunctuation)
		}

		t = t.Append(param.Field, StyleName).Append(": ", StylePunctuation).Add(param.FieldType.Pretty())
		if param.DefaultValue != nil {
			t = t.Append(" = ", StyleFaint).Add(param.DefaultValue.Pretty())
		}
	}
	t = t.Append(")", StylePunctuation)
	return t
}

func (node UIRNode) ShortName() api.Text { return api.Text{Content: node.GetIdentifier().GetName()} }

func (e ASTEndpoint) Pretty() api.Text {
	t := api.Text{Content: e.String(), Style: StyleEndpoint}.Append("(", StylePunctuation)
	t = t.Add(e.Input.Pretty())
	t = t.Append(") ", StylePunctuation).Append("->", StyleFaint).Add(e.Output.Pretty())
	return t
}

func (p PackageNode) Pretty() api.Text {
	t := api.Text{Content: "package ", Style: StyleKeyword}.Append(p.Package, StyleName)

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
	p = p.Append("class ", StyleKeyword).Append(t.Type, StyleName).Append("{", StylePunctuation).NewLine()

	for _, field := range t.Variables {
		p = p.Append("  ").Add(field.Pretty()).Append(";", StylePunctuation).NewLine()
	}
	for _, method := range t.Methods {
		p = p.NewLine().Add(method.Pretty().Indent(2)).NewLine()
	}
	for _, child := range t.Types {
		p = p.NewLine().Add(child.Pretty().Indent(2)).NewLine()
	}
	p = p.NewLine().Append("}", StylePunctuation)

	return p
}

func (r ASTRecord) Pretty() api.Text {
	p := api.Text{Content: string(r.RecordType), Style: StyleKeyword}.Append(r.Type, StyleName).Append("{", StylePunctuation).NewLine()

	for _, field := range r.Fields {
		p = p.Append("  ").Add(field.Pretty()).Append(";", StylePunctuation).NewLine()
	}
	p = p.Append("}", StylePunctuation)

	return p
}

func (f RecordField) Pretty() api.Text {
	p := api.Text{Content: f.Field, Style: StyleName}.Append(": ", StylePunctuation).Add(f.FieldType.Pretty())
	if f.DefaultValue != nil {
		p = p.Append(" = ", StyleFaint).Add(f.DefaultValue.Pretty())
	}
	return p
}

func (n MethodNode) Pretty() api.Text {
	t := api.Text{Content: "", Style: ""}
	t = t.Append("function ", StyleKeyword).Append(n.Method, StyleName).
		Add(n.Params.Pretty())

	if len(n.Returns) > 0 {
		t = t.Append(" : ", StylePunctuation)
		t = t.Add(n.Returns.Pretty())
	}

	if n.Body != nil {
		t = t.Add(n.Body.Pretty())
	}

	return t
}

func (s IfStmt) Pretty() api.Text {
	t := api.Text{Content: "if ", Style: StyleKeyword}.Add(s.Condition.Pretty()).NewLine().Add(s.Then.Pretty().Indent(2))

	if s.Else != nil {
		t = t.Append(" else ", StyleMuted).NewLine().Add(s.Else.Pretty().Indent(2))
	}
	return t
}

func (s IfStmt) String() string {
	return s.Pretty().ANSI()
}

func (s SwitchStmt) Pretty() api.Text {
	t := api.Text{Content: "switch", Style: StyleKeyword}.Add(s.Value.Pretty())

	for _, c := range s.Cases {
		t = t.NewLine().Append("case ", StyleMuted).Add(c.Condition.Pretty()).Append(": ", StyleMuted).Add(c.Body.Pretty())
	}

	return t
}

func (s SwitchStmt) String() string {
	return s.Pretty().ANSI()
}

func (s ForStmt) Pretty() api.Text {
	return api.Text{Content: "for", Style: StyleKeyword}.Add(s.Init.Pretty()).Append("; ", StyleDim).Add(s.Cond.Pretty()).Append("; ", StyleDim).Add(s.Update.Pretty()).
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
	return s.Target.Pretty().Append(" "+op+" ", StyleOperator).Add(s.Value.Pretty())
}

func (s AssignmentStmt) String() string {
	return s.Pretty().ANSI()
}

func (s CastStmt) Pretty() api.Text {
	return api.Text{Content: "(", Style: StylePunctuation}.Append(s.TargetType, StyleTypeName).Append(")", StylePunctuation).Add(s.Expr.Pretty())
}

func (s CastStmt) String() string {
	return s.Pretty().ANSI()
}

func (s WhileStmt) Pretty() api.Text {
	return api.Text{Content: "while", Style: StyleKeyword}.Add(s.Condition.Pretty()).Add(s.Body.Pretty())
}

func (s WhileStmt) String() string {
	return s.Pretty().ANSI()
}

func (t TypeDeclStmt) Pretty() api.Text {
	p := api.Text{Content: "type ", Style: StyleKeyword}.Append(t.Name, StyleName)
	return p
}

func (t TypeDeclStmt) String() string {
	return t.Pretty().ANSI()
}

func (s ConstDeclStmt) Pretty() api.Text {
	p := api.Text{Content: "const ", Style: StyleKeyword}.Append(s.Field, StyleName)
	if s.FieldType != "" {
		p = p.Append(" ", StyleTextMuted).Append(string(s.FieldType))
	}
	if s.DefaultValue != nil {
		p = p.Append(" = ", StyleTextMuted).Add(s.DefaultValue.Pretty())
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
	t := api.Text{Content: "{", Style: StylePunctuation}.NewLine()
	for _, v := range s.Variables {
		t = t.NewLine().Add(v.Pretty()).Append(";", StylePunctuation).NewLine()
	}
	for _, child := range s.Children {
		t = t.NewLine().Add(child.Pretty().Indent(2))
	}
	t = t.NewLine().Append("}", StylePunctuation)
	return t
}

func (s BlockStmt) String() string {
	return s.Pretty().ANSI()
}

func (s TupleStmt) Pretty() api.Text {
	t := api.Text{Content: "(", Style: StylePunctuation}
	for i, elem := range s.Elements {
		if i > 0 {
			t = t.Append(", ", StylePunctuation)
		}
		t = t.Add(elem.Pretty())
	}
	t = t.Append(")", StylePunctuation)
	return t
}

func (s TupleStmt) String() string {
	return s.Pretty().ANSI()
}

func (s ObjectLiteralStmt) Pretty() api.Text {
	t := api.Text{Content: "{ ", Style: StylePunctuation}
	for i, entry := range s.Entries {
		if i > 0 {
			t = t.Append(", ", StylePunctuation)
		}
		t = t.Append(entry.Key, StyleObjectKey).Append(": ", StylePunctuation).Add(entry.Value.Pretty())
	}
	t = t.Append(" }", StylePunctuation)
	return t
}

func (s ObjectLiteralStmt) String() string {
	return s.Pretty().ANSI()
}

func (s VariableStmt) Pretty() api.Text { return api.Text{Content: s.Name, Style: StyleName} }

func (s VariableStmt) String() string {
	return s.Pretty().ANSI()
}

func (s MethodCallStmt) Pretty() api.Text {
	t := api.Text{Content: ""}
	if s.Method != nil {
		t = t.Add(s.Method.Pretty())
	}
	return t.Add(s.Arguments.Pretty().Wrap("(", ")", StylePunctuation))
}

func (s MethodCallStmt) String() string {
	return s.Pretty().ANSI()
}

// Pretty renders target(args) → A | B, naming each candidate by its identifier.
func (s DispatchCallStmt) Pretty() api.Text {
	t := api.Text{Content: ""}
	if s.Method != nil {
		t = t.Add(s.Method.Pretty())
	}
	t = t.Add(s.Arguments.Pretty().Wrap("(", ")", StylePunctuation))
	for i, candidate := range s.Candidates {
		separator := " | "
		if i == 0 {
			separator = " → "
		}
		t = t.Append(separator, StyleFaint).Add(candidate.GetIdentifier().Pretty())
	}
	return t
}

func (s DispatchCallStmt) String() string {
	return s.Pretty().ANSI()
}

func (r RecordType) Pretty() api.Text {
	return api.Text{Content: ""}.Append(string(r), StyleName)
}

func (r RecordIdentifier) Pretty() api.Text {
	t := api.Text{Content: ""}.Add(r.RecordType.Pretty()).Append("://", StyleFaint)
	if r.EnvironmentIdentifier.String() != "" {
		t = t.Add(r.EnvironmentIdentifier.Pretty()).Append("/", StyleFaint)
	}
	t = t.Append(r.ID, StyleName)
	return t
}

func (s RecordReadStmt) String() string {
	return s.Pretty().ANSI()
}

func (s RecordReadStmt) Pretty() api.Text {
	if s.Expression.Expression != "" {
		if s.ExpressionType == ExpressionTypeSQL {
			return api.Text{Content: "db.Query", Style: StyleQuery}.Add(api.Text{Content: ""}.Add(api.Code{Language: "sql", Content: s.Expression.Expression}).Wrap("(\"", "\")", StylePunctuation))
		}
		return api.Text{Content: ""}.Add(api.Code{Language: string(s.ExpressionType), Content: s.Expression.Expression})
	}
	if s.Record == nil {
		return api.Text{Content: "read", Style: StyleName}.Add(s.Arguments.Pretty().Wrap("(", ")", StylePunctuation))
	}
	return api.Text{Content: ""}.Add(s.Record.Pretty()).Append(".read", StyleName).Add(s.Arguments.Pretty().Wrap("(", ")", StylePunctuation))
}

func (s RecordWriteStmt) Pretty() api.Text {
	if s.Content != nil {
		if s.ExpressionType == ExpressionTypeSQL {
			return api.Text{Content: "db.Update", Style: StyleQuery}.Add(api.Text{Content: ""}.Add(api.Code{Language: "sql", Content: *s.Expression.Content}).Wrap("(\"", "\")", StylePunctuation))
		}
		return api.Text{Content: ""}.Add(api.Code{Language: string(s.ExpressionType), Content: *s.Content})
	}
	if s.Record == nil {
		return api.Text{Content: "write", Style: StyleName}.Add(s.Arguments.Pretty().Wrap("(", ")", StylePunctuation))
	}
	return api.Text{Content: ""}.Add(s.Record.Pretty()).Append(".write", StyleName).Add(s.Arguments.Pretty().Wrap("(", ")", StylePunctuation))
}

func (s RecordWriteStmt) String() string {
	return s.Pretty().ANSI()
}

func (e EnvironmentIdentifier) Pretty() api.Text {
	return api.Text{Content: e.Environment, Style: StyleEnvironment}.Append("/", StyleFaint).Append(e.URN, StyleName)
}

func (e EndpointType) Pretty() api.Text {
	return api.Text{Content: e.String(), Style: StyleEndpointType}
}

func (a Arguments) Pretty() api.Text {
	if len(a) == 0 {
		return api.Text{Content: "", Style: StylePunctuation}
	}
	t := api.Text{Content: ""}
	for i, arg := range a {
		if i > 0 {
			t = t.Append(", ", StylePunctuation)
		}
		if arg.Name != nil {
			t = t.Append(*arg.Name, StyleArgumentName).Append(": ", StylePunctuation)
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
	t = t.Add(s.Arguments.Pretty().Wrap(" (", ")", StylePunctuation))
	return t
}

func (s EndpointCallStmt) String() string {
	return s.Pretty().ANSI()
}

func (v ScopedVariableRef) Pretty() api.Text {
	t := api.Text{Content: ""}
	id := v.Identifier.Pretty()
	if id.String() != "" {
		t = t.Add(id).Append(".", StyleMuted)
	}
	return t.Append(v.Name, NodeTypeFunctionVariable.Color())
}

// Pretty renders the expression's variant. An expression with no variant is one
// its producer could not model, so it prints the source text it kept, and the
// expr placeholder only when there is none.
func (s ExprStmt) Pretty() api.Text {
	if value := s.Value(); value != nil {
		return value.Pretty()
	}
	if s.Content != nil && *s.Content != "" {
		return api.Text{Content: *s.Content}
	}
	return api.Text{Content: "expr", Style: StyleDim}
}

func (s ExprStmt) String() string {
	return s.Pretty().ANSI()
}

func (s ReturnStmt) Pretty() api.Text {
	return api.Text{Content: "return ", Style: StyleControlFlow}.Add(s.Value.Pretty())
}

func (s ReturnStmt) String() string {
	return s.Pretty().ANSI()
}

func (s BreakStmt) Pretty() api.Text { return api.Text{Content: "break", Style: StyleControlFlow} }

func (s BreakStmt) String() string {
	return s.Pretty().ANSI()
}

func (s ContinueStmt) Pretty() api.Text {
	return api.Text{Content: "continue", Style: StyleContinue}
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
	return api.Text{Content: "throw", Style: StyleThrow}.Add(s.Exception.Pretty())
}

func (s ThrowStmt) String() string {
	return s.Pretty().ANSI()
}

func (s TryStmt) Pretty() api.Text {
	return api.Text{Content: "try", Style: StyleTry}.
		Add(s.Body.Pretty().Indent(2)).Append("catch", StyleThrow).Add(s.Catch.Pretty().Indent(2))
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
	theme, found := recordFieldTypeThemes[r]
	if !found {
		theme.style = StyleLiteralOther
	}
	if theme.label == "" {
		theme.label = string(r)
	}
	return api.Text{Content: theme.label, Style: theme.style}
}

func (l Location) Pretty() api.Text {
	var result api.Text

	if l.Path != "" {
		fileName := filepath.Base(l.Path)
		result = api.Text{Content: fileName, Style: StyleFileName}
	}

	if l.EndLine != nil && l.StartLine != nil && *l.EndLine != *l.StartLine {
		if l.Path != "" {
			result = result.Append(":", StyleFaint)
		}
		result = result.Append("L", StyleLineMarker)
		result = result.Append(fmt.Sprintf("%d", l.StartLine), StyleLineNumber)
		result = result.Append("-", StyleFaint)
		result = result.Append(fmt.Sprintf("%d", l.EndLine), StyleLineNumber)
	} else if l.StartLine != nil {
		if l.Path != "" {
			result = result.Append(":", StyleFaint)
		}
		result = result.Append("L", StyleLineMarker)
		result = result.Append(fmt.Sprintf("%d", l.StartLine), StyleLineNumber)
	}

	return result
}

func (r RelationshipType) Pretty() api.Text {
	theme, found := relationshipThemes[r]
	if !found {
		theme = referenceRelationshipTheme
	}
	return api.Text{Content: ""}.Add(theme.icon).Append(theme.label, theme.style)
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
		return api.Text{Content: " other", Style: StyleSecondary}
	}
}

func (f FieldType) Pretty() api.Text {
	theme, found := fieldTypeThemes[f]
	if !found {
		theme.style = fieldTypeDefaultStyle
	}
	if theme.label == "" {
		theme.label = string(f)
	}
	return api.Text{Content: theme.label, Style: theme.style}
}

func (v Value) Pretty() api.Text {
	if v.IsEmpty() {
		return api.Text{}
	}
	p := api.Text{Content: " "}.Add(v.FieldType.Pretty())
	if !v.IsEmpty() {
		p = p.Append(" = ", StyleValueAssign).Add(v.AsText(StyleValue))
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
		return api.Text{Content: "null", Style: StyleDim}
	}
	val := t.String()

	if t.Bool != nil {
		return api.Text{Content: val, Style: StyleTypedBoolean}
	}
	if t.Int != nil || t.Float != nil {
		return api.Text{Content: val, Style: StyleTypedNumber}
	}
	if t.Date != nil {
		return api.Text{Content: val, Style: StyleTypedDate}
	}
	return api.Text{Content: val, Style: StyleTypedString}
}

func (v VariableDeclStmt) Pretty() api.Text {
	p := api.Text{Content: "var ", Style: StyleKeyword}.Append(v.Field)
	if v.FieldType != "" {
		p = p.Append(" ", StyleTextMuted).Append(string(v.FieldType))
	}
	if v.DefaultValue != nil {
		p = p.Append(" = ", StyleTextMuted).Add(v.DefaultValue.Pretty())
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
