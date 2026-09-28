package query_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// shopShout is an app file that only references a standard library function, so strings.ToUpper is
// active in the root through a reference posting while no snapshot of the root defines it.
const shopShout = "package app\n\nimport \"strings\"\n\nfunc Shout(name string) string { return strings.ToUpper(name) }\n"

var _ = Describe("compact queries over defined-symbol sets", func() {
	DescribeTable("resolve a symbol only in the snapshots that define or reference it", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		primary, _ := shopWorkspace()
		Expect(os.WriteFile(filepath.Join(primary, "app", "shout.go"), []byte(shopShout), 0o644)).To(Succeed())
		first := indexCheckout(ctx, database, primary)
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		current := query.ModuleScopeOptions{RootKey: shopModule, Location: primary}
		historical := query.ModuleScopeOptions{SnapshotID: first.SnapshotID}

		By("re-extracting app.go several times, so the root holds more Save postings than active documents")
		for edit := range 3 {
			app := shopRun + fmt.Sprintf("\nfunc Edit%d(s *store.Store) error { return s.Save(\"e\") }\n", edit)
			Expect(os.WriteFile(filepath.Join(primary, "app", "app.go"), []byte(app), 0o644)).To(Succeed())
			indexCheckout(ctx, database, primary)
		}
		Expect(runQuery(ctx, pipeline, "store.Store.Save <", current).Total).To(Equal(3), "Run's call, Persist's dispatch, and Edit2")
		Expect(runQuery(ctx, pipeline, "store.Store.Save <", historical).Total).To(Equal(3), "Run's call, Persist's dispatch, and Direct")

		By("dropping Direct: the later snapshot's defined set deletes it, the first still defines it")
		_, err = pipeline.RunModules(ctx, "app.Direct =", current)
		var unresolved *query.UnresolvedSymbolError
		Expect(err).To(BeAssignableToTypeOf(unresolved))
		Expect(runQuery(ctx, pipeline, "app.Direct =", historical).Matches).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "func:Direct", current).Symbols).To(BeEmpty())
		Expect(runQuery(ctx, pipeline, "func:Direct", historical).Symbols).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "func:Edit*", current).Symbols).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "pkg:example.org/shop:app", current).Symbols).To(HaveLen(3), "Run, Edit2, and Shout")

		By("resolving a standard library function the root only calls")
		shout := runQuery(ctx, pipeline, "strings.ToUpper <", current)
		Expect(shout.Total).To(Equal(1))
		Expect(shout.Matches[0].EnclosingKey).To(ContainSubstring("Shout"))
		Expect(runQuery(ctx, pipeline, "func:ToUpper", current).Symbols).To(BeEmpty(), "a selector keeps symbols defined in their own module's root")
		suggested, err := pipeline.SuggestSymbols(ctx, "strings.ToUp", current)
		Expect(err).ToNot(HaveOccurred())
		Expect(suggested.Items).To(HaveLen(1))
		Expect(suggested.Items[0].QueryName).To(Equal("strings.ToUpper"))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
})
