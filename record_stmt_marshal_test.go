package uir

import (
	"encoding/json"
	"testing"
)

// A record read/write names its record through the Node interface, so the JSON
// must say which Node it is — without it the decoder cannot rebuild the value.
// The shape is the one OIPA SQL math lowers to: a table read inside a cast
// inside a return, inside a block.
func TestRecordReadRecordRoundTrips(t *testing.T) {
	const table = "AsPolicy"
	read := NewRecordRead(RecordTypeTable, NewRecord(table, Identifier{Type: table}).Build()).
		WithExpression(ExpressionTypeSQL, "SELECT PolicyNumber FROM AsPolicy").
		Build()
	block := BlockStmt{Children: []Statement{
		ReturnStmt{Value: ExprStmt{Cast: &CastStmt{Expr: ExprStmt{RecordRead: read}, TargetType: "string"}}},
	}}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded BlockStmt
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}

	ret, ok := decoded.Children[0].(*ReturnStmt)
	if !ok {
		t.Fatalf("child = %T, want *ReturnStmt", decoded.Children[0])
	}
	got := ret.Value.Cast.Expr.RecordRead
	record, ok := got.Record.(*ASTRecord)
	if !ok {
		t.Fatalf("Record = %T, want *ASTRecord", got.Record)
	}
	if record.Type != table || record.GetType() != NodeTypeRecord {
		t.Errorf("Record = %+v, want the %s record node", record, table)
	}
	if got.Expression.Expression != read.Expression.Expression {
		t.Errorf("Expression = %q, want %q (the record must not displace its siblings)", got.Expression.Expression, read.Expression.Expression)
	}
}

func TestRecordWriteRecordRoundTrips(t *testing.T) {
	const table = "AsPolicy"
	write := NewRecordWrite(RecordTypeTable, NewRecord(table, Identifier{Type: table}).Build()).
		WithArgument("arg1", ExprStmt{}).
		Build()

	data, err := MarshalStatement(write)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := StatementMarshaler.UnmarshalByType(data)
	if err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}

	got, ok := decoded.(*RecordWriteStmt)
	if !ok {
		t.Fatalf("decoded = %T, want *RecordWriteStmt", decoded)
	}
	if record, ok := got.Record.(*ASTRecord); !ok || record.Type != table {
		t.Errorf("Record = %#v, want the %s record node", got.Record, table)
	}
	if len(got.Arguments) != 1 || *got.Arguments[0].Name != "arg1" {
		t.Errorf("Arguments = %+v, want arg1", got.Arguments)
	}
}
