package indexer

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("historical revision scratch placement", func() {
	It("cleans a caller-selected scratch directory and preserves the source checkout", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		checkout, first := historyCheckout(ctx, engine)
		scratch := filepath.Join(GinkgoT().TempDir(), "worktrees")
		indexed, err := engine.IndexRevision(ctx, RevisionOptions{RootKey: historyModule, Checkout: checkout, Commit: first, Reason: storage.ReasonHistorical, ScratchDirectory: scratch})
		Expect(err).ToNot(HaveOccurred())
		Expect(loadSnapshot(database, indexed.SnapshotID).GitCommit).To(Equal(first))
		entries, err := os.ReadDir(scratch)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty())
		Expect(strings.Count(gitOutput(ctx, checkout, "worktree", "list", "--porcelain"), "worktree ")).To(Equal(1))
		Expect(gitOutput(ctx, checkout, "status", "--porcelain")).To(BeEmpty())
	})

	It("rejects a relative scratch directory before creating a worktree", func(ctx SpecContext) {
		_, err := revisionWorktree(ctx, revisionWorktreeOptions{Directory: "relative"})
		Expect(err).To(MatchError("historical scratch directory must be absolute"))
	})
})
