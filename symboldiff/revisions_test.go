package symboldiff

import (
	"path/filepath"

	"github.com/flanksource/uir/indexer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("revision resolution", func() {
	It("reads every git diff range form with git's meaning", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		repo := newRepository()
		repo.write(map[string]*string{"go.mod": text("module " + shopRoot + "\n\ngo 1.26\n"), "cart.go": text(cartBefore)})
		base := repo.commit("base")
		repo.write(map[string]*string{"cart.go": text(cartAfter)})
		mainline := repo.commit("main change")
		repo.git("checkout", "--quiet", "-b", "feature", base)
		repo.write(map[string]*string{"checkout.go": checkoutSource("")})
		feature := repo.commit("feature change")
		repo.git("checkout", "--quiet", "main")
		repo.git("merge", "--no-ff", "--quiet", "--message", "merge feature", "feature")
		merge := repo.git("rev-parse", "HEAD")
		repo.index(ctx, database)
		remote := filepath.Join(GinkgoT().TempDir(), "origin.git")
		repo.git("init", "--quiet", "--bare", remote)
		repo.git("remote", "add", "origin", remote)
		repo.git("push", "--quiet", "origin", feature+":refs/pull/7/head")

		cases := map[string][]string{
			"A..B": {base + ".." + mainline}, "A..": {"HEAD~1.."}, "..B": {".." + mainline}, "A...B": {mainline + "...feature"},
			"A^!": {mainline + "^!"}, "A^-": {"HEAD^-"}, "A^-2": {"HEAD^-2"}, "A B": {base, "main"}, "pr:N..B": {"pr:7..main"},
			"pr:N B": {"pr:7", "main"}, "A": {"main~1"},
		}
		resolved := map[string]Resolution{}
		for name, revisions := range cases {
			resolution, err := ResolveRevisions(ctx, database, Options{RootKey: shopRoot, Revisions: revisions})
			Expect(err).ToNot(HaveOccurred(), name)
			resolved[name] = resolution
		}
		Expect(resolved).To(Equal(map[string]Resolution{
			"A..B": {From: base, To: mainline}, "A..": {From: mainline, To: merge}, "..B": {From: merge, To: mainline},
			"A...B": {From: base, To: feature}, "A^!": {From: base, To: mainline}, "A^-": {From: mainline, To: merge},
			"A^-2": {From: feature, To: merge}, "A B": {From: base, To: merge}, "pr:N..B": {From: feature, To: merge},
			"pr:N B": {From: feature, To: merge}, "A": {From: mainline, Worktree: true, Checkout: repo.path},
		}))

		for _, revisions := range [][]string{{mainline + "^@"}, {"HEAD^@"}, {"^HEAD"}, {"nosuch"}, {"--output=x"}, {base, mainline, merge}, {}, {base, " "}} {
			_, err := ResolveRevisions(ctx, database, Options{RootKey: shopRoot, Revisions: revisions})
			Expect(err).To(HaveOccurred(), "%q", revisions)
		}
		_, err := ResolveRevisions(ctx, database, Options{RootKey: shopRoot, Revisions: []string{"HEAD^@"}})
		Expect(err).To(MatchError(And(ContainSubstring(`"HEAD^@"`), ContainSubstring(mainline), ContainSubstring(feature))))
		_, err = Diff(ctx, database, Options{RootKey: shopRoot, Revisions: []string{"HEAD"}, Visibility: VisibilityAll})
		Expect(err).To(MatchError(ContainSubstring("worktree")))
	})

	It("takes an external root's revisions literally, as <from>..<to> or two revisions", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		id := schemaColumn{"ID", "ID INT NOT NULL"}
		for _, revision := range []string{"schema-v1", "schema-v2"} {
			_, err := indexer.Publish(ctx, database, schemaPublication(revision, ordersV1, []schemaColumn{id}))
			Expect(err).ToNot(HaveOccurred())
		}
		var resolved []Resolution
		for _, revisions := range [][]string{{"schema-v1..schema-v2"}, {"schema-v1", "schema-v2"}} {
			resolution, err := ResolveRevisions(ctx, database, Options{RootKey: schemaRoot, Revisions: revisions})
			Expect(err).ToNot(HaveOccurred())
			resolved = append(resolved, resolution)
		}
		Expect(resolved).To(Equal([]Resolution{{From: "schema-v1", To: "schema-v2"}, {From: "schema-v1", To: "schema-v2"}}))
		for _, spec := range []string{"schema-v1...schema-v2", "schema-v1", "schema-v1.."} {
			_, err := ResolveRevisions(ctx, database, Options{RootKey: schemaRoot, Revisions: []string{spec}})
			Expect(err).To(MatchError(ContainSubstring("<from>..<to>")), spec)
		}
	})
})
