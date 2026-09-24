package uir

import "fmt"

func (s statementBase) Hash() string {
	h := NewHasher("statement")
	h.AddString("statement_type", string(s.Type))
	return h.String()
}

func (stmt UIRStatement) Hash() string {
	return stmt.statementBase.Hash()
}

func (s BlockStmt) Hash() string {
	h := NewHasher("stmt.block")
	h.AddRecordFields("variables", s.Variables)
	h.AddStatements("children", s.Children)
	return h.String()
}

func (s ExprStmt) Hash() string {
	return HashStatement(s)
}

func HashStatement(stmt Statement) string {
	if stmt == nil {
		return NewHasher("stmt.nil").String()
	}
	switch s := stmt.(type) {
	case *ScopedVariableRef:
		return hashScopedVariableRef(*s)
	case *ExprStmt:
		return hashExprStmt(*s)
	case ExprStmt:
		return hashExprStmt(s)
	case *IfStmt:
		return hashIfStmt(*s)
	case IfStmt:
		return hashIfStmt(s)
	case *SwitchStmt:
		return hashSwitchStmt(*s)
	case SwitchStmt:
		return hashSwitchStmt(s)
	case *ForStmt:
		return hashForStmt(*s)
	case ForStmt:
		return hashForStmt(s)
	case *ForEachStmt:
		return hashForEachStmt(*s)
	case ForEachStmt:
		return hashForEachStmt(s)
	case *WhileStmt:
		return hashWhileStmt(*s)
	case WhileStmt:
		return hashWhileStmt(s)
	case *AssignmentStmt:
		return hashAssignmentStmt(*s)
	case AssignmentStmt:
		return hashAssignmentStmt(s)
	case *UnaryStmt:
		return hashUnaryStmt(*s)
	case UnaryStmt:
		return hashUnaryStmt(s)
	case *LiteralStmt:
		return hashLiteralStmt(*s)
	case LiteralStmt:
		return hashLiteralStmt(s)
	case *BinaryStmt:
		return hashBinaryStmt(*s)
	case BinaryStmt:
		return hashBinaryStmt(s)
	case *CastStmt:
		return hashCastStmt(*s)
	case CastStmt:
		return hashCastStmt(s)
	case *TupleStmt:
		return hashTupleStmt(*s)
	case TupleStmt:
		return hashTupleStmt(s)
	case *ObjectLiteralStmt:
		return hashObjectLiteralStmt(*s)
	case ObjectLiteralStmt:
		return hashObjectLiteralStmt(s)
	case *VariableStmt:
		return hashVariableStmt(*s)
	case VariableStmt:
		return hashVariableStmt(s)
	case *MethodCallStmt:
		return hashMethodCallStmt(*s)
	case MethodCallStmt:
		return hashMethodCallStmt(s)
	case *EndpointCallStmt:
		return hashEndpointCallStmt(*s)
	case EndpointCallStmt:
		return hashEndpointCallStmt(s)
	case *RecordReadStmt:
		return hashRecordReadStmt(*s)
	case RecordReadStmt:
		return hashRecordReadStmt(s)
	case *RecordWriteStmt:
		return hashRecordWriteStmt(*s)
	case RecordWriteStmt:
		return hashRecordWriteStmt(s)
	case *ReturnStmt:
		return hashReturnStmt(*s)
	case ReturnStmt:
		return hashReturnStmt(s)
	case *BreakStmt:
		return NewHasher("stmt.break").String()
	case BreakStmt:
		return NewHasher("stmt.break").String()
	case *ContinueStmt:
		return NewHasher("stmt.continue").String()
	case ContinueStmt:
		return NewHasher("stmt.continue").String()
	case *ThrowStmt:
		return hashThrowStmt(*s)
	case ThrowStmt:
		return hashThrowStmt(s)
	case *TryStmt:
		return hashTryStmt(*s)
	case TryStmt:
		return hashTryStmt(s)
	case *ConditionStmt:
		return hashConditionStmt(*s)
	case ConditionStmt:
		return hashConditionStmt(s)
	case *VariableDeclStmt:
		return hashVariableDeclStmt(*s)
	case VariableDeclStmt:
		return hashVariableDeclStmt(s)
	case *FunctionDeclStmt:
		return hashFunctionDeclStmt(*s)
	case FunctionDeclStmt:
		return hashFunctionDeclStmt(s)
	case *TypeDeclStmt:
		return hashTypeDeclStmt(*s)
	case TypeDeclStmt:
		return hashTypeDeclStmt(s)
	case *ConstDeclStmt:
		return hashConstDeclStmt(*s)
	case ConstDeclStmt:
		return hashConstDeclStmt(s)
	case *DocStmt:
		return hashDocStmt(*s)
	case DocStmt:
		return hashDocStmt(s)
	case *TestStmt:
		return hashTestStmt(*s)
	case TestStmt:
		return hashTestStmt(s)
	case *RawStmt:
		return hashRawStmt(*s)
	case RawStmt:
		return hashRawStmt(s)
	case *DestructuringStmt:
		return hashDestructuringStmt(*s)
	case DestructuringStmt:
		return hashDestructuringStmt(s)
	case *TemplateLiteralStmt:
		return hashTemplateLiteralStmt(*s)
	case TemplateLiteralStmt:
		return hashTemplateLiteralStmt(s)
	case interface{ Hash() string }:
		return s.Hash()
	default:
		h := NewHasher("stmt.unknown")
		h.AddString("type", string(stmt.GetStatementType()))
		h.AddString("signature", stmt.GetSignature())
		return h.String()
	}
}

