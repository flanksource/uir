package graph_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir/graph"
)

var _ = Describe("data node ids", func() {
	DescribeTable("DataID and SplitDataID name a data node by its kind and name",
		func(kind, name, id string) {
			Expect(graph.DataID(kind, name)).To(Equal(id))
			gotKind, gotName, ok := graph.SplitDataID(id)
			Expect([]any{gotKind, gotName, ok}).To(Equal([]any{kind, name, true}))
		},
		Entry("a table", graph.DataTable, "AsPolicy", "table:AsPolicy"),
		Entry("a schema-qualified column", graph.DataColumn, "dbo.AsPolicy.STATUSCODE", "column:dbo.AsPolicy.STATUSCODE"),
		Entry("a stored procedure", graph.DataProcedure, "Update_ClassGroup", "procedure:Update_ClassGroup"),
		Entry("a database function", graph.DataFunction, "fn_Age", "sqlfunction:fn_Age"),
		Entry("an entity", graph.DataEntity, "Policy", "entity:Policy"),
		Entry("a field", graph.DataField, "Policy.StatusCode", "field:Policy.StatusCode"),
	)

	DescribeTable("SplitDataID refuses an id that names no data",
		func(id string) {
			_, _, ok := graph.SplitDataID(id)
			Expect(ok).To(BeFalse())
		},
		Entry("a call-graph node", "transaction:0A"),
		Entry("no kind", "AsPolicy"),
		Entry("a function rule, not a database function", "function:Fee"),
	)

	DescribeTable("SplitMember splits a column at its last dot and a field at its first",
		func(kind, name, owner, member string) {
			gotOwner, gotMember := graph.SplitMember(kind, name)
			Expect([]string{gotOwner, gotMember}).To(Equal([]string{owner, member}))
		},
		Entry("a column", graph.DataColumn, "AsPolicy.STATUSCODE", "AsPolicy", "STATUSCODE"),
		Entry("a column of a schema-qualified table", graph.DataColumn, "audit.AsPlanLog.X", "audit.AsPlanLog", "X"),
		Entry("a nested field", graph.DataField, "Valuation.Fund.Units", "Valuation", "Fund.Units"),
		Entry("a name with no member", graph.DataEntity, "Policy", "Policy", ""),
	)

	It("names an unresolved leaf by the kind of reference and its name", func() {
		Expect(graph.UnresolvedID("function", "Function-Missing")).To(Equal("unresolved:function:Function-Missing"))
	})
})
