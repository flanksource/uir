package uir_test

import (
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
)

// layerEquality records, per layer, whether a change left that layer's hash equal.
type layerEquality struct {
	Public, Private, Content bool
}

var (
	unchanged     = layerEquality{Public: true, Private: true, Content: true}
	publicChange  = layerEquality{Public: false, Private: false, Content: true}
	privateChange = layerEquality{Public: true, Private: false, Content: true}
	contentChange = layerEquality{Public: true, Private: true, Content: false}
)

func layerEqualityOf(before, after uir.Node) layerEquality {
	b, a := uir.Fingerprint(before), uir.Fingerprint(after)
	return layerEquality{Public: b.Public == a.Public, Private: b.Private == a.Private, Content: b.Content == a.Content}
}

func ordersColumns() []uir.RecordColumn {
	id := uir.NewColumn("id", "bigint", uir.RecordFieldTypeInt)
	id.PrimaryKey = true
	id.AutoIncrement = true
	name := uir.NewColumn("name", "text", uir.RecordFieldTypeString)
	name.Nullable = true
	return []uir.RecordColumn{id, name}
}

func ordersTable(columns ...uir.RecordColumn) *uir.TableBuilder {
	if len(columns) == 0 {
		columns = ordersColumns()
	}
	return uir.NewTable("orders", uir.Identifier{Package: "public"}).WithColumns(columns...)
}

func auditTrigger(timing, body string) uir.MethodNode {
	trigger := uir.NewMethod("orders_audit").WithBody(blockOf(callTo(body))).Build()
	trigger.Properties = map[string]any{uir.TriggerEventProperty: "insert", uir.TriggerTimingProperty: timing}
	return trigger
}

func ordersView(definition string) uir.RecordTable {
	view := ordersTable().Build()
	view.RecordType = uir.RecordTypeView
	block := blockOf(callTo(definition))
	view.Definition = &block
	return view
}

func accountStruct(fields ...uir.RecordField) uir.TypedNode {
	balance := uir.Field("Balance", uir.RecordFieldTypeInt)
	balance.Visibility = uir.VisibilityPublic
	return uir.NewType("Account").WithVisibility(uir.VisibilityPublic).
		WithVariables(append([]uir.RecordField{balance}, fields...)...).Build()
}

func privateField(name string) uir.RecordField {
	field := uir.Field(name, uir.RecordFieldTypeString)
	field.Visibility = uir.VisibilityPrivate
	return field
}

func chargeMethod(body, paramSQLType string) uir.MethodNode {
	amount := uir.Field("amount", uir.RecordFieldTypeInt)
	amount.SQLType = paramSQLType
	return uir.NewMethod("Charge").WithVisibility(uir.VisibilityPublic).
		WithParam(amount).WithBody(blockOf(callTo(body))).Build()
}

