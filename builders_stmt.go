package uir

// PackageBuilder provides fluent API for building PackageNode
func (b *MethodCallBuilder) WithSourceCode(text string) *MethodCallBuilder {
	b.Content = &text
	return b
}

func (b MethodCallBuilder) WithSource(path string, start, end int) *MethodCallBuilder {
	if end == 0 {
		end = start
	}
	b.Location = Location{
		Path:      path,
		StartLine: &start,
		EndLine:   &end,
	}
	return &b
}

// BlockBuilder provides fluent API for building BlockStmt
type BlockBuilder struct {
	stmt BlockStmt
}

func NewBlock() *BlockBuilder {
	return &BlockBuilder{
		stmt: BlockStmt{
			statementBase: statementBase{Type: ASTStatementTypeBlock},
		},
	}
}

func (b *BlockBuilder) WithStatement(stmt Statement) *BlockBuilder {
	b.stmt.Children = append(b.stmt.Children, stmt)
	return b
}

func (b *BlockBuilder) WithStatements(stmts ...Statement) *BlockBuilder {
	b.stmt.Children = append(b.stmt.Children, stmts...)
	return b
}

func (b *BlockBuilder) WithVariable(field RecordField) *BlockBuilder {
	b.stmt.Variables = append(b.stmt.Variables, field)
	return b
}

func (b *BlockBuilder) Build() BlockStmt {
	return b.stmt
}

// IfBuilder provides fluent API for building IfStmt
type IfBuilder struct {
	stmt IfStmt
}

func NewIf(condition ExprStmt) *IfBuilder {
	return &IfBuilder{
		stmt: IfStmt{
			statementBase: statementBase{Type: ASTStatementTypeIf},
			Condition:     ConditionStmt{Expr: condition},
		},
	}
}

func (b *IfBuilder) WithThen(block BlockStmt) *IfBuilder {
	b.stmt.Then = block
	return b
}

func (b *IfBuilder) WithElse(block BlockStmt) *IfBuilder {
	b.stmt.Else = &block
	return b
}

func (b *IfBuilder) Build() IfStmt {
	return b.stmt
}

// AssignmentBuilder provides fluent API for building AssignmentStmt
type AssignmentBuilder struct {
	stmt AssignmentStmt
}

func NewAssignment(target string, value ExprStmt) *AssignmentBuilder {
	return &AssignmentBuilder{
		stmt: AssignmentStmt{
			statementBase: statementBase{Type: ASTStatementTypeAssignment},
			Target:        Var(target),
			Value:         value,
			Op:            AssignmentOpAssign,
		},
	}
}

func (b *AssignmentBuilder) WithOp(op AssignmentOp) *AssignmentBuilder {
	b.stmt.Op = op
	return b
}

// AsDeclaration marks the assignment as the first introduction of its target
// in the enclosing scope. TS emitter renders `let X = expr;` instead of `X = expr;`.
func (b *AssignmentBuilder) AsDeclaration() *AssignmentBuilder {
	b.stmt.IsDeclaration = true
	return b
}

func (b *AssignmentBuilder) Build() AssignmentStmt {
	return b.stmt
}

// MethodCallBuilder provides fluent API for building MethodCallStmt
type MethodCallBuilder struct {
	MethodCallStmt
}

func (b MethodCallBuilder) AddTo(method *MethodNode) *MethodNode {
	if method.Body == nil {
		method.Body = &BlockStmt{}
	}
	method.Body.Children = append(method.Body.Children, b.MethodCallStmt)
	return method
}

func NewMethodCall(method string, id ...Identifier) *MethodCallBuilder {
	nodeRef := Identifier{}
	if len(id) > 0 {
		nodeRef = id[0]
	}
	nodeRef.Method = method
	return &MethodCallBuilder{
		MethodCallStmt: MethodCallStmt{
			methodBase: methodBase{
				statementBase: statementBase{Type: ASTStatementTypeCall},
				Method:        NewRef(nodeRef),
			},
		},
	}
}

type CommentBuilder struct {
	comment Comment
}

func NewComment(text string) *CommentBuilder {
	return &CommentBuilder{
		comment: Comment{
			Text: text,
		},
	}
}

func (c *CommentBuilder) Build() Comment {
	return c.comment
}

func (b *MethodCallBuilder) WithReceiver(expr ExprStmt) *MethodCallBuilder {
	b.Receiver = &expr
	return b
}

