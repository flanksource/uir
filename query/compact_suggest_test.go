package query_test

import (
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("compact symbol suggestions", func() {
	It("completes registered modules and relative packages in the selected snapshot", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		primary, _ := shopWorkspace()
		indexCheckout(ctx, database, primary)
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		scope := query.ModuleScopeOptions{RootKey: shopModule, Location: primary}
		modules, err := pipeline.SuggestSelectors(ctx, "mod:example.org/sh", scope)
		Expect(err).ToNot(HaveOccurred())
		Expect(modules.Items).To(Equal([]string{"mod:example.org/shop"}))
		packages, err := pipeline.SuggestSelectors(ctx, "pkg:example.org/shop:st", scope)
		Expect(err).ToNot(HaveOccurred())
		Expect(packages.Items).To(Equal([]string{"pkg:example.org/shop:store"}))
		functions, err := pipeline.SuggestSelectors(ctx, "func:Store.S", scope)
		Expect(err).ToNot(HaveOccurred())
		Expect(functions.Items).To(Equal([]string{"func:example.org/shop/store.Store.Save"}))
		kinds, err := pipeline.SuggestSelectors(ctx, "kind:f", scope)
		Expect(err).ToNot(HaveOccurred())
		Expect(kinds.Items).To(Equal([]string{"kind:field", "kind:foreign_key", "kind:func"}), "kind: completes registered kind names")
		methods, err := pipeline.SuggestSelectors(ctx, "kind:method:Store.S", scope)
		Expect(err).ToNot(HaveOccurred())
		Expect(methods.Items).To(Equal([]string{"kind:method:example.org/shop/store.Store.Save"}))
		_, err = pipeline.SuggestSelectors(ctx, "kind:acme.rule:R", scope)
		Expect(err).To(MatchError(ContainSubstring(`symbol kind "acme.rule" is not registered`)))
	})
	It("completes quoted selector values and Entity:Field references", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		checkout := entitiesCheckout()
		indexCheckout(ctx, database, checkout)
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		scope := query.ModuleScopeOptions{RootKey: entitiesModule, Location: checkout}
		suggest := func(prefix string) []string {
			GinkgoHelper()
			found, err := pipeline.SuggestSelectors(ctx, prefix, scope)
			Expect(err).ToNot(HaveOccurred(), prefix)
			return found.Items
		}

		Expect(suggest(`mod:"example.org/ent`)).To(Equal([]string{"mod:" + entitiesModule}))
		Expect(suggest(`func:"Tou`)).To(Equal([]string{"func:" + entitiesModule + "/model.Touch"}))
		Expect(suggest("Plan:")).To(Equal([]string{"Plan:PlanField1", "Plan:Status"}))
		Expect(suggest("Plan:Pl")).To(Equal([]string{"Plan:PlanField1"}))
		Expect(suggest(`Policy:"Pl`)).To(Equal([]string{"Policy:PlanField1"}))
		Expect(suggest("Touch:")).To(BeEmpty(), "a function owns no fields")
		_, err = pipeline.SuggestSelectors(ctx, "mod:example org", scope)
		Expect(err).To(MatchError(ContainSubstring("invalid selector prefix")), "whitespace needs quotes")
	})
	It("returns active canonical names for a partial Go spelling in the selected primary head", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		primary, branch := shopWorkspace()
		indexCheckout(ctx, database, primary)
		indexCheckout(ctx, database, branch)
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		found, err := pipeline.SuggestSymbols(ctx, "store.Store.S", query.ModuleScopeOptions{RootKey: shopModule, Location: primary, Limit: 10})
		Expect(err).ToNot(HaveOccurred())
		Expect(found.Items).To(HaveLen(1))
		Expect(found.Items[0].QueryName).To(Equal("example.org/shop/store.Store.Save"))
		missing, err := pipeline.SuggestSymbols(ctx, "store.Missing", query.ModuleScopeOptions{RootKey: shopModule, Location: primary})
		Expect(err).ToNot(HaveOccurred())
		Expect(missing.Items).To(Equal([]query.ModuleSymbol{}))
	})
	DescribeTable("validates a partial symbol spelling, which is literal text",
		func(ctx SpecContext, prefix string, valid bool) {
			database := openQueryDatabase(ctx, "sqlite")
			primary, _ := shopWorkspace()
			indexCheckout(ctx, database, primary)
			pipeline, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())
			_, err = pipeline.SuggestSymbols(ctx, prefix, query.ModuleScopeOptions{RootKey: shopModule, Location: primary})
			if valid {
				Expect(err).ToNot(HaveOccurred())
			} else {
				Expect(err).To(MatchError(ContainSubstring("invalid partial symbol")))
			}
		},
		Entry("a name with a space", "Premium Calc (Ann", true),
		Entry("a hyphenated name", "CopyBook-Cyc", true),
		Entry("a quote in a name", `Say "Hi`, true),
		Entry("a leading space", " store", false),
		Entry("a wildcard", "store.S*", false),
		Entry("an empty owner", "store..Save", false),
		Entry("a control character", "store\x00", false),
	)
})
