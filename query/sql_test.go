package query_test

import (
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL navigation", func() {
	DescribeTable("detects the leading unquoted SQL keyword",
		func(input string, language query.Language) {
			actual, err := query.DetectLanguage(input, query.LanguageAuto)
			Expect(err).NotTo(HaveOccurred())
			Expect(actual).To(Equal(language))
		},
		Entry("SQL", " \nselect * FROM func:Run", query.LanguageSQL),
		Entry("malformed SQL", "SELECT", query.LanguageSQL),
		Entry("quoted symbol", `func:"SELECT"`, query.LanguageCompact),
		Entry("name prefix", "SELECTED", query.LanguageCompact),
		Entry("number suffix", "SELECT1", query.LanguageCompact),
	)

	It("uses the recorded read and write roles and SQL selector completion", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		indexCheckout(ctx, database, entitiesCheckout())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{RootKey: entitiesModule}
		reads := runQuery(ctx, pipeline, "SELECT role FROM reads(field:Policy.PlanField1)", scope)
		writes := runQuery(ctx, pipeline, "SELECT role FROM writes(field:Plan.PlanField1)", scope)
		Expect(reads.Rows).To(Equal([]map[string]any{{"role": "reference"}}))
		Expect(writes.Rows).To(Equal([]map[string]any{{"role": "write"}}))
	})

	It("projects caller symbols while preserving call witnesses and pre-limit totals", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		primary, _ := shopWorkspace()
		indexCheckout(ctx, database, primary)
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{RootKey: shopModule, Location: primary}
		result := runQuery(ctx, pipeline, "SELECT symbol.name AS name, path, line FROM callers(method:Store.Save) WHERE symbol IN func:* ORDER BY name DESC LIMIT 1", scope)
		Expect(result.Total).To(Equal(3))
		Expect(result.Rows).To(HaveLen(1))
		Expect(result.Columns).To(Equal([]query.QueryColumn{{Name: "name", Field: "symbol.name", Type: "string"}, {Name: "path", Field: "path", Type: "string"}, {Name: "line", Field: "line", Type: "number"}}))
		Expect(result.Rows[0]["name"]).To(Equal("Run"))
		Expect(result.Matches).To(HaveLen(1))
		Expect(result.Matches[0].Role).To(Equal("call"))
	})

	It("filters only after traversal and nests canonical symbol projections", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		pipeline, scope, _ := flowPipeline(ctx, database)
		result := runQuery(ctx, pipeline, "SELECT symbol.name, depth FROM traverse(func:Top, direction => 'outgoing', depth => 3) WHERE symbol.name = 'Handle'", scope)
		Expect(result.Rows).To(Equal([]map[string]any{{"symbol.name": "Handle", "depth": 2}}))
		result = runQuery(ctx, pipeline, "SELECT symbol.name FROM callers(callees(func:Entry)) ORDER BY symbol.name", scope)
		Expect(result.Rows).NotTo(BeEmpty())
	})

	It("keeps call sites and depth on multi-hop caller and callee results", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		pipeline, scope, _ := flowPipeline(ctx, database)
		callees := runQuery(ctx, pipeline, "SELECT symbol.name, role, line, depth FROM callees(func:Top, depth => 2) ORDER BY depth", scope)
		Expect(callees.Rows).To(Equal([]map[string]any{
			{"symbol.name": "Entry", "role": "call", "line": flowLine("return Entry(channel)"), "depth": 1},
			{"symbol.name": "Handle", "role": "call", "line": flowLine("return Handle(Order{}, channel)"), "depth": 2},
		}))
		callers := runQuery(ctx, pipeline, "SELECT symbol.name, role, line, depth FROM callers(func:Handle, depth => 2) ORDER BY depth", scope)
		Expect(callers.Rows).To(Equal([]map[string]any{
			{"symbol.name": "Entry", "role": "call", "line": flowLine("return Handle(Order{}, channel)"), "depth": 1},
			{"symbol.name": "Top", "role": "call", "line": flowLine("return Entry(channel)"), "depth": 2},
		}))
	})

	It("bounds cycles and retains known interface dispatch in traversal", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		pipeline, scope, _ := flowPipeline(ctx, database)
		cycle := runQuery(ctx, pipeline, "SELECT symbol.name, depth FROM traverse(func:ping, depth => 8)", scope)
		Expect(cycle.Rows).To(Equal([]map[string]any{{"symbol.name": "pong", "depth": 1}}))
		dispatch := runQuery(ctx, pipeline, "SELECT symbol.owner, dispatch FROM callees(func:Process, depth => 2) WHERE dispatch = TRUE ORDER BY symbol.owner", scope)
		Expect(dispatch.Rows).To(Equal([]map[string]any{{"symbol.owner": "Email", "dispatch": true}, {"symbol.owner": "SMS", "dispatch": true}}))
	})

	It("rejects invalid SQL, functions, fields, types, and terminal clauses without fallback", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		for _, input := range []string{"SELECT", "SELECT * FROM missing(func:Run)", "SELECT typo FROM func:Run", "SELECT * FROM func:Run WHERE depth = 'two'", "SELECT * FROM callers(func:Run, depth => 9)", "SELECT * FROM graph(func:Run) LIMIT 1", "SELECT * FROM callers(graph(func:Run))"} {
			_, err := pipeline.RunModules(ctx, input, query.ModuleScopeOptions{})
			Expect(err).To(HaveOccurred(), input)
		}
	})
})