var _ = Describe("Fingerprint", func() {
	DescribeTable("classifies a change by the layers it touches",
		func(change func() (uir.Node, uir.Node), expected layerEquality) {
			before, after := change()
			Expect(layerEqualityOf(before, after)).To(Equal(expected))

			// A shape diff never disagrees with its hash, and Hash() moves exactly
			// when one of the three layers does.
			Expect(uir.Shape(before, uir.LayerPublic) == uir.Shape(after, uir.LayerPublic)).To(Equal(expected.Public))
			Expect(uir.Shape(before, uir.LayerPrivate) == uir.Shape(after, uir.LayerPrivate)).To(Equal(expected.Private))
			Expect(before.Hash() == after.Hash()).To(Equal(expected == unchanged))
		},
		Entry("adding a nullable column changes the table's public layer", func() (uir.Node, uir.Node) {
			note := uir.NewColumn("note", "text", uir.RecordFieldTypeString)
			note.Nullable = true
			return ordersTable().Build(), ordersTable().WithColumns(note).Build()
		}, publicChange),
		Entry("adding an index is a private change of the table", func() (uir.Node, uir.Node) {
			return ordersTable().Build(), ordersTable().WithIndexes(uir.NewIndex("ix_orders_name", "name")).Build()
		}, privateChange),
		Entry("adding a foreign key is a private change of the table", func() (uir.Node, uir.Node) {
			fk := uir.NewForeignKey("fk_orders_customer", []string{"id"}, "customers", []string{"id"})
			return ordersTable().Build(), ordersTable().WithForeignKeys(fk).Build()
		}, privateChange),
		Entry("reordering columns is a private change of the table", func() (uir.Node, uir.Node) {
			columns := ordersColumns()
			return ordersTable().Build(), ordersTable(columns[1], columns[0]).Build()
		}, privateChange),
		Entry("rewriting a view definition with the same output columns is a content change", func() (uir.Node, uir.Node) {
			return ordersView("select_v1"), ordersView("select_v2")
		}, contentChange),
		Entry("changing a trigger body is a content change of the table", func() (uir.Node, uir.Node) {
			return ordersTable().WithTriggers(auditTrigger("after", "log_v1")).Build(),
				ordersTable().WithTriggers(auditTrigger("after", "log_v2")).Build()
		}, contentChange),
		Entry("changing a trigger body is a content change of the trigger", func() (uir.Node, uir.Node) {
			return auditTrigger("after", "log_v1"), auditTrigger("after", "log_v2")
		}, contentChange),
		Entry("changing a trigger's timing is a private change of the table", func() (uir.Node, uir.Node) {
			return ordersTable().WithTriggers(auditTrigger("after", "log")).Build(),
				ordersTable().WithTriggers(auditTrigger("before", "log")).Build()
		}, privateChange),
		Entry("changing only the row count touches no layer", func() (uir.Node, uir.Node) {
			before, after := ordersTable().Build(), ordersTable().Build()
			rows := int64(42)
			after.RowCount = &rows
			return before, after
		}, unchanged),
		Entry("changing only source text and location touches no layer", func() (uir.Node, uir.Node) {
			before, after := ordersTable().Build(), ordersTable().Build()
			text := "CREATE TABLE orders (id bigint)  -- reformatted"
			after.Content = &text
			after.Path = "schema/orders.sql"
			return before, after
		}, unchanged),
		Entry("changing a column default is a private change of the column", func() (uir.Node, uir.Node) {
			before := uir.NewColumn("status", "text", uir.RecordFieldTypeString)
			after := before
			pending := "pending"
			after.DefaultValue = &uir.TypedValue{Str: &pending}
			return before, after
		}, privateChange),
		Entry("making a column nullable is a public change of the column", func() (uir.Node, uir.Node) {
			before := uir.NewColumn("status", "text", uir.RecordFieldTypeString)
			after := before
			after.Nullable = true
			return before, after
		}, publicChange),
		Entry("the first unexported field of an exported struct flips its opaque marker", func() (uir.Node, uir.Node) {
			return accountStruct(), accountStruct(privateField("ledger"))
		}, publicChange),
		Entry("a further unexported field of an already-opaque struct is private", func() (uir.Node, uir.Node) {
			return accountStruct(privateField("ledger")), accountStruct(privateField("ledger"), privateField("audit"))
		}, privateChange),
		Entry("an exported field of a struct is public", func() (uir.Node, uir.Node) {
			owner := uir.Field("Owner", uir.RecordFieldTypeString)
			owner.Visibility = uir.VisibilityPublic
			return accountStruct(), accountStruct(owner)
		}, publicChange),
		Entry("editing a method body is a content change", func() (uir.Node, uir.Node) {
			return chargeMethod("debit_v1", "bigint"), chargeMethod("debit_v2", "bigint")
		}, contentChange),
		Entry("retyping a parameter's SQL type is a public change", func() (uir.Node, uir.Node) {
			return chargeMethod("debit", "bigint"), chargeMethod("debit", "numeric")
		}, publicChange),
		Entry("adding an unexported function is private, and its body is content", func() (uir.Node, uir.Node) {
			helper := uir.NewMethod("helper").WithVisibility(uir.VisibilityPrivate).WithBody(blockOf(callTo("x"))).Build()
			before := *uir.NewPackage("billing").WithFunction(chargeMethod("debit", "bigint")).Build()
			after := before
			after.Functions = append(append([]uir.MethodNode(nil), before.Functions...), helper)
			return before, after
		}, layerEquality{Public: true, Private: false, Content: false}),
	)

	It("renders one member per line, the public layer as a set and the private layer in declared order", func() {
		columns := ordersColumns()
		table := ordersTable(columns[1], columns[0]).WithIndexes(uir.NewIndex("ix_orders_name", "name")).Build()

		Expect(uir.Shape(table, uir.LayerPublic)).To(Equal("table sql:table {\n" +
			"  column id bigint (int) not null identity\n" +
			"  column name text (string) null\n" +
			"}"))
		Expect(uir.Shape(table, uir.LayerPrivate)).To(Equal("table sql:table {\n" +
			"  column name text (string) null\n" +
			"  column id bigint (int) not null identity pk\n" +
			"  primary key (id)\n" +
			"  index ix_orders_name (name)\n" +
			"}"))
		Expect(uir.Shape(table, uir.LayerContent)).To(BeEmpty())
	})

	It("composes the private layer from the public one, so a public change is always a private change", func() {
		before := uir.NewColumn("status", "text", uir.RecordFieldTypeString)
		after := before
		after.SQLType = "varchar"
		Expect(uir.Fingerprint(after).Public).NotTo(Equal(uir.Fingerprint(before).Public))
		Expect(uir.Fingerprint(after).Private).NotTo(Equal(uir.Fingerprint(before).Private))
	})

	It("handles every registered node type, by value and by pointer", func() {
		for _, node := range uir.Nodes {
			pointer := reflect.New(reflect.TypeOf(node))
			pointer.Elem().Set(reflect.ValueOf(node))
			Expect(func() { uir.Fingerprint(node) }).NotTo(Panic(), "%T", node)
			Expect(func() { uir.Fingerprint(pointer.Interface().(uir.Node)) }).NotTo(Panic(), "*%T", node)
		}
	})

	It("panics on a node type it has no layers for", func() {
		Expect(func() { uir.Fingerprint(uir.NodeList{}) }).To(PanicWith(ContainSubstring("uir.NodeList")))
	})
})