func hashExprStmt(s ExprStmt) string {
	h := NewHasher("stmt.expr")
	if s.Literal != nil {
		h.AddString("literal", HashStatement(*s.Literal))
	}
	if s.Variable != nil {
		h.AddString("variable", hashScopedVariableRef(*s.Variable))
	}
	if s.Binary != nil {
		h.AddString("binary", HashStatement(*s.Binary))
	}
	if s.Cast != nil {
		h.AddString("cast", HashStatement(*s.Cast))
	}
	if s.Unary != nil {
		h.AddString("unary", HashStatement(*s.Unary))
	}
	if s.MethodCall != nil {
		h.AddString("method_call", HashStatement(*s.MethodCall))
	}
	if s.EndpointCall != nil {
		h.AddString("endpoint_call", HashStatement(*s.EndpointCall))
	}
	if s.RecordRead != nil {
		h.AddString("record_read", HashStatement(*s.RecordRead))
	}
	if s.Tuple != nil {
		h.AddString("tuple", HashStatement(*s.Tuple))
	}
	if s.ObjectLiteral != nil {
		h.AddString("object_literal", HashStatement(*s.ObjectLiteral))
	}
	return h.String()
}

func hashIfStmt(s IfStmt) string {
	h := NewHasher("stmt.if")
	h.AddString("condition", HashStatement(s.Condition))
	h.AddString("then", s.Then.Hash())
	if s.Else != nil {
		h.AddString("else", s.Else.Hash())
	}
	return h.String()
}

func hashSwitchStmt(s SwitchStmt) string {
	h := NewHasher("stmt.switch")
	h.AddString("value", HashStatement(s.Value))
	h.AddInt("cases.len", len(s.Cases))
	for i, c := range s.Cases {
		h.AddString(fmt.Sprintf("cases.%d.condition", i), HashStatement(c.Condition))
		h.AddString(fmt.Sprintf("cases.%d.body", i), c.Body.Hash())
	}
	return h.String()
}

func hashForStmt(s ForStmt) string {
	h := NewHasher("stmt.for")
	h.AddString("init", HashStatement(s.Init))
	h.AddString("cond", HashStatement(s.Cond))
	h.AddString("body", s.Body.Hash())
	h.AddString("update", HashStatement(s.Update))
	return h.String()
}

func hashForEachStmt(s ForEachStmt) string {
	h := NewHasher("stmt.foreach")
	h.AddString("variable", HashStatement(s.Variable))
	h.AddString("iterable", HashStatement(s.Iterable))
	h.AddString("body", s.Body.Hash())
	return h.String()
}

