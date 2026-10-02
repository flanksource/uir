package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/tools/go/packages"
)

const (
	historyModule   = "example.org/history"
	historyManifest = "module example.org/history\n\ngo 1.26\n"
)

// historyCheckout commits two versions of example.org/history in a repository that ignores nothing,
// registers the checkout, and returns it with its first commit.
func historyCheckout(ctx context.Context, engine *Indexer) (string, string) {
	GinkgoHelper()
	checkout := canonicalTempDir()
	gitOutput(ctx, checkout, "init")
	writeFile(filepath.Join(checkout, "go.mod"), historyManifest)
	writeFile(filepath.Join(checkout, "history.go"), "package history\n\nfunc Version() int { return 1 }\n")
	first := commitPaths(ctx, checkout, "test: first", "go.mod", "history.go")
	writeFile(filepath.Join(checkout, "history.go"), "package history\n\nfunc Version() int { return 2 }\n")
	commitPaths(ctx, checkout, "test: second", "history.go")
	indexOnce(ctx, engine, checkout)
	return checkout, first
}

// failLoads makes every go/packages load of the engine fail with message.
func failLoads(engine *Indexer, message string) {
	engine.loadPackages = func(*packages.Config, ...string) ([]*packages.Package, error) {
		return nil, errors.New(message)
	}
}

const offlineLookup = "go: example.org/missing@v1.0.0: module lookup disabled by GOPROXY=off"

var _ = Describe("historical revisions", func() {
	It("removes its worktree from a repository that does not ignore .tmp", func(ctx SpecContext) {
		GinkgoT().Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
		GinkgoT().Setenv("GIT_CONFIG_NOSYSTEM", "1")
		GinkgoT().Setenv("XDG_CONFIG_HOME", GinkgoT().TempDir())
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		checkout, first := historyCheckout(ctx, engine)

		indexed, err := engine.IndexRevision(ctx, RevisionOptions{RootKey: historyModule, Checkout: checkout, Commit: first, Reason: storage.ReasonHistorical})
		Expect(err).ToNot(HaveOccurred())

		Expect(loadSnapshot(database, indexed.SnapshotID).GitCommit).To(Equal(first))
		worktrees := strings.Count(gitOutput(ctx, checkout, "worktree", "list", "--porcelain"), "worktree ")
		Expect(worktrees).To(Equal(1), "only the checkout itself remains a worktree")
		entries, err := os.ReadDir(filepath.Join(checkout, ".git", "worktrees"))
		if !errors.Is(err, os.ErrNotExist) {
			Expect(err).ToNot(HaveOccurred())
			Expect(entries).To(BeEmpty())
		}
		Expect(filepath.Join(checkout, ".tmp")).ToNot(BeAnExistingFile())
		Expect(gitOutput(ctx, checkout, "status", "--porcelain")).To(BeEmpty())
	})

	It("does not reuse a syntax-coverage snapshot, but reuses an identical re-extraction", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		checkout, first := historyCheckout(ctx, engine)
		options := RevisionOptions{RootKey: historyModule, Checkout: checkout, Commit: first, Reason: storage.ReasonHistorical}
		failLoads(engine, offlineLookup)

		degraded, err := engine.IndexRevision(ctx, options)
		Expect(err).ToNot(HaveOccurred())
		Expect(loadSnapshot(database, degraded.SnapshotID).Coverage).To(Equal(storage.CoverageSyntax))
		again, err := engine.IndexRevision(ctx, options)
		Expect(err).ToNot(HaveOccurred())
		Expect(again.SnapshotID).To(Equal(degraded.SnapshotID), "the same degraded extraction is not published twice")

		engine.loadPackages = packages.Load
		recovered, err := engine.IndexRevision(ctx, options)
		Expect(err).ToNot(HaveOccurred())
		Expect(recovered.SnapshotID).ToNot(Equal(degraded.SnapshotID))
		Expect(loadSnapshot(database, recovered.SnapshotID).Coverage).To(Equal(storage.CoverageIndexed))
	})

	DescribeTable("falls back to syntax only for a load failure the historical source itself causes",
		func(ctx SpecContext, message string, cancel bool, fallsBack bool) {
			module := canonicalTempDir()
			writeFile(filepath.Join(module, "go.mod"), historyManifest)
			writeFile(filepath.Join(module, "history.go"), "package history\n\nfunc Version() int { return 1 }\n")
			roots, err := discoverModules(ctx, module, false)
			Expect(err).ToNot(HaveOccurred())
			root := roots[0]
			root.Historical = true
			loadCtx, stop := context.WithCancel(ctx)
			defer stop()
			loader := func(*packages.Config, ...string) ([]*packages.Package, error) {
				if cancel {
					stop()
					return nil, loadCtx.Err()
				}
				return nil, errors.New(message)
			}

			extraction, err := extractModule(loadCtx, loader, root, false)

			if !fallsBack {
				Expect(err).To(HaveOccurred())
				return
			}
			Expect(err).ToNot(HaveOccurred())
			Expect(extraction.coverage).To(Equal(storage.CoverageSyntax))
		},
		Entry("a cancelled load fails", "", true, false),
		Entry("an unreachable proxy fails", `go: example.org/missing@v1.0.0: Get "https://proxy.golang.org/example.org/missing/@v/v1.0.0.mod": dial tcp: lookup proxy.golang.org: no such host`, false, false),
		Entry("a proxy server error fails", "go: example.org/missing@v1.0.0: reading https://proxy.golang.org/example.org/missing/@v/v1.0.0.mod: 502 Bad Gateway", false, false),
		Entry("a module lookup the environment disables falls back", offlineLookup, false, true),
		Entry("a version the proxy does not have falls back", "go: example.org/missing@v1.0.0: reading https://proxy.golang.org/example.org/missing/@v/v1.0.0.mod: 404 Not Found", false, true),
		Entry("a missing go.sum entry falls back", "go: example.org/missing@v1.0.0: missing go.sum entry for go.mod file", false, true),
	)
})

var _ = Describe("versioned dependency snapshots", func() {
	It("re-indexes a moved version tag at its new commit", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		checkout := canonicalTempDir()
		gitOutput(ctx, checkout, "init")
		writeFile(filepath.Join(checkout, "go.mod"), "module example.org/pricing\n\ngo 1.26\n")
		writeFile(filepath.Join(checkout, "pricing.go"), "package pricing\n\nfunc Price() int { return 1 }\n")
		tagged := commitPaths(ctx, checkout, "test: tagged module", "go.mod", "pricing.go")
		gitOutput(ctx, checkout, "tag", "v1.2.3")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		indexOnce(ctx, engine, checkout)
		version, err := engine.versionedSnapshot(ctx, "example.org/pricing", "v1.2.3", false)
		Expect(err).ToNot(HaveOccurred())
		Expect(version.GitCommit).To(Equal(tagged))

		writeFile(filepath.Join(checkout, "pricing.go"), "package pricing\n\nfunc Price() int { return 2 }\n")
		moved := commitPaths(ctx, checkout, "test: move the tag", "pricing.go")
		gitOutput(ctx, checkout, "tag", "-f", "v1.2.3")
		reindexed, err := engine.versionedSnapshot(ctx, "example.org/pricing", "v1.2.3", false)

		Expect(err).ToNot(HaveOccurred())
		Expect(reindexed.GitCommit).To(Equal(moved))
		Expect(reindexed.ID).ToNot(Equal(version.ID))
	})
})