var _ = Describe("MemberLine", func() {
	It("is exactly the line its container's shape embeds, inline for a leaf and by hash for a container", func() {
		table := ordersTable().WithTriggers(auditTrigger("after", "log")).Build()
		account := accountStruct(privateField("ledger"))
		billing := *uir.NewPackage("billing").WithType(account).Build()
		for _, layer := range []uir.Layer{uir.LayerPublic, uir.LayerPrivate} {
			Expect(uir.Shape(table, layer)).To(ContainSubstring("\n  " + uir.MemberLine(table.Columns[1], layer) + "\n"))
			Expect(uir.Shape(billing, layer)).To(ContainSubstring("\n  " + uir.MemberLine(&account, layer) + "\n"))
		}
		Expect(uir.MemberLine(table.Columns[1], uir.LayerPrivate)).To(Equal("column name text (string) null"))
		Expect(uir.MemberLine(account, uir.LayerPrivate)).To(Equal("class Account public {…} " + uir.Fingerprint(account).Private))
		Expect(uir.MemberLine(table.Triggers[0], uir.LayerPrivate)).To(Equal("method orders_audit () event=insert timing=after"))
	})

	It("has no line in the content layer", func() {
		Expect(func() { uir.MemberLine(ordersTable().Build(), uir.LayerContent) }).To(PanicWith(ContainSubstring(`layer "content"`)))
	})
})

