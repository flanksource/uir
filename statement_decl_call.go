package uir

// The declaration statements nest their declared node under its own key: the
// statement and the node each carry Metadata (and the node its own Identifier),
// and flattened into one object the node's copy was shadowed and lost.
type VariableDeclStmt struct {
	statementBase `json:",inline"`
	RecordField   `json:"field"`
}

func (v VariableDeclStmt) GetStatementType() StatementType {
	return ASTStatementTypeDeclareVariable
}

type FunctionDeclStmt struct {
	statementBase `json:",inline"`
	MethodNode    `json:"function"`
}

func (f FunctionDeclStmt) GetStatementType() StatementType {
	return ASTStatementTypeDeclareFunction
}

type TypeDeclStmt struct {
	statementBase `json:",inline"`
	Name          string `json:"name,omitempty"`
}

func (s TypeDeclStmt) GetStatementType() StatementType {
	return ASTStatementTypeDeclareType
}

type ConstDeclStmt struct {
	statementBase `json:",inline"`
	RecordField   `json:"field"`
}

func (s ConstDeclStmt) GetStatementType() StatementType {
	return ASTStatementTypeDeclareConstant
}

type MethodCallStmt struct {
	methodBase `json:",inline"`
	Arguments  Arguments `json:"arguments,omitempty"`
	// IsConstructor signals the language emitter that this call should be
	// rendered as a constructor invocation (e.g. `new X(...)` in TS). When
	// false (default), the call is a free function or method invocation.
	// Language emitters that lack a `new` concept ignore this flag.
	IsConstructor bool `json:"is_constructor,omitempty"`
}

func (method MethodCallStmt) GetSignature() string {
	return method.GetLocation().GetSignature()
}

type methodBase struct {
	statementBase `json:",inline"`
	// Method is encoded under the "Method" key by MethodCallStmt's codec.
	Method   Node      `json:",inline"`
	Receiver *ExprStmt `json:"receiver,omitempty"` // nil if function call
}

func (s MethodCallStmt) GetStatementType() StatementType {
	return ASTStatementTypeCall
}

type EndpointCallStmt struct {
	statementBase
	Endpoint  Node      `json:"endpoint,omitempty"`
	Arguments Arguments `json:"arguments,omitempty"`
}

type Arguments []struct {
	Name  *string  `json:"name,omitempty"` // nil if positional argument
	Value ExprStmt `json:"value,omitempty"`
}

func (args Arguments) GetSignature() string {
	sig := "("
	for i, arg := range args {
		if i > 0 {
			sig += ", "
		}
		if arg.Name != nil {
			sig += *arg.Name + ": "
		} else {
			sig += "arg"
		}
	}
	sig += ")"
	return sig
}

type recordBase struct {
	statementBase `json:",inline"`
	// Record is encoded under the "Record" key by the read/write codecs.
	Record Node `json:",inline"`
	// RecordType is the kind of record read or written (table, view, ...), which a
	// Record held as a bare NodeRef cannot say.
	RecordType RecordType `json:"recordType,omitempty"`
}

// RecordReadStmt and RecordWriteStmt nest their Expression under "expression":
// flattened, its SourceCode shadowed the statement's own location and source.
type RecordReadStmt struct {
	recordBase `json:",inline"`
	Expression `json:"expression"`
	Arguments  Arguments `json:"arguments,omitempty"`
}

func (s RecordReadStmt) GetStatementType() StatementType {
	return ASTStatementTypeRecordRead
}

type RecordWriteStmt struct {
	recordBase `json:",inline"`
	Expression `json:"expression"`
	Arguments  Arguments `json:"arguments,omitempty"`
}

func (s RecordWriteStmt) GetStatementType() StatementType {
	return ASTStatementTypeRecordWrite
}

func (s EndpointCallStmt) GetStatementType() StatementType {
	return ASTStatementTypeCallAPI
}

func (r MethodCallStmt) GetRelationships() []Relationship {
	return []Relationship{
		NewRelationship(RelationshipTypeCall, nil, r.Method).Build(),
	}
}

func (r RecordReadStmt) GetRelationships() []Relationship {
	return []Relationship{NewRelationship(RelationshipTypeRead, nil, r.Record).Build()}
}

func (r RecordWriteStmt) GetRelationships() []Relationship {
	return []Relationship{NewRelationship(RelationshipTypeWrite, nil, r.Record).Build()}
}

func (e EndpointCallStmt) GetRelationships() []Relationship {
	return []Relationship{NewRelationship(RelationshipTypeCall, nil, e.Endpoint).Build()}
}

// Declaration Statements
func (s VariableDeclStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	return "var " + s.Label
}

func (s FunctionDeclStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	return "func " + s.MethodNode.GetIdentifier().Method
}

func (s TypeDeclStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	return "type " + s.Name
}

func (s ConstDeclStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	return "const " + s.Label
}

// Method and Endpoint Calls - Update existing implementation
func (s EndpointCallStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	if s.Endpoint != nil {
		return "endpoint:" + s.Endpoint.GetIdentifier().String()
	}
	return "endpoint"
}

// Record Operations
func (s RecordReadStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	if s.Record != nil {
		return "read:" + s.Record.GetIdentifier().String()
	}
	return "read"
}

func (s RecordWriteStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	if s.Record != nil {
		return "write:" + s.Record.GetIdentifier().String()
	}
	return "write"
}
