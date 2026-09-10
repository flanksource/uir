package uir

// PackageBuilder provides fluent API for building PackageNode
// Var creates a variable reference
func Var(name string) ScopedVariableRef {
	return ScopedVariableRef{
		VariableRef: VariableRef{Name: name},
	}
}

// VarExpr creates a variable reference expression
func VarExpr(name string) ExprStmt {
	v := Var(name)
	return ExprStmt{
		statementBase: statementBase{Type: ASTStatementTypeExpression},
		Variable:      &v,
	}
}

// ObjectExpr creates an inline object-literal expression from ordered
// key/value entries, e.g. `{ "service.id": ServiceID, Country: Country }`.
// Use this instead of VarExpr for object literals — VarExpr names are
// sanitized into identifiers by emitters, which mangles the literal.
func ObjectExpr(entries ...ObjectLiteralEntry) ExprStmt {
	return ExprStmt{
		statementBase: statementBase{Type: ASTStatementTypeExpression},
		ObjectLiteral: &ObjectLiteralStmt{
			statementBase: statementBase{Type: ASTStatementTypeObjectLiteral},
			Entries:       entries,
		},
	}
}

// ObjectEntry creates a single key/value pair for ObjectExpr.
func ObjectEntry(key string, value ExprStmt) ObjectLiteralEntry {
	return ObjectLiteralEntry{Key: key, Value: value}
}

// Literal creates a literal statement
func Literal(value string, fieldType RecordFieldType) LiteralStmt {
	return LiteralStmt{
		statementBase: statementBase{Type: ASTStatementTypeLiteral},
		Value:         &value,
		FieldType:     fieldType,
	}
}

// StringLit creates a string literal
func StringLit(s string) LiteralStmt {
	return Literal(s, RecordFieldTypeString)
}

// IntLit creates an integer literal
func IntLit(i int) LiteralStmt {
	val := string(rune(i))
	return Literal(val, RecordFieldTypeNumber)
}

// BoolLit creates a boolean literal
func BoolLit(b bool) LiteralStmt {
	val := "false"
	if b {
		val = "true"
	}
	return Literal(val, RecordFieldTypeBoolean)
}

// LitExpr creates a literal expression
func LitExpr(value string, fieldType RecordFieldType) ExprStmt {
	lit := Literal(value, fieldType)
	return ExprStmt{
		statementBase: statementBase{Type: ASTStatementTypeExpression},
		Literal:       &lit,
	}
}

// Field creates a record field
func Field(name string, fieldType RecordFieldType) RecordField {
	val := RecordField{
		FieldType: fieldType,
	}
	val.Field = name
	val.NodeType = NodeTypeField
	return val
}

// NewCondition creates a condition statement
func NewCondition(expr ExprStmt) ConditionStmt {
	return ConditionStmt{
		statementBase: statementBase{Type: ASTStatmentTypeCondition},
		Expr:          expr,
	}
}

// BinaryExpr creates a binary expression
func BinaryExpr(left ExprStmt, op BinaryOp, right ExprStmt) ExprStmt {
	binary := BinaryStmt{
		statementBase: statementBase{Type: ASTStatementTypeBinary},
		Left:          left,
		Operator:      op,
		Right:         right,
	}
	return ExprStmt{
		statementBase: statementBase{Type: ASTStatementTypeExpression},
		Binary:        &binary,
	}
}

// UnaryExpr creates a unary expression
func UnaryExpr(op UnaryOp, operand ExprStmt) ExprStmt {
	unary := UnaryStmt{
		statementBase: statementBase{Type: ASTStatementTypeUnary},
		Operator:      op,
		Operand:       operand,
	}
	return ExprStmt{
		statementBase: statementBase{Type: ASTStatementTypeExpression},
		Unary:         &unary,
	}
}

// CastExpr creates a cast expression
func CastExpr(expr ExprStmt, targetType string) ExprStmt {
	cast := CastStmt{
		statementBase: statementBase{Type: ASTStatementTypeCast},
		Expr:          expr,
		TargetType:    targetType,
	}
	return ExprStmt{
		statementBase: statementBase{Type: ASTStatementTypeExpression},
		Cast:          &cast,
	}
}

// NewReturn creates a return statement
func NewReturn(value ExprStmt) ReturnStmt {
	return ReturnStmt{
		statementBase: statementBase{Type: ASTStatementTypeReturn},
		Value:         value,
	}
}

// NewBreak creates a break statement
func NewBreak() BreakStmt {
	return BreakStmt{
		statementBase: statementBase{Type: ASTStatementTypeBreak},
	}
}

// NewContinue creates a continue statement
func NewContinue() ContinueStmt {
	return ContinueStmt{
		statementBase: statementBase{Type: ASTStatementTypeContinue},
	}
}

// NewThrow creates a throw statement
func NewThrow(exception ExprStmt) ThrowStmt {
	return ThrowStmt{
		statementBase: statementBase{Type: ASTStatementTypeThrow},
		Exception:     exception,
	}
}