func (b *MethodCallBuilder) WithArgument(name string, value ExprStmt) *MethodCallBuilder {
	b.Arguments = append(b.Arguments, struct {
		Name  *string  `json:"name,omitempty"`
		Value ExprStmt `json:"value,omitempty"`
	}{
		Name:  &name,
		Value: value,
	})
	return b
}

func (b *MethodCallBuilder) WithPositionalArg(value ExprStmt) *MethodCallBuilder {
	b.Arguments = append(b.Arguments, struct {
		Name  *string  `json:"name,omitempty"`
		Value ExprStmt `json:"value,omitempty"`
	}{
		Value: value,
	})
	return b
}

// AsConstructor marks the call as a constructor invocation. Language
// emitters that distinguish constructors from function calls (e.g. TS
// `new X(...)`) honour this opt-in. The default is false: a MethodCall
// is a free function/method call unless this is set.
func (b *MethodCallBuilder) AsConstructor() *MethodCallBuilder {
	b.IsConstructor = true
	return b
}

func (b *MethodCallBuilder) Build() MethodCallStmt {
	return b.MethodCallStmt
}

// EndpointCallBuilder provides fluent API for building EndpointCallStmt
type EndpointCallBuilder struct {
	stmt EndpointCallStmt
}

func NewEndpointCall(endpointType EndpointType, endpoint Node) *EndpointCallBuilder {
	return &EndpointCallBuilder{
		stmt: EndpointCallStmt{
			statementBase: statementBase{Type: ASTStatementTypeCallAPI},
			Endpoint:      endpoint,
		},
	}
}

func (b *EndpointCallBuilder) WithArgument(name string, value ExprStmt) *EndpointCallBuilder {
	b.stmt.Arguments = append(b.stmt.Arguments, struct {
		Name  *string  `json:"name,omitempty"`
		Value ExprStmt `json:"value,omitempty"`
	}{
		Name:  &name,
		Value: value,
	})
	return b
}

func (b *EndpointCallBuilder) WithPositionalArg(value ExprStmt) *EndpointCallBuilder {
	b.stmt.Arguments = append(b.stmt.Arguments, struct {
		Name  *string  `json:"name,omitempty"`
		Value ExprStmt `json:"value,omitempty"`
	}{
		Value: value,
	})
	return b
}

func (b *EndpointCallBuilder) Build() EndpointCallStmt {
	return b.stmt
}

// RecordReadBuilder provides fluent API for building RecordReadStmt
type RecordReadBuilder struct {
	stmt RecordReadStmt
}

func NewRecordRead(recordType RecordType, record Node) *RecordReadBuilder {
	return &RecordReadBuilder{
		stmt: RecordReadStmt{
			recordBase: recordBase{
				statementBase: statementBase{Type: ASTStatementTypeRecordRead},
				Record:        record,
				RecordType:    recordType,
			},
		},
	}
}

func (b *RecordReadBuilder) WithExpression(exprType ExpressionType, expr string) *RecordReadBuilder {
	b.stmt.Expression = Expression{
		ExpressionType: exprType,
		Expression:     expr,
	}
	return b
}

func (b *RecordReadBuilder) WithArgument(name string, value ExprStmt) *RecordReadBuilder {
	b.stmt.Arguments = append(b.stmt.Arguments, struct {
		Name  *string  `json:"name,omitempty"`
		Value ExprStmt `json:"value,omitempty"`
	}{
		Name:  &name,
		Value: value,
	})
	return b
}

func (b *RecordReadBuilder) Build() *RecordReadStmt {
	return &b.stmt
}

// RecordWriteBuilder provides fluent API for building RecordWriteStmt
type RecordWriteBuilder struct {
	stmt RecordWriteStmt
}

func NewRecordWrite(recordType RecordType, record Node) *RecordWriteBuilder {
	return &RecordWriteBuilder{
		stmt: RecordWriteStmt{
			recordBase: recordBase{
				statementBase: statementBase{Type: ASTStatementTypeRecordWrite},
				Record:        record,
				RecordType:    recordType,
			},
		},
	}
}

func (b *RecordWriteBuilder) WithArgument(name string, value ExprStmt) *RecordWriteBuilder {
	b.stmt.Arguments = append(b.stmt.Arguments, struct {
		Name  *string  `json:"name,omitempty"`
		Value ExprStmt `json:"value,omitempty"`
	}{
		Name:  &name,
		Value: value,
	})
	return b
}

