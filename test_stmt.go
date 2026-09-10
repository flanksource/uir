package uir

type TestStmt struct {
	BlockStmt `json:",inline"`
	Name      string `json:"name,omitempty"`
}

func NewTest(name string) *TestStmt {
	t := TestStmt{}
	t.Name = name
	t.Type = ASTStatementTypeTest
	return &t
}

func (t TestStmt) GetStatementType() StatementType {
	return ASTStatementTypeTest
}
