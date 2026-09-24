package uir

import (
	"encoding/json"
	"testing"
)

// TestStatementMarshalType ensures that all statements marshal with their type field set
func TestStatementMarshalType(t *testing.T) {
	tests := []struct {
		name     string
		stmt     Statement
		wantType StatementType
	}{
		{
			name:     "IfStmt",
			stmt:     IfStmt{statementBase: statementBase{Type: ASTStatementTypeIf}},
			wantType: ASTStatementTypeIf,
		},
		{
			name:     "SwitchStmt",
			stmt:     SwitchStmt{statementBase: statementBase{Type: ASTStatementTypeSwitch}},
			wantType: ASTStatementTypeSwitch,
		},
		{
			name:     "ForStmt",
			stmt:     ForStmt{statementBase: statementBase{Type: ASTStatementTypeLoopFor}},
			wantType: ASTStatementTypeLoopFor,
		},
		{
			name:     "WhileStmt",
			stmt:     WhileStmt{statementBase: statementBase{Type: ASTStatementTypeLoopWhile}},
			wantType: ASTStatementTypeLoopWhile,
		},
		{
			name:     "AssignmentStmt",
			stmt:     AssignmentStmt{statementBase: statementBase{Type: ASTStatementTypeAssignment}},
			wantType: ASTStatementTypeAssignment,
		},
		{
			name:     "BinaryStmt",
			stmt:     BinaryStmt{statementBase: statementBase{Type: ASTStatementTypeBinary}},
			wantType: ASTStatementTypeBinary,
		},
		{
			name:     "UnaryStmt",
			stmt:     UnaryStmt{statementBase: statementBase{Type: ASTStatementTypeUnary}},
			wantType: ASTStatementTypeUnary,
		},
		{
			name:     "LiteralStmt",
			stmt:     LiteralStmt{statementBase: statementBase{Type: ASTStatementTypeLiteral}},
			wantType: ASTStatementTypeLiteral,
		},
		{
			name:     "VariableStmt",
			stmt:     VariableStmt{statementBase: statementBase{Type: ASTStatementTypeVariable}},
			wantType: ASTStatementTypeVariable,
		},
		{
			name:     "MethodCallStmt",
			stmt:     MethodCallStmt{methodBase: methodBase{statementBase: statementBase{Type: ASTStatementTypeCall}}},
			wantType: ASTStatementTypeCall,
		},
		{
			name:     "ExprStmt",
			stmt:     ExprStmt{statementBase: statementBase{Type: ASTStatementTypeExpression}},
			wantType: ASTStatementTypeExpression,
		},
		{
			name:     "ReturnStmt",
			stmt:     ReturnStmt{statementBase: statementBase{Type: ASTStatementTypeReturn}},
			wantType: ASTStatementTypeReturn,
		},
		{
			name:     "BreakStmt",
			stmt:     BreakStmt{statementBase: statementBase{Type: ASTStatementTypeBreak}},
			wantType: ASTStatementTypeBreak,
		},
		{
			name:     "ContinueStmt",
			stmt:     ContinueStmt{statementBase: statementBase{Type: ASTStatementTypeContinue}},
			wantType: ASTStatementTypeContinue,
		},
		{
			name:     "ThrowStmt",
			stmt:     ThrowStmt{statementBase: statementBase{Type: ASTStatementTypeThrow}},
			wantType: ASTStatementTypeThrow,
		},
		{
			name:     "TryStmt",
			stmt:     TryStmt{statementBase: statementBase{Type: ASTStatementTypeTry}},
			wantType: ASTStatementTypeTry,
		},
		{
			name:     "TupleStmt",
			stmt:     TupleStmt{statementBase: statementBase{Type: ASTStatementTypeTuple}},
			wantType: ASTStatementTypeTuple,
		},
		{
			name:     "ConditionStmt",
			stmt:     ConditionStmt{statementBase: statementBase{Type: ASTStatmentTypeCondition}},
			wantType: ASTStatmentTypeCondition,
		},
		{
			name:     "BlockStmt",
			stmt:     BlockStmt{statementBase: statementBase{Type: ASTStatementTypeBlock}},
			wantType: ASTStatementTypeBlock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.stmt)
			if err != nil {
				t.Fatalf("failed to marshal %s: %v", tt.name, err)
			}

			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatalf("failed to unmarshal %s JSON: %v", tt.name, err)
			}

			gotType, ok := result["statement_type"].(string)
			if !ok {
				t.Fatalf("%s: type field not found or not a string", tt.name)
			}

			if StatementType(gotType) != tt.wantType {
				t.Errorf("%s: type = %v, want %v", tt.name, gotType, tt.wantType)
			}
		})
	}
}