func hashWhileStmt(s WhileStmt) string {
	h := NewHasher("stmt.while")
	h.AddString("condition", HashStatement(s.Condition))
	h.AddString("body", s.Body.Hash())
	return h.String()
}

func hashAssignmentStmt(s AssignmentStmt) string {
	h := NewHasher("stmt.assignment")
	h.AddString("target", hashScopedVariableRef(s.Target))
	h.AddString("value", HashStatement(s.Value))
	h.AddString("op", string(s.Op))
	h.AddBool("is_declaration", s.IsDeclaration)
	return h.String()
}

func hashUnaryStmt(s UnaryStmt) string {
	h := NewHasher("stmt.unary")
	h.AddString("op", string(s.Operator))
	h.AddString("operand", HashStatement(s.Operand))
	return h.String()
}

func hashLiteralStmt(s LiteralStmt) string {
	h := NewHasher("stmt.literal")
	h.AddStringPtr("value", s.Value)
	h.AddString("field_type", string(s.FieldType))
	return h.String()
}

func hashBinaryStmt(s BinaryStmt) string {
	h := NewHasher("stmt.binary")
	h.AddString("left", HashStatement(s.Left))
	h.AddString("op", string(s.Operator))
	h.AddString("right", HashStatement(s.Right))
	return h.String()
}

func hashCastStmt(s CastStmt) string {
	h := NewHasher("stmt.cast")
	h.AddString("expr", HashStatement(s.Expr))
	h.AddString("target_type", s.TargetType)
	return h.String()
}

func hashTupleStmt(s TupleStmt) string {
	h := NewHasher("stmt.tuple")
	h.AddInt("elements.len", len(s.Elements))
	for i, elem := range s.Elements {
		h.AddString(fmt.Sprintf("elements.%d", i), HashStatement(elem))
	}
	return h.String()
}

func hashObjectLiteralStmt(s ObjectLiteralStmt) string {
	h := NewHasher("stmt.object_literal")
	h.AddInt("entries.len", len(s.Entries))
	for i, entry := range s.Entries {
		h.AddString(fmt.Sprintf("entries.%d.key", i), entry.Key)
		h.AddString(fmt.Sprintf("entries.%d.value", i), HashStatement(entry.Value))
	}
	return h.String()
}

func hashVariableStmt(s VariableStmt) string {
	h := NewHasher("stmt.variable")
	h.AddString("name", s.Name)
	return h.String()
}

func hashMethodCallStmt(s MethodCallStmt) string {
	h := NewHasher("stmt.method_call")
	if s.Method != nil {
		h.AddString("method", s.Method.GetIdentifier().SymbolKey())
	}
	if s.Receiver != nil {
		h.AddString("receiver", HashStatement(*s.Receiver))
	}
	h.AddString("arguments", hashArguments(s.Arguments))
	h.AddBool("is_constructor", s.IsConstructor)
	return h.String()
}

func hashEndpointCallStmt(s EndpointCallStmt) string {
	h := NewHasher("stmt.endpoint_call")
	if s.Endpoint != nil {
		h.AddString("endpoint", s.Endpoint.GetIdentifier().SymbolKey())
	}
	h.AddString("arguments", hashArguments(s.Arguments))
	return h.String()
}

func hashRecordReadStmt(s RecordReadStmt) string {
	h := NewHasher("stmt.record_read")
	if s.Record != nil {
		h.AddString("record", s.Record.GetIdentifier().SymbolKey())
	}
	h.AddString("record_type", string(s.RecordType))
	h.AddString("arguments", hashArguments(s.Arguments))
	h.AddString("expression_type", string(s.ExpressionType))
	h.AddString("expression", s.Expression.Expression)
	return h.String()
}

