package uir

type AssignmentStmt struct {
	statementBase `json:",inline"`
	Target        ScopedVariableRef `json:"target,omitempty"`
	Value         ExprStmt          `json:"value,omitempty"`
	Op            AssignmentOp      `json:"op,omitempty"`
	// IsDeclaration marks this assignment as the first introduction of the
	// binding in its scope. Language emitters render it as a declaration
	// (`let X = expr;` in TypeScript, `var X := expr` in Go-ish, etc.) instead
	// of a plain assignment. Op stays as AssignmentOpAssign.
	IsDeclaration bool `json:"isDeclaration,omitempty"`
}

func (s AssignmentStmt) GetRelationships() []Relationship {

	return []Relationship{NewRelationship(RelationshipTypeWrite, nil, s.Target.AsRef()).Build()}
}

func (s AssignmentStmt) GetStatementType() StatementType {
	return ASTStatementTypeAssignment
}

type UnaryStmt struct {
	statementBase `json:",inline"`
	Operator      UnaryOp  `json:"op,omitempty"`
	Operand       ExprStmt `json:"operand,omitempty"`
}

func (s UnaryStmt) GetStatementType() StatementType {
	return ASTStatementTypeUnary
}

type LiteralStmt struct {
	statementBase `json:",inline"`
	Value         *string         `json:"value,omitempty"`
	FieldType     RecordFieldType `json:"fieldType,omitempty"`
}

func (s LiteralStmt) GetStatementType() StatementType {
	return ASTStatementTypeLiteral
}

type BinaryStmt struct {
	statementBase `json:",inline"`
	Left          ExprStmt `json:"left,omitempty"`
	Operator      BinaryOp `json:"op,omitempty"`
	Right         ExprStmt `json:"right,omitempty"`
}

func (s BinaryStmt) GetStatementType() StatementType {
	return ASTStatementTypeBinary
}

type CastStmt struct {
	statementBase `json:",inline"`
	Expr          ExprStmt `json:"expr,omitempty"`
	TargetType    string   `json:"targetType,omitempty"`
}

func (s CastStmt) GetStatementType() StatementType {
	return ASTStatementTypeCast
}

type TupleStmt struct {
	statementBase `json:",inline"`
	Elements      []ExprStmt `json:"elements,omitempty"`
}

func (s TupleStmt) GetStatementType() StatementType {
	return ASTStatementTypeTuple
}

// ObjectLiteralStmt is an inline object literal expression, e.g. the TS
// `{ "service.id": ServiceID, Country: Country }`. Entries preserve insertion
// order; keys are quoted by the target emitter only when they are not bare
// identifiers. Distinct from TupleStmt (array literal) and from a sanitized
// variable reference — see ExprStmt.ObjectLiteral.
type ObjectLiteralStmt struct {
	statementBase `json:",inline"`
	Entries       []ObjectLiteralEntry `json:"entries,omitempty"`
}

func (s ObjectLiteralStmt) GetStatementType() StatementType {
	return ASTStatementTypeObjectLiteral
}

// ObjectLiteralEntry is one key/value pair of an ObjectLiteralStmt.
type ObjectLiteralEntry struct {
	Key   string   `json:"key,omitempty"`
	Value ExprStmt `json:"value,omitempty"`
}

type VariableStmt struct {
	statementBase `json:",inline"`
	Name          string `json:"name,omitempty"`
}

func (s VariableStmt) GetStatementType() StatementType {
	return ASTStatementTypeVariable
}

// ExprStmt represents a value e.g. a literal, variable reference, result of a method call, etc.
type ExprStmt struct {
	statementBase `json:",inline"`
	Literal       *LiteralStmt       `json:"literal,omitempty"`
	Variable      *ScopedVariableRef `json:"variable,omitempty"`
	Binary        *BinaryStmt        `json:"binary,omitempty"`
	Cast          *CastStmt          `json:"cast,omitempty"`
	Unary         *UnaryStmt         `json:"unary,omitempty"`
	MethodCall    *MethodCallStmt    `json:"method_call,omitempty"`
	EndpointCall  *EndpointCallStmt  `json:"endpoint_call,omitempty"`
	RecordRead    *RecordReadStmt    `json:"record_read,omitempty"`
	Tuple         *TupleStmt         `json:"tuple,omitempty"`
	ObjectLiteral *ObjectLiteralStmt `json:"object_literal,omitempty"`
}

func (e ExprStmt) Value() Statement {
	return firstStatement(
		e.Literal,
		e.Variable,
		e.Binary,
		e.Cast,
		e.Unary,
		e.MethodCall,
		e.EndpointCall,
		e.RecordRead,
		e.Tuple,
		e.ObjectLiteral,
	)
}

// GetIdentifier implements Node.
func (e ExprStmt) GetIdentifier() Identifier {
	panic("unimplemented")
}

// GetLanguage implements Node.
func (e ExprStmt) GetLanguage() string {
	panic("unimplemented")
}

// GetLocation implements Node.
// Subtle: this method shadows the method (statementBase).GetLocation of ExprStmt.statementBase.
func (e ExprStmt) GetLocation() Location {
	panic("unimplemented")
}

// GetType implements Node.
func (e ExprStmt) GetType() NodeType {
	panic("unimplemented")
}

