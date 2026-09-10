package uir

import "encoding/json"

type VariableDeclStmt struct {
	statementBase `json:",inline"`
	RecordField   `json:",inline"`
}

func (v VariableDeclStmt) GetStatementType() StatementType {
	return ASTStatementTypeDeclareVariable
}

type FunctionDeclStmt struct {
	statementBase `json:",inline"`
	MethodNode    `json:",inline"`
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
	RecordField   `json:",inline"`
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
	Method        Node      `json:",inline"`
	Receiver      *ExprStmt `json:"receiver,omitempty"` // nil if function call
}

func (s MethodCallStmt) GetStatementType() StatementType {
	return ASTStatementTypeCall
}

func (s *MethodCallStmt) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Method may be inlined or nested under "Method"
	var id Identifier
	if methodData, ok := raw["Method"]; ok {
		_ = json.Unmarshal(methodData, &id)
	} else {
		_ = json.Unmarshal(data, &id)
	}
	s.Method = NodeRef{Identifier: id}

	if v, ok := raw["statement_type"]; ok {
		_ = json.Unmarshal(v, &s.Type)
	}
	if v, ok := raw["arguments"]; ok {
		_ = json.Unmarshal(v, &s.Arguments)
	}
	if v, ok := raw["receiver"]; ok {
		_ = json.Unmarshal(v, &s.Receiver)
	}
	return nil
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
	Record        Node `json:",inline"`
}

type RecordReadStmt struct {
	recordBase `json:",inline"`
	Expression `json:",inline"`
	Arguments  Arguments `json:"arguments,omitempty"`
}

func (s RecordReadStmt) GetStatementType() StatementType {
	return ASTStatementTypeRecordRead
}

type RecordWriteStmt struct {
	recordBase `json:",inline"`
	Expression `json:",inline"`
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
