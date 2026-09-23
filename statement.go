package uir

import (
	"reflect"

	"github.com/flanksource/clicky/api"
)

type Statement interface {
	GetStatementType() StatementType
	// GetCategory() CategoryType
	// GetChildren() []Statement
	Pretty() api.Text
	// GetNode() *ASTNode
	// GetStatement() *ASTStatement
	String() string

	// GetSignatures returns the a unique signature representing the statement within its scope
	GetSignature() string
}

type UIRStatement struct {
	statementBase `json:",inline"`
}

type Relatable interface {
	GetRelationships() []Relationship
}

var AllRelatables = []Relatable{
	AssignmentStmt{},
	MethodCallStmt{},
	EndpointCallStmt{},
	RecordReadStmt{},
	RecordWriteStmt{},
	ExprStmt{},
}

// GetIdentifier implements Node.
func (stmt UIRStatement) GetIdentifier() Identifier {
	panic("unimplemented")
}

// GetLanguage implements Node.
func (stmt UIRStatement) GetLanguage() string {
	panic("unimplemented")
}

// GetLocation implements Node.
func (stmt UIRStatement) GetLocation() Location {
	panic("unimplemented")
}

// GetType implements Node.
// Subtle: this method shadows the method (statementBase).GetType of UIRStatement.statementBase.
func (stmt UIRStatement) GetType() NodeType {
	return NodeTypeStatement
}

// Pretty implements Node.
func (stmt UIRStatement) Pretty() api.Text {
	panic("unimplemented")
}

func (stmt statementBase) String() string {
	return string(stmt.Type)
}

func (stmt UIRStatement) AsNode() Node {
	return stmt
}

func (stmt UIRStatement) GetRelationships() []Relationship {
	return nil
}

func (stmt UIRStatement) GetChildren() []Node {
	return nil
}

type statementBase struct {
	// Type is a cache of the concrete statement kind, not its source of truth —
	// GetStatementType is. MarshalStatement stamps the encoded statement_type from
	// there, so a literal that leaves this unset still round-trips.
	Type       StatementType `json:"statement_type,omitempty"`
	SourceCode `json:",inline"`
	Metadata   `json:",inline"`
}

func (s statementBase) GetStatementType() StatementType {
	return s.Type
}

func (s statementBase) GetLocation() Location {
	return s.Location
}

func (s statementBase) WithLocation(location Location) statementBase {
	s.Location = location
	return s
}

func (s *MethodCallBuilder) WithLocation(location Location) *MethodCallBuilder {
	s.Location = location
	return s
}

func (s statementBase) WithFile(file string) statementBase {
	s.Path = file
	return s
}

func (s statementBase) WithLine(start, end int) statementBase {
	s.StartLine = new(start)
	if end == 0 {
		end = start
	}
	s.EndLine = new(end)
	return s
}

func (s statementBase) WithSourceCode(code string) statementBase {
	s.Content = &code
	return s
}

func (s statementBase) WithColumn(col int) statementBase {
	s.Column = &col
	return s
}

type Stmt struct {
	Assignment       *AssignmentStmt      `json:"assignment,omitempty"`
	Binary           *BinaryStmt          `json:"binary,omitempty"`
	Block            *BlockStmt           `json:"block,omitempty"`
	Break            *BreakStmt           `json:"break,omitempty"`
	Cast             *CastStmt            `json:"cast,omitempty"`
	Condition        *ConditionStmt       `json:"condition,omitempty"`
	Continue         *ContinueStmt        `json:"continue,omitempty"`
	EndpointCall     *EndpointCallStmt    `json:"endpoint_call,omitempty"`
	Expr             *ExprStmt            `json:"expr,omitempty"`
	For              *ForStmt             `json:"for,omitempty"`
	ForEach          *ForEachStmt         `json:"for_each,omitempty"`
	FunctionDeclStmt *FunctionDeclStmt    `json:"function_decl,omitempty"`
	If               *IfStmt              `json:"if,omitempty"`
	Literal          *LiteralStmt         `json:"literal,omitempty"`
	MethodCall       *MethodCallStmt      `json:"method_call,omitempty"`
	RecordRead       *RecordReadStmt      `json:"record_read,omitempty"`
	RecordWriteStmt  *RecordWriteStmt     `json:"record_write,omitempty"`
	Return           *ReturnStmt          `json:"return,omitempty"`
	SwitchStmt       *SwitchStmt          `json:"switch,omitempty"`
	Throw            *ThrowStmt           `json:"throw,omitempty"`
	Try              *TryStmt             `json:"try,omitempty"`
	Tuple            *TupleStmt           `json:"tuple,omitempty"`
	Unary            *UnaryStmt           `json:"unary,omitempty"`
	Variable         *VariableStmt        `json:"variable,omitempty"`
	VariableDeclStmt *VariableDeclStmt    `json:"variable_decl,omitempty"`
	While            *WhileStmt           `json:"while,omitempty"`
	DocStmt          *DocStmt             `json:"doc,omitempty"`
	TestStmt         *TestStmt            `json:"test,omitempty"`
	Raw              *RawStmt             `json:"raw,omitempty"`
	Destructure      *DestructuringStmt   `json:"destructure,omitempty"`
	TemplateLiteral  *TemplateLiteralStmt `json:"template_literal,omitempty"`
}

