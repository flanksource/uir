package diff_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/diff"
)

var (
	orders             = table("orders", idColumn(), totalColumn("total"), placedAtColumn()).Build()
	ordersWithStatus   = table("orders", idColumn(), statusColumn(), totalColumn("total"), placedAtColumn()).Build()
	ordersIDLast       = table("orders", totalColumn("total"), placedAtColumn(), idColumn()).Build()
	ordersAmount       = table("orders", idColumn(), totalColumn("amount"), placedAtColumn()).Build()
	ordersIndexed      = table("orders", idColumn(), totalColumn("total"), placedAtColumn()).WithIndexes(uir.NewIndex("ix_total", "total")).Build()
	customers          = table("customers", idColumn()).Build()
	ordersIDOnly       = table("orders", idColumn()).Build()
	purchasesIDOnly    = table("purchases", idColumn()).Build()
	ordersAudited      = table("orders", idColumn()).WithTriggers(trigger("audit", "insert into audit_log select 1")).Build()
	ordersAuditedAgain = table("orders", idColumn()).WithTriggers(trigger("audit", "insert into audit_log select 2")).Build()
	orderTotals        = view("order_totals", "select sum(total) from orders")
	orderTotalsAgain   = view("order_totals", "select sum(total) from orders where paid")

	ordersKey      = symbolOf(orders)
	orderTotalsKey = symbolOf(orderTotals)
	auditKey       = memberSymbol(ordersAudited, "audit")
)