// TestBlockStmtMarshalUnmarshal tests that BlockStmt can marshal and unmarshal with children
func TestBlockStmtMarshalUnmarshal(t *testing.T) {
	varName := "x"
	value := "42"

	original := BlockStmt{
		statementBase: statementBase{Type: ASTStatementTypeBlock},
		Children: []Statement{
			VariableStmt{statementBase: statementBase{Type: ASTStatementTypeVariable}, Name: varName},
			LiteralStmt{statementBase: statementBase{Type: ASTStatementTypeLiteral}, Value: &value, FieldType: RecordFieldTypeNumber},
			ReturnStmt{
				statementBase: statementBase{Type: ASTStatementTypeReturn},
				Value: ExprStmt{
					Variable: &ScopedVariableRef{VariableRef: VariableRef{Name: varName}},
				},
			},
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal BlockStmt: %v", err)
	}

	// Unmarshal from JSON
	var unmarshaled BlockStmt
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal BlockStmt: %v", err)
	}

	// Verify structure
	if len(unmarshaled.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(unmarshaled.Children))
	}

	// Check first child (VariableStmt) — registry returns pointer types
	varStmt, ok := unmarshaled.Children[0].(*VariableStmt)
	if !ok {
		t.Fatalf("expected first child to be *VariableStmt, got %T", unmarshaled.Children[0])
	}
	if varStmt.Name != varName {
		t.Errorf("VariableStmt name = %v, want %v", varStmt.Name, varName)
	}

	// Check second child (LiteralStmt)
	litStmt, ok := unmarshaled.Children[1].(*LiteralStmt)
	if !ok {
		t.Fatalf("expected second child to be *LiteralStmt, got %T", unmarshaled.Children[1])
	}
	if litStmt.Value == nil || *litStmt.Value != value {
		t.Errorf("LiteralStmt value = %v, want %v", litStmt.Value, value)
	}

	// Check third child (ReturnStmt)
	retStmt, ok := unmarshaled.Children[2].(*ReturnStmt)
	if !ok {
		t.Fatalf("expected third child to be *ReturnStmt, got %T", unmarshaled.Children[2])
	}
	if retStmt.Value.Variable == nil {
		t.Fatal("ReturnStmt value should have variable")
	}
	if retStmt.Value.Variable.Name != varName {
		t.Errorf("ReturnStmt variable name = %v, want %v", retStmt.Value.Variable.Name, varName)
	}
}

func NewVar(name string) *ScopedVariableRef {
	return &ScopedVariableRef{VariableRef: VariableRef{Name: name}}
}

