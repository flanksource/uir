package symboldiff

import (
	"fmt"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	shopRoot     = "example.org/shop"
	cartBefore   = "package shop\n\ntype Cart struct{ Items []string }\n"
	cartDirty    = cartBefore + "\nfunc Empty() bool { return true }\n"
	cartAfter    = "package shop\n\ntype Cart struct {\n\tItems []string\n\tOwner string\n}\n"
	checkoutFunc = "package shop\n\nfunc Checkout(c *Cart) int { return len(c.Items)%s }\n"
)

func checkoutSource(suffix string) *string {
	return text(fmt.Sprintf(checkoutFunc, suffix))
}

var _ = Describe("commit-to-snapshot selection", func() {
	DescribeTable("rejects dirty-only, unindexed, mismatched, and empty selections and fails one file's lines on a hash mismatch",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDatabase(ctx, options())
			repo := newRepository()
			repo.write(map[string]*string{"go.mod": text("module " + shopRoot + "\n\ngo 1.26\n"), "cart.go": text(cartBefore), "checkout.go": checkoutSource("")})
			from := repo.commit("shop before")
			repo.write(map[string]*string{"cart.go": text(cartDirty)})
			dirty := repo.index(ctx, database)
			repo.write(map[string]*string{"cart.go": text(cartAfter), "checkout.go": checkoutSource(" + 0")})
			to := repo.commit("shop after")
			clean := repo.index(ctx, database)

			_, err := Diff(ctx, database, Options{RootKey: shopRoot, From: from, To: to, Visibility: VisibilityAll})
			Expect(err).To(MatchError(And(ContainSubstring("only dirty snapshots"), ContainSubstring(dirty), ContainSubstring("--snapshot-from"))))

			repo.write(map[string]*string{"checkout.go": checkoutSource(" + 1")})
			unindexed := repo.commit("shop unindexed")
			_, err = Diff(ctx, database, Options{RootKey: shopRoot, From: to, To: unindexed, Visibility: VisibilityAll})
			Expect(err).To(MatchError(And(ContainSubstring(unindexed), ContainSubstring("has no snapshot"), ContainSubstring("index"))))

			_, err = Diff(ctx, database, Options{RootKey: shopRoot, From: "", To: to, Visibility: VisibilityAll})
			Expect(err).To(MatchError(ContainSubstring("empty revision")))
			_, _, err = ParseRange(".." + to)
			Expect(err).To(MatchError(ContainSubstring("empty revision")))

			_, err = Diff(ctx, database, Options{RootKey: shopRoot, From: from, To: to, SnapshotFrom: clean, Visibility: VisibilityAll})
			Expect(err).To(MatchError(And(ContainSubstring(clean), ContainSubstring("records revision "+to), ContainSubstring("not "+from))))

			result, err := Diff(ctx, database, Options{RootKey: shopRoot, From: from, To: to, SnapshotFrom: dirty, Visibility: VisibilityAll, Stat: true})
			Expect(err).ToNot(HaveOccurred())
			Expect([]string{result.From.SnapshotID, result.From.WorktreeState}).To(Equal([]string{dirty, "dirty"}))
			cart := result.file("cart.go")
			Expect(cart.LinesError).To(And(ContainSubstring("cart.go at "+from), ContainSubstring("does not match")))
			Expect([]any{cart.Lines, cart.FileScope, cart.names()}).To(Equal([]any{(*LineCount)(nil), (*LineCount)(nil), []string{"signature Cart", "added Cart.Owner", "removed Empty"}}),
				"a hash mismatch fails the file's line counts but keeps its symbol rows")
			Expect(result.file("checkout.go").Lines).To(Equal(&LineCount{Added: 1, Removed: 1}))
			Expect(body(result)).To(ContainSubstring("  cart.go (modified)  lines unavailable: "))
		},
		Entry("SQLite", sqliteOptions),
		Entry("PostgreSQL", postgresOptions),
	)

	It("rejects snapshots indexed under different configuration hashes", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		repo := newRepository()
		repo.write(map[string]*string{"go.mod": text("module " + shopRoot + "\n\ngo 1.26\n"), "cart.go": text(cartBefore), "checkout.go": checkoutSource("")})
		GinkgoT().Setenv("CGO_ENABLED", "1")
		from := repo.commit("shop before")
		repo.index(ctx, database)
		repo.write(map[string]*string{"cart.go": text(cartAfter)})
		to := repo.commit("shop after")
		GinkgoT().Setenv("CGO_ENABLED", "0")
		repo.index(ctx, database)
		_, err := Diff(ctx, database, Options{RootKey: shopRoot, From: from, To: to, Visibility: VisibilityAll})
		Expect(err).To(MatchError(And(ContainSubstring("configuration hash"), ContainSubstring("not comparable"))))
	})

	It("rejects an unknown visibility and an unknown root", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		_, err := Diff(ctx, database, Options{RootKey: shopRoot, From: "a", To: "b", Visibility: "public"})
		Expect(err).To(MatchError(ContainSubstring(`visibility "public"`)))
		_, err = Diff(ctx, database, Options{RootKey: "example.org/missing", From: "a", To: "b", Visibility: VisibilityAll})
		Expect(err).To(MatchError(ContainSubstring(`root "example.org/missing" was not found`)))
	})
})
