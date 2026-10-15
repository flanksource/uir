package diff_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/diff"
)

func TestDiffSpecs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Diff")
}

const schemaName = "public"

// row is a change reduced to what the specs assert: nested changes are
// flattened, and the node refs reduce to the symbol they name.
type row struct {
	Kind   diff.ChangeKind
	Path   string
	Symbol string
	Detail string
	Layer  uir.Layer
}

func rowsOf(result diff.DiffResult) []row {
	rows := []row{}
	var walk func([]diff.DiffChange)
	walk = func(changes []diff.DiffChange) {
		for _, change := range changes {
			r := row{Kind: change.Kind, Path: change.Path, Detail: change.Detail, Layer: change.Layer}
			switch {
			case change.After != nil:
				r.Symbol = change.After.Symbol
			case change.Before != nil:
				r.Symbol = change.Before.Symbol
			}
			rows = append(rows, r)
			walk(change.Changes)
		}
	}
	walk(result.Changes)
	return rows
}

func symbolOf(node uir.Node) string { return node.GetIdentifier().SymbolKey() }

func memberSymbol(t uir.RecordTable, name string) string {
	for _, child := range t.GetChildren() {
		if child.GetIdentifier().GetName() == name {
			return symbolOf(child)
		}
	}
	panic("table " + t.Type + " has no member " + name)
}

func idColumn() uir.RecordColumn {
	column := uir.NewColumn("id", "int", uir.RecordFieldTypeInt)
	column.PrimaryKey = true
	return column
}

func totalColumn(name string) uir.RecordColumn {
	return uir.NewColumn(name, "decimal", uir.RecordFieldTypeFloat)
}

func placedAtColumn() uir.RecordColumn {
	return uir.NewColumn("placed_at", "timestamp", uir.RecordFieldTypeDate)
}

func statusColumn() uir.RecordColumn {
	return uir.NewColumn("status", "varchar", uir.RecordFieldTypeString)
}

func table(name string, columns ...uir.RecordColumn) *uir.TableBuilder {
	return uir.NewTable(name, uir.Identifier{Package: schemaName}).WithColumns(columns...)
}

func withRowCount(t uir.RecordTable, count int64) uir.RecordTable {
	t.RowCount = &count
	return t
}

func view(name, definition string) uir.RecordTable {
	t := table(name, totalColumn("total")).Build()
	t.RecordType = uir.RecordTypeView
	t.Definition = blockWithRaw(definition)
	return t
}

func trigger(name, body string) uir.MethodNode {
	method := uir.MethodNode{Body: blockWithRaw(body)}
	method.Method = name
	method.NodeType = uir.NodeTypeMethod
	method.Properties = map[string]any{uir.TriggerEventProperty: "insert", uir.TriggerTimingProperty: "after"}
	return method
}

func schema(tables ...uir.RecordTable) *uir.UIR { return &uir.UIR{Tables: tables} }

// The node structs embed unexported bases, so fixtures set the promoted fields
// by assignment rather than through composite literal keys.
func recordWithField(recordName, fieldName string) uir.ASTRecord {
	field := uir.RecordField{FieldType: uir.RecordFieldTypeString}
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
