package query_test

import (
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// queryRows is what a query answers, without the stages that describe how it got there.
type queryRows struct {
	Total        int
	Matches      []query.ModuleMatch
	Symbols      []query.ModuleSymbol
	Declarations []query.ModuleMatch
}

func rowsOf(result query.ModuleQueryResult) queryRows {
	return queryRows{Total: result.Total, Matches: result.Matches, Symbols: result.Symbols, Declarations: result.Declarations}
}

// smallCorpus has four products, so eight product and plan roots besides the entities root.
var smallCorpus = corpusShape{products: 4, rules: 6, screens: 3, screenFields: 4, planFields: 3}

var _ = Describe("scope operands", Ordered, func() {
	var pipeline *query.Pipeline
	all := query.ModuleScopeOptions{Limit: 1000}
	BeforeAll(func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		Expect(publishCorpus(ctx, database, smallCorpus)).To(Succeed())
		var err error
		pipeline, err = query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
	})

	// A scope written as `S & S` is no longer a pure scope operand, so the query lists its symbols as a
	// set and filters by them, which is the path every scope operand took before scope predicates.
	DescribeTable("answer exactly what the scope's symbols, listed as a set, answer",
		func(ctx SpecContext, left, operator, scope string, rows int) {
			predicate := runQuery(ctx, pipeline, left+" "+operator+" "+scope, all)
			listed := runQuery(ctx, pipeline, left+" "+operator+" ("+scope+" & "+scope+")", all)
			Expect(rowsOf(predicate)).To(Equal(rowsOf(listed)))
			Expect(predicate.Total).To(Equal(rows))
		},
		Entry("readers of a field in a module tree", "Plan:SchemeNumber", "<", `mod:"Co/Product01/..."`, 6),
		Entry("readers of a field in one module", "Plan:SchemeNumber", "<", "mod:Co/Product01", 3),
		Entry("readers of a field in one package", "Plan:SchemeNumber", "<", "pkg:Co/Product02/Plan/rules", 3),
		Entry("readers of a field in a package relative to matching modules", "Plan:SchemeNumber", "<", `pkg:"Co/Product0*/Plan":rules`, 12),
		Entry("readers of a field in a union of scopes", "Plan:SchemeNumber", "<", "(mod:Co/Product00 | pkg:Co/Product03/rules)", 6),
		Entry("readers of a field in no module", "Plan:SchemeNumber", "<", `mod:"Nowhere/..."`, 0),
		Entry("writers of every Plan field in a module tree", "Plan:*", "~w", `mod:"Co/Product02/..."`, 12),
		Entry("callers of a rule in its own module", "kind:test.rule:Rule002", "<", "mod:Co/Product03", 1),
		Entry("callers of every rule in one package", "kind:test.rule", "<", "pkg:Co/Product01/Plan/rules", 5),
		Entry("screen fields of a module tree", "field:*", "&", `mod:"Co/Product01/..."`, 24),
		Entry("screen fields of a union of packages", "field:*", "&", "(pkg:Co/Product00/screens | pkg:Co/Product02/Plan/screens)", 24),
		Entry("rules of a module, the scope first", `mod:"Co/Product03"`, "&", "kind:test.rule", 6),
		Entry("callees of rules into a module", "kind:test.rule", ">", "mod:Co/Product01", 5),
	)

	It("keeps a right operand that is not a scope a set of symbols", func(ctx SpecContext) {
		result := runQuery(ctx, pipeline, "Plan:SchemeNumber < kind:test.rule:Rule002", all)
		Expect(result.Total).To(Equal(len(smallCorpus.rootKeys())))
		for _, match := range result.Matches {
			Expect(match.Identifier.Method).To(Equal("Rule002"))
		}
	})
})

// rootKeys are the keys of the corpus's product and plan roots.
func (shape corpusShape) rootKeys() []string {
	var keys []string
	for product := range shape.products {
		keys = append(keys, corpusProduct(product), corpusProduct(product)+"/Plan")
	}
	return keys
}
