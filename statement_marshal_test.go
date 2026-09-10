package uir

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Statements are routinely built as plain struct literals (`uir.RawStmt{Source: src}`)
// that never populate the embedded statementBase.Type. The concrete Go type is the
// source of truth, so the JSON discriminator the polymorphic decoder keys on has to be
// derived from GetStatementType rather than read off the field.
func TestStatementTypeIsDerivedFromConcreteType(t *testing.T) {
	for _, prototype := range Statements {
		kind := prototype.GetStatementType()
		t.Run(string(kind), func(t *testing.T) {
			zero, ok := reflect.New(reflect.TypeOf(prototype)).Elem().Interface().(Statement)
			if !ok {
				t.Fatalf("%T does not implement Statement", prototype)
			}
			if zero.GetStatementType() != kind {
				t.Fatalf("zero value GetStatementType() = %q, want %q", zero.GetStatementType(), kind)
			}

			raw, err := MarshalStatement(zero)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			decoded, err := StatementMarshaler.UnmarshalByType(raw)
			if err != nil {
				t.Fatalf("failed to unmarshal %s: %v", raw, err)
			}
			if decoded.GetStatementType() != kind {
				t.Errorf("round-tripped statement type = %q, want %q", decoded.GetStatementType(), kind)
			}
		})
	}
}

func TestBlockChildrenRoundTripWithoutExplicitType(t *testing.T) {
	value := "42"
	original := BlockStmt{Children: []Statement{
		VariableStmt{Name: "x"},
		RawStmt{Source: "console.log(x);", Language: "typescript"},
		IfStmt{Then: BlockStmt{Children: []Statement{
			ReturnStmt{Value: ExprStmt{Literal: &LiteralStmt{Value: &value, FieldType: RecordFieldTypeNumber}}},
		}}},
	}}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal block: %v", err)
	}

	var decoded BlockStmt
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal block: %v", err)
	}

	if decoded.GetStatementType() != ASTStatementTypeBlock {
		t.Errorf("block type = %q, want %q", decoded.GetStatementType(), ASTStatementTypeBlock)
	}
	want := []StatementType{ASTStatementTypeVariable, ASTStatementTypeRaw, ASTStatementTypeIf}
	if len(decoded.Children) != len(want) {
		t.Fatalf("got %d children, want %d", len(decoded.Children), len(want))
	}
	for i, kind := range want {
		if got := decoded.Children[i].GetStatementType(); got != kind {
			t.Errorf("child %d type = %q, want %q", i, got, kind)
		}
	}

	nested, ok := decoded.Children[2].(*IfStmt)
	if !ok {
		t.Fatalf("child 2 = %T, want *IfStmt", decoded.Children[2])
	}
	if len(nested.Then.Children) != 1 {
		t.Fatalf("nested block has %d children, want 1", len(nested.Then.Children))
	}
	if got := nested.Then.Children[0].GetStatementType(); got != ASTStatementTypeReturn {
		t.Errorf("nested child type = %q, want %q", got, ASTStatementTypeReturn)
	}
}

// MethodNode.Body is decoded through the polymorphic registry, so a method whose body
// was assembled without an explicit statement type must still survive a cache round-trip.
func TestMethodBodyRoundTripWithoutExplicitType(t *testing.T) {
	method := MethodNode{Body: &BlockStmt{Children: []Statement{
		RawStmt{Source: "return 1;", Language: "typescript"},
	}}}
	method.Method = "compute"

	data, err := json.Marshal(method)
	if err != nil {
		t.Fatalf("failed to marshal method: %v", err)
	}

	var decoded MethodNode
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal method: %v", err)
	}
	if decoded.Body == nil {
		t.Fatal("decoded body is nil")
	}
	if len(decoded.Body.Children) != 1 {
		t.Fatalf("decoded body has %d children, want 1", len(decoded.Body.Children))
	}
	if got := decoded.Body.Children[0].GetStatementType(); got != ASTStatementTypeRaw {
		t.Errorf("body child type = %q, want %q", got, ASTStatementTypeRaw)
	}
}

func TestTestStmtKeepsNameWhenMarshalled(t *testing.T) {
	stmt := NewTest("renders totals")
	stmt.Children = []Statement{RawStmt{Source: "expect(1).toBe(1);", Language: "typescript"}}

	data, err := json.Marshal(stmt)
	if err != nil {
		t.Fatalf("failed to marshal test statement: %v", err)
	}

	decoded, err := StatementMarshaler.UnmarshalByType(data)
	if err != nil {
		t.Fatalf("failed to unmarshal test statement: %v", err)
	}
	got, ok := decoded.(*TestStmt)
	if !ok {
		t.Fatalf("decoded = %T, want *TestStmt", decoded)
	}
	if got.Name != stmt.Name {
		t.Errorf("name = %q, want %q", got.Name, stmt.Name)
	}
	if len(got.Children) != 1 {
		t.Fatalf("got %d children, want 1", len(got.Children))
	}
}
