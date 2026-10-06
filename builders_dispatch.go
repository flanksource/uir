package uir

// DispatchCallBuilder provides fluent API for building DispatchCallStmt
type DispatchCallBuilder struct {
	DispatchCallStmt
}

// NewDispatchCall starts a call to method, the declared target, whose
// implementation is chosen at run time among the candidates added to it.
func NewDispatchCall(method string, id ...Identifier) *DispatchCallBuilder {
	nodeRef := Identifier{}
	if len(id) > 0 {
		nodeRef = id[0]
	}
	nodeRef.Method = method
	return &DispatchCallBuilder{
		DispatchCallStmt: DispatchCallStmt{
			methodBase: methodBase{
				statementBase: statementBase{Type: ASTStatementTypeDispatchCall},
				Method:        NewRef(nodeRef),
			},
		},
	}
}

func (b *DispatchCallBuilder) WithCandidate(node Node) *DispatchCallBuilder {
	b.Candidates = append(b.Candidates, node)
	return b
}

func (b *DispatchCallBuilder) WithReceiver(expr ExprStmt) *DispatchCallBuilder {
	b.Receiver = &expr
	return b
}

func (b *DispatchCallBuilder) WithArgument(name string, value ExprStmt) *DispatchCallBuilder {
	b.Arguments = append(b.Arguments, struct {
		Name  *string  `json:"name,omitempty"`
		Value ExprStmt `json:"value,omitempty"`
	}{
		Name:  &name,
		Value: value,
	})
	return b
}

func (b *DispatchCallBuilder) WithPositionalArg(value ExprStmt) *DispatchCallBuilder {
	b.Arguments = append(b.Arguments, struct {
		Name  *string  `json:"name,omitempty"`
		Value ExprStmt `json:"value,omitempty"`
	}{
		Value: value,
	})
	return b
}

func (b *DispatchCallBuilder) Build() DispatchCallStmt {
	return b.DispatchCallStmt
}
