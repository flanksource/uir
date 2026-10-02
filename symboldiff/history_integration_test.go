package symboldiff

import (
	"path/filepath"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Git history for a registered root", func() {
	It("compares a selected commit with its first parent and indexes the missing parent on demand", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		repo := newRepository()
		repo.write(map[string]*string{"go.mod": text("module " + shopRoot + "\n\ngo 1.26\n"), "cart.go": text(cartBefore)})
		from := repo.commit("Add cart")
		repo.write(map[string]*string{"cart.go": text(cartAfter)})
		to := repo.commit("Change cart")
		head := repo.index(ctx, database)

		result, err := DiffCommit(ctx, database, CommitOptions{RootKey: shopRoot, Commit: to, Visibility: VisibilityAll, Stat: true, TaskContext: ctx})
		Expect(err).ToNot(HaveOccurred())
		Expect([]string{result.From.Commit, result.To.Commit}).To(Equal([]string{from, to}))
		Expect(result.file("cart.go").names()).To(ContainElement("signature Cart"))
		locations, err := storage.ModuleLocations(ctx, database, shopRoot)
		Expect(err).ToNot(HaveOccurred())
		Expect(locations[0].HeadSnapshotID.String()).To(Equal(head))
		Expect(repo.git("rev-parse", "HEAD")).To(Equal(to))
		Expect(repo.git("status", "--porcelain")).To(BeEmpty())
	})

	It("rejects an initial commit because it has no parent", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		repo := newRepository()
		repo.write(map[string]*string{"go.mod": text("module " + shopRoot + "\n\ngo 1.26\n"), "cart.go": text(cartBefore)})
		first := repo.commit("Add cart")
		repo.index(ctx, database)
		_, err := DiffCommit(ctx, database, CommitOptions{RootKey: shopRoot, Commit: first, Visibility: VisibilityAll, TaskContext: ctx})
		Expect(err).To(MatchError(ContainSubstring("has no parent to compare")))
	})

	It("lists a pull request ref and compares its fetched head", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		repo := newRepository()
		repo.write(map[string]*string{"go.mod": text("module " + shopRoot + "\n\ngo 1.26\n"), "cart.go": text(cartBefore)})
		prCommit := repo.commit("Pull request head")
		repo.write(map[string]*string{"cart.go": text(cartAfter)})
		mainCommit := repo.commit("Main branch head")
		repo.index(ctx, database)
		remote := filepath.Join(GinkgoT().TempDir(), "origin.git")
		repo.git("init", "--bare", remote)
		repo.git("remote", "add", "origin", remote)
		repo.git("push", "origin", mainCommit+":refs/heads/main", prCommit+":refs/pull/12/head")

		history, err := ListHistory(ctx, database, shopRoot, repo.path, 20)
		Expect(err).ToNot(HaveOccurred())
		Expect(history.PullRequests).To(ContainElement(PullRequestRef{Number: 12, Commit: prCommit}))
		result, err := Diff(ctx, database, Options{RootKey: shopRoot, From: "main", To: "pr:12", Visibility: VisibilityAll, AutoIndex: true, TaskContext: ctx})
		Expect(err).ToNot(HaveOccurred())
		Expect([]string{result.From.Commit, result.To.Commit}).To(Equal([]string{mainCommit, prCommit}))
		Expect(result.file("cart.go").names()).To(ContainElement("signature Cart"))
	})

	It("lists branch and commit choices with their parent commits", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		repo := newRepository()
		repo.write(map[string]*string{"go.mod": text("module " + shopRoot + "\n\ngo 1.26\n"), "cart.go": text(cartBefore)})
		from := repo.commit("Add cart")
		repo.write(map[string]*string{"cart.go": text(cartAfter)})
		to := repo.commit("Change cart")
		repo.index(ctx, database)

		history, err := ListHistory(ctx, database, shopRoot, repo.path, 20)
		Expect(err).ToNot(HaveOccurred())
		Expect(history.Branches).To(ContainElement(GitRef{Name: "main", Commit: to}))
		Expect(history.Commits).To(HaveLen(2))
		Expect(history.Commits[0]).To(And(HaveField("Commit", to), HaveField("Parents", []string{from}), HaveField("Subject", "Change cart")))
		Expect(history.Commits[1].Commit).To(Equal(from))
	})
})