var _ = Describe("DiffTree", func() {
	DescribeTable("classifies schema and code changes",
		func(opts diff.TreeDiffOptions, before, after *uir.UIR, expected []row) {
			Expect(rowsOf(diff.DiffTree(before, after, opts))).To(ConsistOf(expected))
		},
		Entry("tables sharing a column name diff as unchanged",
			diff.DefaultTreeDiffOptions(), schema(orders, customers), schema(orders, customers), []row{}),
		Entry("a row-count change is no change",
			diff.DefaultTreeDiffOptions(), schema(withRowCount(orders, 10)), schema(withRowCount(orders, 20)), []row{}),
		Entry("a column inserted mid-table is added, the shifted columns are not moved",
			diff.DefaultTreeDiffOptions(), schema(orders), schema(ordersWithStatus), []row{
				{Kind: diff.ChangeAdded, Path: memberSymbol(ordersWithStatus, "status"), Symbol: memberSymbol(ordersWithStatus, "status")},
				{Kind: diff.ChangeModified, Path: ordersKey, Symbol: ordersKey, Detail: "table attributes", Layer: uir.LayerPublic},
			}),
		Entry("a column reordered out of sequence is moved and the table changes privately",
			diff.DefaultTreeDiffOptions(), schema(orders), schema(ordersIDLast), []row{
				{Kind: diff.ChangeMoved, Path: ordersKey + ".columns", Symbol: memberSymbol(orders, "id"), Detail: "column position 1 -> 3"},
				{Kind: diff.ChangeModified, Path: ordersKey, Symbol: ordersKey, Detail: "table attributes", Layer: uir.LayerPrivate},
			}),
		Entry("an added index is added and the table changes privately",
			diff.DefaultTreeDiffOptions(), schema(orders), schema(ordersIndexed), []row{
				{Kind: diff.ChangeAdded, Path: memberSymbol(ordersIndexed, "ix_total"), Symbol: memberSymbol(ordersIndexed, "ix_total")},
				{Kind: diff.ChangeModified, Path: ordersKey, Symbol: ordersKey, Detail: "table attributes", Layer: uir.LayerPrivate},
			}),
		Entry("a rewritten view definition is a content change with statement changes",
			diff.DefaultTreeDiffOptions(), schema(orderTotals), schema(orderTotalsAgain), []row{
				{Kind: diff.ChangeAdded, Path: orderTotalsKey + ".definition", Detail: "statement 0"},
				{Kind: diff.ChangeDeleted, Path: orderTotalsKey + ".definition", Detail: "statement 0"},
				{Kind: diff.ChangeModified, Path: orderTotalsKey, Symbol: orderTotalsKey, Detail: "table attributes", Layer: uir.LayerContent},
			}),
		Entry("a trigger body change is a content change of the trigger",
			diff.DefaultTreeDiffOptions(), schema(ordersAudited), schema(ordersAuditedAgain), []row{
				{Kind: diff.ChangeAdded, Path: auditKey + ".body", Detail: "statement 0"},
				{Kind: diff.ChangeDeleted, Path: auditKey + ".body", Detail: "statement 0"},
				{Kind: diff.ChangeModified, Path: auditKey, Symbol: auditKey, Detail: "method attributes", Layer: uir.LayerContent},
				{Kind: diff.ChangeModified, Path: ordersKey, Symbol: ordersKey, Detail: "table attributes", Layer: uir.LayerContent},
			}),
		Entry("a renamed column is renamed",
			diff.DefaultTreeDiffOptions(), schema(orders), schema(ordersAmount), []row{
				{Kind: diff.ChangeRenamed, Path: memberSymbol(ordersAmount, "amount"), Symbol: memberSymbol(ordersAmount, "amount")},
				{Kind: diff.ChangeModified, Path: ordersKey, Symbol: ordersKey, Detail: "table attributes", Layer: uir.LayerPublic},
			}),
		Entry("a renamed table is renamed and its column moves with it",
			diff.DefaultTreeDiffOptions(), schema(ordersIDOnly), schema(purchasesIDOnly), []row{
				{Kind: diff.ChangeRenamed, Path: symbolOf(purchasesIDOnly), Symbol: symbolOf(purchasesIDOnly)},
				{Kind: diff.ChangeMoved, Path: memberSymbol(purchasesIDOnly, "id"), Symbol: memberSymbol(purchasesIDOnly, "id")},
			}),
		Entry("a renamed record field is renamed",
			diff.DefaultTreeDiffOptions(),
			&uir.UIR{Records: []uir.ASTRecord{recordWithField("User", "name")}},
			&uir.UIR{Records: []uir.ASTRecord{recordWithField("User", "fullName")}}, []row{
				{Kind: diff.ChangeRenamed, Path: symbolOf(recordWithField("User", "fullName").Fields[0]), Symbol: symbolOf(recordWithField("User", "fullName").Fields[0])},
				{Kind: diff.ChangeModified, Path: symbolOf(recordWithField("User", "name")), Symbol: symbolOf(recordWithField("User", "name")), Detail: "record attributes", Layer: uir.LayerPublic},
			}),
		Entry("a renamed record field is a delete and add with rename detection off",
			diff.TreeDiffOptions{},
			&uir.UIR{Records: []uir.ASTRecord{recordWithField("User", "name")}},
			&uir.UIR{Records: []uir.ASTRecord{recordWithField("User", "fullName")}}, []row{
				{Kind: diff.ChangeDeleted, Path: symbolOf(recordWithField("User", "name").Fields[0]), Symbol: symbolOf(recordWithField("User", "name").Fields[0])},
				{Kind: diff.ChangeAdded, Path: symbolOf(recordWithField("User", "fullName").Fields[0]), Symbol: symbolOf(recordWithField("User", "fullName").Fields[0])},
				{Kind: diff.ChangeModified, Path: symbolOf(recordWithField("User", "name")), Symbol: symbolOf(recordWithField("User", "name")), Detail: "record attributes", Layer: uir.LayerPublic},
			}),
		Entry("a method moved between types is moved",
			diff.DefaultTreeDiffOptions(),
			&uir.UIR{Types: []uir.TypedNode{typeWithMethods("Source", methodWithBody("Run", "return x")), typeWithMethods("Target")}},
			&uir.UIR{Types: []uir.TypedNode{typeWithMethods("Source"), typeWithMethods("Target", methodWithBody("Run", "return x"))}}, []row{
				{Kind: diff.ChangeMoved, Path: "method:pkg.Target:Run", Symbol: "method:pkg.Target:Run"},
				{Kind: diff.ChangeModified, Path: symbolOf(typeWithMethods("Source")), Symbol: symbolOf(typeWithMethods("Source")), Detail: "type attributes", Layer: uir.LayerPublic},
				{Kind: diff.ChangeModified, Path: symbolOf(typeWithMethods("Target")), Symbol: symbolOf(typeWithMethods("Target")), Detail: "type attributes", Layer: uir.LayerPublic},
			}),
	)
})