func hashRecordWriteStmt(s RecordWriteStmt) string {
	h := NewHasher("stmt.record_write")
	if s.Record != nil {
		h.AddString("record", s.Record.GetIdentifier().SymbolKey())
	}
	h.AddString("record_type", string(s.RecordType))
	h.AddString("arguments", hashArguments(s.Arguments))
	h.AddString("expression_type", string(s.ExpressionType))
	h.AddString("expression", s.Expression.Expression)
	return h.String()
}

func hashReturnStmt(s ReturnStmt) string {
	h := NewHasher("stmt.return")
	h.AddString("value", HashStatement(s.Value))
	return h.String()
}

func hashThrowStmt(s ThrowStmt) string {
	h := NewHasher("stmt.throw")
	h.AddString("exception", HashStatement(s.Exception))
	return h.String()
}

func hashTryStmt(s TryStmt) string {
	h := NewHasher("stmt.try")
	h.AddString("body", s.Body.Hash())
	h.AddString("catch", s.Catch.Hash())
	return h.String()
}

func hashConditionStmt(s ConditionStmt) string {
	h := NewHasher("stmt.condition")
	h.AddString("expr", HashStatement(s.Expr))
	return h.String()
}

func hashVariableDeclStmt(s VariableDeclStmt) string {
	h := NewHasher("stmt.variable_decl")
	h.AddString("field", s.RecordField.Hash())
	return h.String()
}

func hashFunctionDeclStmt(s FunctionDeclStmt) string {
	h := NewHasher("stmt.function_decl")
	h.AddString("method", s.MethodNode.Hash())
	return h.String()
}

func hashTypeDeclStmt(s TypeDeclStmt) string {
	h := NewHasher("stmt.type_decl")
	h.AddString("name", s.Name)
	return h.String()
}

func hashConstDeclStmt(s ConstDeclStmt) string {
	h := NewHasher("stmt.const_decl")
	h.AddString("field", s.RecordField.Hash())
	return h.String()
}

func hashDocStmt(s DocStmt) string {
	h := NewHasher("stmt.doc")
	h.AddString("doc_type", string(s.DocType))
	h.AddString("content", s.Content)
	h.AddString("style", s.Style)
	h.AddInt("children.len", len(s.Children))
	for i, child := range s.Children {
		h.AddString(fmt.Sprintf("children.%d", i), hashDocStmt(child))
	}
	return h.String()
}

func hashTestStmt(s TestStmt) string {
	h := NewHasher("stmt.test")
	h.AddString("name", s.Name)
	h.AddString("body", s.Hash())
	return h.String()
}

func hashRawStmt(s RawStmt) string {
	h := NewHasher("stmt.raw")
	h.AddString("source", s.Source)
	h.AddString("language", s.Language)
	return h.String()
}

func hashDestructuringStmt(s DestructuringStmt) string {
	h := NewHasher("stmt.destructure")
	h.AddBool("is_array", s.IsArray)
	h.AddInt("bindings.len", len(s.Bindings))
	for i, binding := range s.Bindings {
		h.AddString(fmt.Sprintf("bindings.%d.name", i), binding.Name)
		h.AddString(fmt.Sprintf("bindings.%d.alias", i), binding.Alias)
		if binding.Default != nil {
			h.AddString(fmt.Sprintf("bindings.%d.default", i), HashStatement(*binding.Default))
		}
		if binding.Nested != nil {
			h.AddString(fmt.Sprintf("bindings.%d.nested", i), hashDestructuringStmt(*binding.Nested))
		}
	}
	h.AddStringPtr("rest", s.Rest)
	h.AddString("value", HashStatement(s.Value))
	h.AddString("declare", s.Declare)
	return h.String()
}

func hashTemplateLiteralStmt(s TemplateLiteralStmt) string {
	h := NewHasher("stmt.template")
	h.AddStringSlice("strings", s.Strings)
	h.AddInt("expressions.len", len(s.Expressions))
	for i, expr := range s.Expressions {
		h.AddString(fmt.Sprintf("expressions.%d", i), HashStatement(expr))
	}
	if s.Tag != nil {
		h.AddString("tag", HashStatement(*s.Tag))
	}
	return h.String()
}
