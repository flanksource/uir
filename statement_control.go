package uir

type IfStmt struct {
	statementBase `json:",inline"`
	Condition     ConditionStmt `json:"condition,omitempty"`
	Then          BlockStmt     `json:"then,omitempty"`
	Else          *BlockStmt    `json:"else,omitempty"`
}

func (s IfStmt) GetStatementType() StatementType {
	return ASTStatementTypeIf
}

type SwitchStmt struct {
	statementBase `json:",inline"`
	Value         ExprStmt `json:"value,omitempty"`
	Cases         []struct {
		Condition ConditionStmt `json:"condition,omitempty"`
		Body      BlockStmt     `json:"body,omitempty"`
	} `json:"cases,omitempty"`
}

func (s SwitchStmt) GetStatementType() StatementType {
	return ASTStatementTypeSwitch
}

type ForStmt struct {
	statementBase `json:",inline"`
	Init          AssignmentStmt `json:"init,omitempty"`
	Cond          ConditionStmt  `json:"cond,omitempty"`
	Body          BlockStmt      `json:"body,omitempty"`
	Update        ExprStmt       `json:"update,omitempty"`
}

func (s ForStmt) GetStatementType() StatementType {
	return ASTStatementTypeLoopFor
}

type ForEachStmt struct {
	statementBase `json:",inline"`
	Variable      AssignmentStmt `json:"variable,omitempty"`
	Iterable      ExprStmt       `json:"iterable,omitempty"`
	Body          BlockStmt      `json:"body,omitempty"`
}

func (s ForEachStmt) GetStatementType() StatementType {
	return ASTStatementTypeLoopForEach
}

type WhileStmt struct {
	statementBase `json:",inline"`
	Condition     ConditionStmt `json:"condition,omitempty"`
	Body          BlockStmt     `json:"body,omitempty"`
}

func (s WhileStmt) GetStatementType() StatementType {
	return ASTStatementTypeLoopWhile
}

type BlockStmt struct {
	statementBase `json:",inline"`
	// Defines the variable declared in the block's scope
	Variables ParamsDef   `json:"variables,omitempty" gorm:"type:text"`
	Children  []Statement `json:"children,omitempty" gorm:"serializer:json"`
}

func (b BlockStmt) GetStatementType() StatementType {
	return ASTStatementTypeBlock
}

// GetRelationships collects the relationships of the block's statements.
// Statements are not Nodes, so a node tree over the block cannot reach them;
// nested blocks recurse through their own GetRelationships.
func (b BlockStmt) GetRelationships() []Relationship {
	var relationships []Relationship
	for _, child := range b.Children {
		if relatable, ok := child.(Relatable); ok {
			relationships = append(relationships, relatable.GetRelationships()...)
		}
	}
	return relationships
}

func (b BlockStmt) AsTree() NodeTree {
	return NodeTree{
		Node: b,
	}
}

func (b BlockStmt) GetChildren() []Node {
	var children []Node
	for _, child := range b.Children {
		if node, ok := child.(Node); ok {
			children = append(children, node)
		}
	}
	return children
}

func (s BlockStmt) GetType() NodeType {
	return NodeTypeStatement
}

// GetIdentifier implements Node.
func (s BlockStmt) GetIdentifier() Identifier {
	return Identifier{} // Block statements typically don't have meaningful identifiers
}

// GetLanguage implements Node.
func (s BlockStmt) GetLanguage() string {
	return "" // Can be overridden when block is created
}

type ReturnStmt struct {
	statementBase `json:",inline"`
	Value         ExprStmt `json:"value,omitempty"`
}

func (s ReturnStmt) GetStatementType() StatementType {
	return ASTStatementTypeReturn
}

type BreakStmt struct {
	statementBase `json:",inline"`
}

func (s BreakStmt) GetStatementType() StatementType {
	return ASTStatementTypeBreak
}

type ContinueStmt struct {
	statementBase `json:",inline"`
}

func (s ContinueStmt) GetStatementType() StatementType {
	return ASTStatementTypeContinue
}

type ThrowStmt struct {
	statementBase `json:",inline"`
	Exception     ExprStmt `json:"exception,omitempty"`
}

func (s ThrowStmt) GetStatementType() StatementType {
	return ASTStatementTypeThrow
}

type TryStmt struct {
	statementBase `json:",inline"`
	Body          BlockStmt `json:"body,omitempty"`
	Catch         BlockStmt `json:"catch,omitempty"`
}

func (s TryStmt) GetStatementType() StatementType {
	return ASTStatementTypeTry
}

type ConditionStmt struct {
	statementBase `json:",inline"`
	Expr          ExprStmt `json:"expr,omitempty"`
}

func (s ConditionStmt) GetStatementType() StatementType {
	return ASTStatmentTypeCondition
}

// Control Flow Statements
func (s IfStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	return "if(" + s.Condition.GetSignature() + ")"
}

func (s SwitchStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	return "switch(" + s.Value.GetSignature() + ")"
}

func (s ForStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	return "for(" + s.Cond.GetSignature() + ")"
}

func (s ForEachStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	return "for(" + s.Variable.GetSignature() + " of " + s.Iterable.GetSignature() + ")"
}

func (s WhileStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	return "while(" + s.Condition.GetSignature() + ")"
}

func (s BlockStmt) GetSignature() string {
	location := s.GetLocation()
	if !location.IsEmpty() {
		return location.GetSignature()
	}
	return "block"
}