func (e ExprStmt) GetRelationships() []Relationship {
	var relationships []Relationship
	if e.MethodCall != nil {
		relationships = append(relationships, e.MethodCall.GetRelationships()...)
	}
	if e.EndpointCall != nil {
		relationships = append(relationships, e.EndpointCall.GetRelationships()...)
	}
	if e.RecordRead != nil {
		relationships = append(relationships, e.RecordRead.GetRelationships()...)
	}
	if e.Variable != nil {
		relationships = append(relationships, NewRelationship(RelationshipTypeRead, nil, e.Variable.AsRef()).Build())
	}
	if e.ObjectLiteral != nil {
		for _, entry := range e.ObjectLiteral.Entries {
			relationships = append(relationships, entry.Value.GetRelationships()...)
		}
	}
	return relationships
}

func (e ExprStmt) GetChildren() []Node {
	var children []Node
	if e.Variable != nil {
		children = append(children, e.Variable.AsRef())
	}
	if e.Binary != nil {
		children = append(children, e.Binary.Left, e.Binary.Right)
	}
	if e.Cast != nil {
		children = append(children, e.Cast.Expr)
	}

	if e.MethodCall != nil {
		children = append(children, e.MethodCall.Method)
	}
	if e.EndpointCall != nil {
		children = append(children, e.EndpointCall.Endpoint)
	}
	if e.RecordRead != nil {
		children = append(children, e.RecordRead.Record)
	}
	if e.Tuple != nil {
		for _, child := range e.Tuple.Elements {
			children = append(children, child)
		}
	}
	if e.ObjectLiteral != nil {
		for _, entry := range e.ObjectLiteral.Entries {
			children = append(children, entry.Value)
		}
	}
	return children
}

func (s ExprStmt) GetStatementType() StatementType {
	return ASTStatementTypeExpression
}

// RawStmt preserves unparseable source code verbatim for lossless round-tripping.
// Use this when the UIR type system cannot represent a language-specific construct.
type RawStmt struct {
	statementBase `json:",inline"`
	// The raw source code text
	Source string `json:"source"`
	// The language this raw source is from
	Language string `json:"language,omitempty"`
}

func (s RawStmt) GetStatementType() StatementType {
	return ASTStatementTypeRaw
}

func (s RawStmt) GetSignature() string {
	return "raw"
}

// DestructuringStmt represents destructuring assignment patterns
// e.g. const {a, b} = obj; const [x, y] = arr; const {a, ...rest} = obj
type DestructuringStmt struct {
	statementBase `json:",inline"`
	// Whether this is an object ({}) or array ([]) destructuring
	IsArray bool `json:"isArray,omitempty"`
	// The destructured bindings (name → alias/default pairs)
	Bindings []DestructureBinding `json:"bindings,omitempty"`
	// The rest element (...rest)
	Rest *string `json:"rest,omitempty"`
	// The source expression being destructured
	Value ExprStmt `json:"value,omitempty"`
	// Declare selects the binding form. "" (the default) declares with
	// `const`; DestructureDeclareLet declares a mutable binding; and
	// DestructureDeclareNone assigns onto bindings that already exist in
	// the enclosing scope, which is the only form that can target a name
	// another statement also writes.
	Declare string `json:"declare,omitempty"`
}

type DestructureBinding struct {
	// The property name being extracted
	Name string `json:"name"`
	// Alias if renamed (e.g. {name: alias})
	Alias string `json:"alias,omitempty"`
	// Default value if property is undefined
	Default *ExprStmt `json:"default,omitempty"`
	// Nested destructuring pattern
	Nested *DestructuringStmt `json:"nested,omitempty"`
}

func (s DestructuringStmt) GetStatementType() StatementType {
	return ASTStatementTypeDestructure
}

func (s DestructuringStmt) GetSignature() string {
	return "destructure"
}

// TemplateLiteralStmt represents template literals with embedded expressions
// e.g. `Hello ${name}, you have ${count} items`
type TemplateLiteralStmt struct {
	statementBase `json:",inline"`
	// Alternating string parts and expression parts
	// Strings[0] + Expressions[0] + Strings[1] + Expressions[1] + ... + Strings[n]
	Strings     []string   `json:"strings"`
	Expressions []ExprStmt `json:"expressions,omitempty"`
	// Tag function for tagged templates (e.g. html`...`)
	Tag *ExprStmt `json:"tag,omitempty"`
}

func (s TemplateLiteralStmt) GetStatementType() StatementType {
	return ASTStatementTypeTemplateLit
}

func (s TemplateLiteralStmt) GetSignature() string {
	return "template"
}

// Expression Statements
func (s AssignmentStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	targetSig := s.Target.GetSignature()
	valueSig := s.Value.GetSignature()
	return targetSig + " " + string(s.Op) + " " + valueSig
}

func (s UnaryStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	return string(s.Operator) + s.Operand.GetSignature()
}

func (s BinaryStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	return s.Left.GetSignature() + " " + string(s.Operator) + " " + s.Right.GetSignature()
}

func (s LiteralStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	if s.Value != nil {
		return "lit:" + *s.Value
	}
	return "literal"
}

func (s CastStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	return "(" + s.TargetType + ")" + s.Expr.GetSignature()
}

func (s VariableStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	if s.Name != "" {
		return s.Name
	}
	return "var"
}

func (s TupleStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	if len(s.Elements) == 0 {
		return "()"
	}
	sig := "("
	for i, elem := range s.Elements {
		if i > 0 {
			sig += ", "
		}
		sig += elem.GetSignature()
	}
	sig += ")"
	return sig
}

// Expression Statement
func (s ExprStmt) GetSignature() string {
	return s.Value().GetSignature()
}
