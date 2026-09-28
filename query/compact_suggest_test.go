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
})