// TestNestedBlockStmt tests deeply nested block statements
func TestNestedBlockStmt(t *testing.T) {
	innerVarName := "y"

	original := BlockStmt{
		statementBase: statementBase{Type: ASTStatementTypeBlock},
		Children: []Statement{
			IfStmt{
				statementBase: statementBase{Type: ASTStatementTypeIf},
				Condition: ConditionStmt{
					Expr: ExprStmt{
						Variable: NewVar("x"),
					},
				},
				Then: BlockStmt{
					statementBase: statementBase{Type: ASTStatementTypeBlock},
					Children: []Statement{
						VariableStmt{statementBase: statementBase{Type: ASTStatementTypeVariable}, Name: innerVarName},
					},
				},
			},
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal nested BlockStmt: %v", err)
	}

	// Unmarshal from JSON
	var unmarshaled BlockStmt
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal nested BlockStmt: %v", err)
	}

	// Verify structure
	if len(unmarshaled.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(unmarshaled.Children))
	}

	ifStmt, ok := unmarshaled.Children[0].(*IfStmt)
	if !ok {
		t.Fatalf("expected child to be *IfStmt, got %T", unmarshaled.Children[0])
	}

	if len(ifStmt.Then.Children) != 1 {
		t.Fatalf("expected 1 child in Then block, got %d", len(ifStmt.Then.Children))
	}

	innerVar, ok := ifStmt.Then.Children[0].(*VariableStmt)
	if !ok {
		t.Fatalf("expected *VariableStmt in Then block, got %T", ifStmt.Then.Children[0])
	}

	if innerVar.Name != innerVarName {
		t.Errorf("inner variable name = %v, want %v", innerVar.Name, innerVarName)
	}
}

// TestRoundTripAllStatementTypes tests that all statement types can marshal and unmarshal correctly
func TestRoundTripAllStatementTypes(t *testing.T) {
	testValue := "test"

	sb := func(t StatementType) statementBase { return statementBase{Type: t} }

	statements := []Statement{
		IfStmt{statementBase: sb(ASTStatementTypeIf), Condition: ConditionStmt{}, Then: BlockStmt{}},
		SwitchStmt{statementBase: sb(ASTStatementTypeSwitch), Value: ExprStmt{}},
		ForStmt{statementBase: sb(ASTStatementTypeLoopFor), Init: AssignmentStmt{}, Cond: ConditionStmt{}, Body: BlockStmt{}, Update: ExprStmt{}},
		WhileStmt{statementBase: sb(ASTStatementTypeLoopWhile), Condition: ConditionStmt{}, Body: BlockStmt{}},
		AssignmentStmt{statementBase: sb(ASTStatementTypeAssignment), Target: *NewVar("x"), Value: ExprStmt{}, Op: AssignmentOpAssign},
		BinaryStmt{statementBase: sb(ASTStatementTypeBinary), Left: ExprStmt{}, Operator: BinaryOpAdd, Right: ExprStmt{}},
		UnaryStmt{statementBase: sb(ASTStatementTypeUnary), Operator: UnaryOpNot, Operand: ExprStmt{}},
		LiteralStmt{statementBase: sb(ASTStatementTypeLiteral), Value: &testValue, FieldType: RecordFieldTypeString},
		VariableStmt{statementBase: sb(ASTStatementTypeVariable), Name: testValue},
		NewMethodCall(testValue).Build(),
		ExprStmt{statementBase: sb(ASTStatementTypeExpression), Variable: NewVar(testValue)},
		ReturnStmt{statementBase: sb(ASTStatementTypeReturn), Value: ExprStmt{}},
		BreakStmt{statementBase: sb(ASTStatementTypeBreak)},
		ContinueStmt{statementBase: sb(ASTStatementTypeContinue)},
		ThrowStmt{statementBase: sb(ASTStatementTypeThrow), Exception: ExprStmt{}},
		TryStmt{statementBase: sb(ASTStatementTypeTry), Body: BlockStmt{}, Catch: BlockStmt{}},
		TupleStmt{statementBase: sb(ASTStatementTypeTuple), Elements: []ExprStmt{}},
		ConditionStmt{statementBase: sb(ASTStatmentTypeCondition), Expr: ExprStmt{}},
		BlockStmt{statementBase: sb(ASTStatementTypeBlock), Children: []Statement{}},
	}

	for _, stmt := range statements {
		t.Run(string(stmt.GetStatementType()), func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(stmt)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			// Unmarshal
			unmarshaled, err := StatementMarshaler.UnmarshalByType(data)
			if err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			// Check type matches
			if unmarshaled.GetStatementType() != stmt.GetStatementType() {
				t.Errorf("type mismatch: got %v, want %v", unmarshaled.GetStatementType(), stmt.GetStatementType())
			}
		})
	}
}
