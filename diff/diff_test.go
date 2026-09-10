package diff

import (
	"testing"

	"github.com/flanksource/uir"
)

func TestDiffTreeDetectsExactFieldRename(t *testing.T) {
	before := &uir.UIR{
		Records: []uir.ASTRecord{recordWithField("User", "name", uir.RecordFieldTypeString)},
	}
	after := &uir.UIR{
		Records: []uir.ASTRecord{recordWithField("User", "fullName", uir.RecordFieldTypeString)},
	}

	result := DiffTree(before, after, DefaultTreeDiffOptions())

	if !hasChange(result, ChangeRenamed) {
		t.Fatalf("expected rename change, got %#v", result.Changes)
	}
	if result.Summary.Added != 0 || result.Summary.Deleted != 0 {
		t.Fatalf("rename should not be reported as add/delete, summary=%#v changes=%#v", result.Summary, result.Changes)
	}
}

func TestDiffTreeCanDisableExactRenameDetection(t *testing.T) {
	before := &uir.UIR{
		Records: []uir.ASTRecord{recordWithField("User", "name", uir.RecordFieldTypeString)},
	}
	after := &uir.UIR{
		Records: []uir.ASTRecord{recordWithField("User", "fullName", uir.RecordFieldTypeString)},
	}

	result := DiffTree(before, after, TreeDiffOptions{})

	if hasChange(result, ChangeRenamed) {
		t.Fatalf("expected rename detection to be disabled, got %#v", result.Changes)
	}
	if result.Summary.Added == 0 || result.Summary.Deleted == 0 {
		t.Fatalf("disabled rename detection should leave add/delete, summary=%#v changes=%#v", result.Summary, result.Changes)
	}
}

func TestDiffNodeDetectsNestedFieldRename(t *testing.T) {
	before := recordWithField("User", "name", uir.RecordFieldTypeString)
	after := recordWithField("User", "fullName", uir.RecordFieldTypeString)

	result := DiffNode(before, after, DefaultNodeDiffOptions())

	if !hasChange(result, ChangeRenamed) {
		t.Fatalf("expected nested field rename, got %#v", result.Changes)
	}
	if result.Summary.Added != 0 || result.Summary.Deleted != 0 {
		t.Fatalf("rename should not be reported as add/delete, summary=%#v changes=%#v", result.Summary, result.Changes)
	}
}

func TestDiffTreeDetectsExactMethodMove(t *testing.T) {
	before := &uir.UIR{
		Types: []uir.TypedNode{
			typeWithMethods("Source", methodWithBody("Run", "return x")),
			typeWithMethods("Target"),
		},
	}
	after := &uir.UIR{
		Types: []uir.TypedNode{
			typeWithMethods("Source"),
			typeWithMethods("Target", methodWithBody("Run", "return x")),
		},
	}

	result := DiffTree(before, after, DefaultTreeDiffOptions())

	if !hasChange(result, ChangeMoved) {
		t.Fatalf("expected move change, got %#v", result.Changes)
	}
}

func TestDiffNodeDetectsMethodMoveInList(t *testing.T) {
	before := uir.NodeList{methodForType("Source", "Run", "return x")}
	after := uir.NodeList{methodForType("Target", "Run", "return x")}

	result := DiffNode(before, after, DefaultNodeDiffOptions())

	if !hasChange(result, ChangeMoved) {
		t.Fatalf("expected method move, got %#v", result.Changes)
	}
	if result.Summary.Added != 0 || result.Summary.Deleted != 0 {
		t.Fatalf("move should not be reported as add/delete, summary=%#v changes=%#v", result.Summary, result.Changes)
	}
}

func TestDiffNodeReportsStatementBodyChange(t *testing.T) {
	before := methodWithBody("Run", "return x")
	after := methodWithBody("Run", "return y")

	result := DiffNode(before, after, DefaultNodeDiffOptions())

	if !hasChange(result, ChangeDeleted) || !hasChange(result, ChangeAdded) {
		t.Fatalf("expected statement add/delete from body diff, got %#v", result.Changes)
	}
}

func TestDiffNodeCanDisableStatementBodyDiff(t *testing.T) {
	before := methodWithBody("Run", "return x")
	after := methodWithBody("Run", "return y")

	result := DiffNode(before, after, NodeDiffOptions{Statements: false})

	if hasChange(result, ChangeAdded) || hasChange(result, ChangeDeleted) {
		t.Fatalf("expected statement add/delete to be disabled, got %#v", result.Changes)
	}
	if !hasChange(result, ChangeModified) {
		t.Fatalf("expected method modification summary, got %#v", result.Changes)
	}
}

func hasChange(result DiffResult, kind ChangeKind) bool {
	for _, change := range result.Changes {
		if change.Kind == kind {
			return true
		}
		for _, child := range change.Changes {
			if child.Kind == kind {
				return true
			}
		}
	}
	return false
}

// The node structs embed unexported bases, so fixtures set the promoted fields
// by assignment rather than through composite literal keys.
func recordWithField(recordName, fieldName string, fieldType uir.RecordFieldType) uir.ASTRecord {
	field := uir.RecordField{FieldType: fieldType}
	field.Identifier = uir.Identifier{Package: "pkg", Type: recordName, Field: fieldName, NodeType: uir.NodeTypeField}

	record := uir.ASTRecord{Fields: []uir.RecordField{field}}
	record.Identifier = uir.Identifier{Package: "pkg", Type: recordName, NodeType: uir.NodeTypeRecord}
	return record
}

func typeWithMethods(name string, methods ...uir.MethodNode) uir.TypedNode {
	for i := range methods {
		methods[i].Package = "pkg"
		methods[i].Type = name
	}

	typed := uir.TypedNode{Methods: methods}
	typed.Identifier = uir.Identifier{Package: "pkg", Type: name, NodeType: uir.NodeTypeType}
	return typed
}

func methodForType(typeName, methodName, raw string) uir.MethodNode {
	method := methodWithBody(methodName, raw)
	method.Type = typeName
	return method
}

func methodWithBody(name, raw string) uir.MethodNode {
	method := uir.MethodNode{Body: blockWithRaw(raw)}
	method.Identifier = uir.Identifier{Package: "pkg", Type: "Service", Method: name, NodeType: uir.NodeTypeMethod}
	return method
}

func blockWithRaw(raw string) *uir.BlockStmt {
	stmt := uir.RawStmt{Source: raw}
	stmt.Type = uir.ASTStatementTypeRaw

	block := &uir.BlockStmt{Children: []uir.Statement{stmt}}
	block.Type = uir.ASTStatementTypeBlock
	return block
}
