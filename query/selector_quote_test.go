package query_test

import (
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("QuoteSelectorValue", func() {
	DescribeTable("spells a literal as a selector value",
		func(literal, spelled string) {
			Expect(query.QuoteSelectorValue(literal)).To(Equal(spelled))
		},
		Entry("a plain name is unchanged", "Calc", "Calc"),
		Entry("a hyphenated name is unchanged", "CopyBook-CycleA", "CopyBook-CycleA"),
		Entry("a path keeps its slashes unquoted", "Acme/Ins/rules", "Acme/Ins/rules"),
		Entry("a qualified name keeps its dots", "Acme/Ins/screens.PolicyScreen", "Acme/Ins/screens.PolicyScreen"),
		Entry("a space quotes the value", "Digital Funeral", `"Digital Funeral"`),
		Entry("a path with a space is quoted whole, slashes kept", "Acme/Digital Funeral/rules", `"Acme/Digital Funeral/rules"`),
		Entry("a colon is quoted, not a separator", "Co:Prod", `"Co:Prod"`),
		Entry("a star is escaped", "Rate*", `"Rate\*"`),
		Entry("a question mark is escaped", "Rate?", `"Rate\?"`),
		Entry("a quote is escaped", `Say "Hi"`, `"Say \"Hi\""`),
		Entry("a backslash is escaped", `A\B`, `"A\\B"`),
	)

	DescribeTable("selects exactly the symbol or package it spells",
		func(ctx SpecContext, backend string) {
			pipeline, scope := insurancePipeline(ctx, openQueryDatabase(ctx, backend))
			screen := indexer.SymbolID(insuranceIdentities().screen)
			selected := func(expression string) []string {
				result := runQuery(ctx, pipeline, expression, scope)
				ids := make([]string, 0, len(result.Matches))
				for _, match := range result.Matches {
					ids = append(ids, match.SymbolID)
				}
				return ids
			}
			Expect(selected("kind:acme.screen:"+query.QuoteSelectorValue("PolicyScreen"))).To(Equal([]string{screen}), "a bare name matches the name")
			Expect(selected("kind:acme.screen:"+query.QuoteSelectorValue(insScreens+".PolicyScreen"))).To(Equal([]string{screen}),
				"a value with a slash matches the whole query name")
			Expect(selected("kind:acme.screen:"+query.QuoteSelectorValue("Policy Screen"))).To(BeEmpty(), "a quoted space is literal")
			Expect(runQuery(ctx, pipeline, "pkg:"+query.QuoteSelectorValue(insScreens), scope).Matches).ToNot(BeEmpty(),
				"a package path matches segment by segment")
		},
		Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"),
	)
})
