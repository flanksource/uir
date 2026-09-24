package query_test

import (
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("compact PEG query grammar", func() {
	It("parses precedence, filters, and bounded paths into a typed tree", func() {
		parsed, err := query.Parse("(store.Store.Save < -f _test.go & app.Run >) | main.*")
		Expect(err).ToNot(HaveOccurred())
		Expect(parsed.Expr).To(Equal(&query.Expr{Kind: query.ExprUnion,
			Left: &query.Expr{Kind: query.ExprIntersection,
				Left: &query.Expr{Kind: query.ExprRelation, Relation: "<", Filters: []query.Filter{{Kind: "-f", Value: "_test.go"}},
					Left: &query.Expr{Kind: query.ExprSymbol, Symbol: "store.Store.Save"}},
				Right: &query.Expr{Kind: query.ExprRelation, Relation: ">", Left: &query.Expr{Kind: query.ExprSymbol, Symbol: "app.Run"}}},
			Right: &query.Expr{Kind: query.ExprSymbol, Symbol: "main.*"},
		}))
		path, err := query.Parse("main.* >>4 store.Store.Save")
		Expect(err).ToNot(HaveOccurred())
		Expect(path.Expr.Kind).To(Equal(query.ExprPath))
		Expect(path.Expr.Depth).To(Equal(4))
	})

	DescribeTable("rejects invalid expressions", func(expression string) {
		_, err := query.Parse(expression)
		Expect(err).To(HaveOccurred())
	},
		Entry("empty", ""),
		Entry("old syntax", `references of node where name = "Save"`),
		Entry("unknown relation", "store.Save :uses"),
		Entry("missing filter value", "store.Save < -f"),
		Entry("unbounded depth", "store.Save <<9"),
		Entry("invalid wildcard", "store.S*ve <"),
		Entry("trailing operator", "store.Save &"),
		Entry("second path operator", "app.Run >> store.Save >> app.Done"),
	)
})
