package uir

import (
	"testing"

	"github.com/google/uuid"
)

func TestIdentifierGetUUIDUsesProvidedID(t *testing.T) {
	provided := uuid.New()
	id := Identifier{Id: &provided, Module: "m", Package: "p", Type: "T", NodeType: NodeTypeType}

	if got := id.GetUUID(); got != provided {
		t.Fatalf("GetUUID() = %s, want provided %s", got, provided)
	}
}

func TestIdentifierGetUUIDIsIdempotent(t *testing.T) {
	id := Identifier{Module: "m", Package: "p", Type: "T", Method: "Run", NodeType: NodeTypeMethod}

	first := id.GetUUID()
	second := id.GetUUID()

	if first == uuid.Nil {
		t.Fatal("GetUUID() returned nil UUID")
	}
	if first != second {
		t.Fatalf("GetUUID() is not idempotent: %s != %s", first, second)
	}
}

func TestHashIgnoresLocationAndSourceContent(t *testing.T) {
	before := MethodNode{
		nodeBase: nodeBase{
			Identifier: Identifier{Package: "pkg", Type: "Service", Method: "Run", NodeType: NodeTypeMethod},
			SourceCode: SourceCode{
				Location: Location{Path: "before.go"},
				Content:  hashTestStrPtr("func before() {}"),
			},
		},
		Body: hashTestBlockWithRaw("return x"),
	}
	after := before
	after.Path = "after.go"
	after.Content = hashTestStrPtr("func after() {}")

	if before.Hash() != after.Hash() {
		t.Fatalf("location/source-only change affected hash:\nbefore=%s\nafter=%s", before.Hash(), after.Hash())
	}
}

func TestHashChangesForSemanticMethodBodyChange(t *testing.T) {
	before := MethodNode{Body: hashTestBlockWithRaw("return x")}
	after := MethodNode{Body: hashTestBlockWithRaw("return y")}

	if before.Hash() == after.Hash() {
		t.Fatal("method body semantic change did not affect hash")
	}
}

func TestNodeListHashIsOrderSensitive(t *testing.T) {
	a := MethodNode{nodeBase: nodeBase{Identifier: Identifier{Type: "T", Method: "A", NodeType: NodeTypeMethod}}, Body: hashTestBlockWithRaw("a")}
	b := MethodNode{nodeBase: nodeBase{Identifier: Identifier{Type: "T", Method: "B", NodeType: NodeTypeMethod}}, Body: hashTestBlockWithRaw("b")}

	first := NodeList{a, b}
	second := NodeList{b, a}

	if first.Hash() == second.Hash() {
		t.Fatal("NodeList.Hash() should be order-sensitive")
	}
}

func hashTestBlockWithRaw(raw string) *BlockStmt {
	return &BlockStmt{
		statementBase: statementBase{Type: ASTStatementTypeBlock},
		Children: []Statement{
			RawStmt{
				statementBase: statementBase{Type: ASTStatementTypeRaw},
				Source:        raw,
			},
		},
	}
}

func hashTestStrPtr(s string) *string {
	return &s
}