func (n Stmt) Value() Statement {
	return firstStatement(
		n.Assignment,
		n.DocStmt,
		n.Binary,
		n.Block,
		n.Break,
		n.Cast,
		n.Condition,
		n.Continue,
		n.EndpointCall,
		n.Expr,
		n.For,
		n.ForEach,
		n.FunctionDeclStmt,
		n.If,
		n.Literal,
		n.MethodCall,
		n.RecordRead,
		n.RecordWriteStmt,
		n.Return,
		n.SwitchStmt,
		n.Throw,
		n.Try,
		n.Tuple,
		n.Unary,
		n.Variable,
		n.VariableDeclStmt,
		n.While,
		n.TestStmt,
		n.Raw,
		n.Destructure,
		n.TemplateLiteral,
	)
}

// firstStatement returns the first candidate holding a statement. The
// candidates are the pointer fields of a one-of wrapper (Stmt, ExprStmt), so an
// unset field is a typed nil inside a non-nil interface and must be skipped.
func firstStatement(candidates ...Statement) Statement {
	for _, candidate := range candidates {
		if candidate != nil && !reflect.ValueOf(candidate).IsNil() {
			return candidate
		}
	}
	return nil
}

func (s Stmt) Pretty() api.Text {
	val := s.Value()
	if val != nil {
		return val.Pretty()
	}
	return api.Text{Content: "null", Style: "text-gray-500"}
}

func (s Stmt) GetStatement() Statement {
	return s.Value()
}

func NewRef(id Identifier) Node {
	return NodeRef{
		Identifier: id,
	}
}

const (
	// DestructureDeclareLet renders `let { a, b } = value;`.
	DestructureDeclareLet = "let"
	// DestructureDeclareNone renders `({ a, b } = value);` — an assignment
	// to existing bindings rather than a declaration.
	DestructureDeclareNone = "none"
)

var Statements = []Statement{
	AssignmentStmt{statementBase: statementBase{Type: ASTStatementTypeAssignment}},
	BinaryStmt{statementBase: statementBase{Type: ASTStatementTypeBinary}},
	BlockStmt{statementBase: statementBase{Type: ASTStatementTypeBlock}},
	BreakStmt{statementBase: statementBase{Type: ASTStatementTypeBreak}},
	CastStmt{statementBase: statementBase{Type: ASTStatementTypeCast}},
	ConditionStmt{statementBase: statementBase{Type: ASTStatmentTypeCondition}},
	ConstDeclStmt{statementBase: statementBase{Type: ASTStatementTypeDeclareConstant}},
	ContinueStmt{statementBase: statementBase{Type: ASTStatementTypeContinue}},
	EndpointCallStmt{statementBase: statementBase{Type: ASTStatementTypeCallAPI}},
	ExprStmt{statementBase: statementBase{Type: ASTStatementTypeExpression}},
	ForStmt{statementBase: statementBase{Type: ASTStatementTypeLoopFor}},
	ForEachStmt{statementBase: statementBase{Type: ASTStatementTypeLoopForEach}},
	FunctionDeclStmt{statementBase: statementBase{Type: ASTStatementTypeDeclareFunction}},
	IfStmt{statementBase: statementBase{Type: ASTStatementTypeIf}},
	LiteralStmt{statementBase: statementBase{Type: ASTStatementTypeLiteral}},
	MethodCallStmt{methodBase: methodBase{statementBase: statementBase{Type: ASTStatementTypeCall}}},
	RecordReadStmt{recordBase: recordBase{statementBase: statementBase{Type: ASTStatementTypeRecordRead}}},
	RecordWriteStmt{recordBase: recordBase{statementBase: statementBase{Type: ASTStatementTypeRecordWrite}}},
	ReturnStmt{statementBase: statementBase{Type: ASTStatementTypeReturn}},
	SwitchStmt{statementBase: statementBase{Type: ASTStatementTypeSwitch}},
	ThrowStmt{statementBase: statementBase{Type: ASTStatementTypeThrow}},
	TryStmt{statementBase: statementBase{Type: ASTStatementTypeTry}},
	TupleStmt{statementBase: statementBase{Type: ASTStatementTypeTuple}},
	ObjectLiteralStmt{statementBase: statementBase{Type: ASTStatementTypeObjectLiteral}},
	TypeDeclStmt{statementBase: statementBase{Type: ASTStatementTypeDeclareType}},
	UnaryStmt{statementBase: statementBase{Type: ASTStatementTypeUnary}},
	VariableDeclStmt{statementBase: statementBase{Type: ASTStatementTypeDeclareVariable}},
	VariableStmt{statementBase: statementBase{Type: ASTStatementTypeVariable}},
	WhileStmt{statementBase: statementBase{Type: ASTStatementTypeLoopWhile}},
	DocStmt{statementBase: statementBase{Type: ASTStatementTypeDoc}},
	TestStmt{BlockStmt: BlockStmt{statementBase: statementBase{Type: ASTStatementTypeTest}}},
	RawStmt{statementBase: statementBase{Type: ASTStatementTypeRaw}},
	DestructuringStmt{statementBase: statementBase{Type: ASTStatementTypeDestructure}},
	TemplateLiteralStmt{statementBase: statementBase{Type: ASTStatementTypeTemplateLit}},
}

// Basic Statement Types
func (s statementBase) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	return "stmt"
}

func (s UIRStatement) GetSignature() string {
	return s.statementBase.GetSignature()
}

// Generic Statement
func (s Stmt) GetSignature() string {
	return s.Value().GetSignature()
}