func (b *RecordWriteBuilder) WithExpression(exprType ExpressionType, expr string) *RecordWriteBuilder {
	b.stmt.Expression = Expression{
		ExpressionType: exprType,
		Expression:     expr,
	}
	return b
}

func (b *RecordWriteBuilder) Build() RecordWriteStmt {
	return b.stmt
}

// ForBuilder provides fluent API for building ForStmt
type ForBuilder struct {
	stmt ForStmt
}

func NewFor() *ForBuilder {
	return &ForBuilder{
		stmt: ForStmt{
			statementBase: statementBase{Type: ASTStatementTypeLoopFor},
		},
	}
}

func (b *ForBuilder) WithInit(init AssignmentStmt) *ForBuilder {
	b.stmt.Init = init
	return b
}

func (b *ForBuilder) WithCondition(cond ConditionStmt) *ForBuilder {
	b.stmt.Cond = cond
	return b
}

func (b *ForBuilder) WithUpdate(update ExprStmt) *ForBuilder {
	b.stmt.Update = update
	return b
}

func (b *ForBuilder) WithBody(body BlockStmt) *ForBuilder {
	b.stmt.Body = body
	return b
}

func (b *ForBuilder) Build() ForStmt {
	return b.stmt
}

// ForEachBuilder provides fluent API for building ForEachStmt
type ForEachBuilder struct {
	stmt ForEachStmt
}

func NewForEach() *ForEachBuilder {
	return &ForEachBuilder{
		stmt: ForEachStmt{
			statementBase: statementBase{Type: ASTStatementTypeLoopForEach},
		},
	}
}

func (b *ForEachBuilder) WithVariable(variable AssignmentStmt) *ForEachBuilder {
	b.stmt.Variable = variable
	return b
}

func (b *ForEachBuilder) WithIterable(iterable ExprStmt) *ForEachBuilder {
	b.stmt.Iterable = iterable
	return b
}

func (b *ForEachBuilder) WithBody(body BlockStmt) *ForEachBuilder {
	b.stmt.Body = body
	return b
}

func (b *ForEachBuilder) Build() ForEachStmt {
	return b.stmt
}

// WhileBuilder provides fluent API for building WhileStmt
type WhileBuilder struct {
	stmt WhileStmt
}

func NewWhile(condition ConditionStmt) *WhileBuilder {
	return &WhileBuilder{
		stmt: WhileStmt{
			statementBase: statementBase{Type: ASTStatementTypeLoopWhile},
			Condition:     condition,
		},
	}
}

func (b *WhileBuilder) WithBody(body BlockStmt) *WhileBuilder {
	b.stmt.Body = body
	return b
}

func (b *WhileBuilder) Build() WhileStmt {
	return b.stmt
}

// SwitchBuilder provides fluent API for building SwitchStmt
type SwitchBuilder struct {
	stmt SwitchStmt
}

func NewSwitch(value ExprStmt) *SwitchBuilder {
	return &SwitchBuilder{
		stmt: SwitchStmt{
			statementBase: statementBase{Type: ASTStatementTypeSwitch},
			Value:         value,
		},
	}
}

func (b *SwitchBuilder) WithCase(condition ConditionStmt, body BlockStmt) *SwitchBuilder {
	b.stmt.Cases = append(b.stmt.Cases, struct {
		Condition ConditionStmt `json:"condition,omitempty"`
		Body      BlockStmt     `json:"body,omitempty"`
	}{
		Condition: condition,
		Body:      body,
	})
	return b
}

func (b *SwitchBuilder) Build() SwitchStmt {
	return b.stmt
}

// TryBuilder provides fluent API for building TryStmt
type TryBuilder struct {
	stmt TryStmt
}

func NewTry() *TryBuilder {
	return &TryBuilder{
		stmt: TryStmt{
			statementBase: statementBase{Type: ASTStatementTypeTry},
		},
	}
}

func (b *TryBuilder) WithBody(body BlockStmt) *TryBuilder {
	b.stmt.Body = body
	return b
}

func (b *TryBuilder) WithCatch(catch BlockStmt) *TryBuilder {
	b.stmt.Catch = catch
	return b
}

func (b *TryBuilder) Build() TryStmt {
	return b.stmt
}