var _ = Describe("DiffNode", func() {
	DescribeTable("pairs members by identity only",
		func(opts diff.NodeDiffOptions, before, after uir.Node, expected []row) {
			Expect(rowsOf(diff.DiffNode(before, after, opts))).To(ConsistOf(expected))
		},
		Entry("an unrelated table of the same structure is a delete and an add, not a rename",
			diff.DefaultNodeDiffOptions(), uir.NodeList{ordersIDOnly}, uir.NodeList{customers}, []row{
				{Kind: diff.ChangeDeleted, Path: "nodes", Symbol: symbolOf(ordersIDOnly)},
				{Kind: diff.ChangeAdded, Path: "nodes", Symbol: symbolOf(customers)},
			}),
		Entry("a renamed record field is a delete and an add",
			diff.DefaultNodeDiffOptions(), recordWithField("User", "name"), recordWithField("User", "fullName"), []row{
				{Kind: diff.ChangeDeleted, Path: "record:pkg.User.fields", Symbol: symbolOf(recordWithField("User", "name").Fields[0])},
				{Kind: diff.ChangeAdded, Path: "record:pkg.User.fields", Symbol: symbolOf(recordWithField("User", "fullName").Fields[0])},
				{Kind: diff.ChangeModified, Path: "record:pkg.User", Symbol: "record:pkg.User", Detail: "record attributes", Layer: uir.LayerPublic},
			}),
		Entry("a column inserted mid-table is added under the table's columns",
			diff.DefaultNodeDiffOptions(), orders, ordersWithStatus, []row{
				{Kind: diff.ChangeAdded, Path: ordersKey + ".columns", Symbol: memberSymbol(ordersWithStatus, "status")},
				{Kind: diff.ChangeModified, Path: ordersKey, Symbol: ordersKey, Detail: "table attributes", Layer: uir.LayerPublic},
			}),
		Entry("a column reordered out of sequence is moved",
			diff.DefaultNodeDiffOptions(), orders, ordersIDLast, []row{
				{Kind: diff.ChangeMoved, Path: ordersKey + ".columns", Symbol: memberSymbol(orders, "id"), Detail: "column position 1 -> 3"},
				{Kind: diff.ChangeModified, Path: ordersKey, Symbol: ordersKey, Detail: "table attributes", Layer: uir.LayerPrivate},
			}),
		Entry("a trigger body change recurses into the trigger",
			diff.DefaultNodeDiffOptions(), ordersAudited, ordersAuditedAgain, []row{
				{Kind: diff.ChangeAdded, Path: auditKey + ".body", Detail: "statement 0"},
				{Kind: diff.ChangeDeleted, Path: auditKey + ".body", Detail: "statement 0"},
				{Kind: diff.ChangeModified, Path: auditKey, Symbol: auditKey, Detail: "method attributes", Layer: uir.LayerContent},
				{Kind: diff.ChangeModified, Path: ordersKey, Symbol: ordersKey, Detail: "table attributes", Layer: uir.LayerContent},
			}),
		Entry("a method body change is a content change with statement changes",
			diff.DefaultNodeDiffOptions(), methodWithBody("Run", "return x"), methodWithBody("Run", "return y"), []row{
				{Kind: diff.ChangeAdded, Path: "method:pkg.Service:Run.body", Detail: "statement 0"},
				{Kind: diff.ChangeDeleted, Path: "method:pkg.Service:Run.body", Detail: "statement 0"},
				{Kind: diff.ChangeModified, Path: "method:pkg.Service:Run", Symbol: "method:pkg.Service:Run", Detail: "method attributes", Layer: uir.LayerContent},
			}),
		Entry("a method body change without statement diffing is only the content change",
			diff.NodeDiffOptions{}, methodWithBody("Run", "return x"), methodWithBody("Run", "return y"), []row{
				{Kind: diff.ChangeModified, Path: "method:pkg.Service:Run", Symbol: "method:pkg.Service:Run", Detail: "method attributes", Layer: uir.LayerContent},
			}),
	)
})