var _ = DescribeTable("IsPublic is whether a member reaches its container's public layer, by value and by pointer",
	func(node uir.Node, public bool) {
		Expect(uir.IsPublic(node)).To(Equal(public))
	},
	Entry("a public method", chargeMethod("debit", "bigint"), true),
	Entry("a private field", privateField("ledger"), false),
	Entry("a pointer to a private field", func() uir.Node { field := privateField("ledger"); return &field }(), false),
	Entry("a pointer to a private type", func() uir.Node {
		typ := uir.NewType("ledger").WithVisibility(uir.VisibilityPrivate).Build()
		return &typ
	}(), false),
	Entry("a column, which has no visibility", uir.NewColumn("id", "bigint", uir.RecordFieldTypeInt), true),
)

var _ = Describe("TypedNode", func() {
	It("lists its variables among its children, as Fingerprint composes them", func() {
		account := accountStruct(privateField("ledger"))
		withCtor := uir.NewType("Account").WithVariables(account.Variables...).
			WithConstructor(uir.NewMethod("NewAccount").Build()).WithMethod(chargeMethod("debit", "bigint")).Build()
		var names []string
		for _, child := range withCtor.GetChildren() {
			names = append(names, uir.MemberName(child))
		}
		Expect(names).To(Equal([]string{"Balance", "ledger", "NewAccount", "Charge"}))
	})
})

var _ = Describe("TypeBuilder", func() {
	It("qualifies members with their type whatever order the builder is called in, so a shared member name keeps distinct symbol keys", func() {
		build := func(typeName string) uir.TypedNode {
			return uir.NewType(typeName).
				WithVariables(uir.Field("id", uir.RecordFieldTypeString)).
				WithMethod(uir.NewMethod("Save").Build()).
				WithConstructor(uir.NewMethod(typeName, uir.Identifier{NodeType: uir.NodeTypeConstructor}).Build()).
				WithDestructor(uir.NewMethod("close").Build()).
				WithModule("example.org/app").WithPackage("example.org/app/billing").Build()
		}
		account, invoice := build("Account"), build("Invoice")
		owner := uir.Identifier{Module: "example.org/app", Package: "example.org/app/billing", Type: "Account"}

		Expect([]uir.Identifier{
			account.Variables[0].Identifier, account.Methods[0].Identifier,
			account.Constructor.Identifier, account.Destructor.Identifier,
		}).To(Equal([]uir.Identifier{
			{Module: owner.Module, Package: owner.Package, Type: owner.Type, Field: "id", NodeType: uir.NodeTypeField},
			{Module: owner.Module, Package: owner.Package, Type: owner.Type, Method: "Save", NodeType: uir.NodeTypeMethod},
			{Module: owner.Module, Package: owner.Package, Type: owner.Type, Method: "Account", NodeType: uir.NodeTypeConstructor},
			{Module: owner.Module, Package: owner.Package, Type: owner.Type, Method: "close", NodeType: uir.NodeTypeMethod},
		}))
		Expect(account.Variables[0].SymbolKey()).NotTo(Equal(invoice.Variables[0].SymbolKey()))
		Expect(account.Methods[0].SymbolKey()).NotTo(Equal(invoice.Methods[0].SymbolKey()))
	})
})

var _ = Describe("TableBuilder", func() {
	It("qualifies members with their table, so a shared column name keeps distinct symbol keys", func() {
		orders := ordersTable().WithTriggers(auditTrigger("after", "log")).Build()
		customers := uir.NewTable("customers", uir.Identifier{Package: "public"}).
			WithColumns(uir.NewColumn("id", "bigint", uir.RecordFieldTypeInt)).Build()

		Expect(orders.Columns[0].Identifier).To(Equal(uir.Identifier{
			Package: "public", Type: "orders", Field: "id", NodeType: uir.NodeTypeColumn,
		}))
		Expect(orders.Columns[0].SymbolKey()).NotTo(Equal(customers.Columns[0].SymbolKey()))
		Expect(orders.Triggers[0].Identifier).To(Equal(uir.Identifier{
			Package: "public", Type: "orders", Method: "orders_audit", NodeType: uir.NodeTypeMethod,
		}))
	})
})
