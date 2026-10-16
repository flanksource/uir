package indexer

import (
	"path/filepath"

	rpchttp "github.com/flanksource/clicky/rpc/http"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("versioned dependency closure", func() {
	It("checks each version of a chain out once, and an unchanged closure with an unregistered dependency not at all", func(ctx SpecContext) {
		const thirdParty = "example.org/thirdparty"
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		base := canonicalTempDir()
		proxy := filepath.Join(base, "proxy")
		useModuleProxy(proxy)
		writeModuleProxy(proxy, thirdParty, "v1.0.0", "module "+thirdParty+"\n\ngo 1.26\n")
		chain := []struct{ name, requirement string }{
			{"gamma", thirdParty}, {"beta", "example.org/gamma"}, {"alpha", "example.org/beta"},
		}
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		for _, module := range chain {
			checkout := filepath.Join(base, module.name)
			writeTaggedModule(ctx, proxy, checkout, "example.org/"+module.name, "\nrequire "+module.requirement+" v1.0.0\n")
			writeFile(filepath.Join(checkout, "go.mod"), "module example.org/"+module.name+"\n\ngo 1.26\n")
			commitPaths(ctx, checkout, "test: head without requirements", "go.mod")
			indexOnce(ctx, engine, checkout)
		}
		root := filepath.Join(base, "root")
		writeVersionedConsumer(ctx, root, "root", "example.org/alpha")

		timed, timings := rpchttp.WithTimings(ctx)
		first := indexOnce(timed, engine, root)
		checkouts, _ := timings.Counter("checkout", "calls")
		Expect(checkouts).To(Equal(int64(len(chain))), "alpha, beta and gamma at v1.0.0 are each checked out once")
		target := first.SnapshotID
		for _, module := range []string{"example.org/alpha", "example.org/beta", "example.org/gamma"} {
			target = dependencyTarget(ctx, engine, target, module).String()
		}
		Expect(loadSnapshot(database, target).ModuleVersion).To(Equal("v1.0.0"))

		timed, timings = rpchttp.WithTimings(ctx)
		again := indexOnce(timed, engine, root)
		checkouts, _ = timings.Counter("checkout", "calls")
		Expect(checkouts).To(BeZero(), "gamma's edge to the unregistered third-party module does not make the stored closure stale")
		Expect(again).To(And(HaveField("Unchanged", true), HaveField("SnapshotID", first.SnapshotID)))
		Expect(countRows(database, &storage.ModuleSnapshot{}, "module_version = ?", "v1.0.0")).To(Equal(int64(len(chain))))
	})

	It("does not check out again a version whose stored snapshot is excluded because no file builds", func(ctx SpecContext) {
		const excluded = "example.org/excluded"
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		base := canonicalTempDir()
		proxy := filepath.Join(base, "proxy")
		useModuleProxy(proxy)
		checkout := filepath.Join(base, "excluded")
		writeTaggedModule(ctx, proxy, checkout, excluded, "")
		writeFile(filepath.Join(checkout, "source.go"), "//go:build never\n\npackage source\n\nfunc Value() int { return 1 }\n")
		commitPaths(ctx, checkout, "test: no file builds", "source.go")
		gitOutput(ctx, checkout, "tag", "-f", "v1.0.0")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		indexOnce(ctx, engine, checkout)
		root := filepath.Join(base, "root")
		writeVersionedConsumer(ctx, root, "root", excluded)

		first := indexOnce(ctx, engine, root)
		target := loadSnapshot(database, dependencyTarget(ctx, engine, first.SnapshotID, excluded).String())
		Expect([]any{target.ModuleVersion, target.Coverage}).To(Equal([]any{"v1.0.0", storage.CoverageExcluded}))

		timed, timings := rpchttp.WithTimings(ctx)
		indexOnce(timed, engine, root)
		checkouts, _ := timings.Counter("checkout", "calls")
		Expect(checkouts).To(BeZero(), "an excluded snapshot is as decided for its commit as an indexed one")
	})

	It("does not check out again a closure whose version failed module selection at its commit", func(ctx SpecContext) {
		const unsummed, thirdParty = "example.org/unsummed", "example.org/thirdparty"
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		base := canonicalTempDir()
		proxy := filepath.Join(base, "proxy")
		useModuleProxy(proxy)
		writeModuleProxy(proxy, thirdParty, "v1.0.0", "module "+thirdParty+"\n\ngo 1.26\n")
		checkout := filepath.Join(base, "unsummed")
		writeTaggedModule(ctx, proxy, checkout, unsummed, "")
		writeFile(filepath.Join(checkout, "go.mod"), "module "+unsummed+"\n\ngo 1.26\n\nrequire "+thirdParty+" v1.0.0\n")
		writeFile(filepath.Join(checkout, "source.go"), "//go:build never\n\npackage source\n\nfunc Value() int { return 1 }\n")
		commitPaths(ctx, checkout, "test: a requirement without a go.sum entry", "go.mod", "source.go")
		gitOutput(ctx, checkout, "tag", "-f", "v1.0.0")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		indexOnce(ctx, engine, checkout)
		root := filepath.Join(base, "root")
		writeVersionedConsumer(ctx, root, "root", unsummed)

		first := indexOnce(ctx, engine, root)
		target := dependencyTarget(ctx, engine, first.SnapshotID, unsummed)
		edges, err := storage.ModuleDependencies(ctx, database, target)
		Expect(err).ToNot(HaveOccurred())
		Expect(edges.Items).To(ConsistOf(And(
			HaveField("ModulePath", thirdParty), HaveField("SelectedVersion", ""), HaveField("TargetSnapshotID", BeNil()),
			HaveField("UnresolvedReason", ContainSubstring("missing go.sum entry")),
		)), "module selection over the committed go.mod fails, so the edge selects no version")

		timed, timings := rpchttp.WithTimings(ctx)
		again := indexOnce(timed, engine, root)
		checkouts, _ := timings.Counter("checkout", "calls")
		Expect(checkouts).To(BeZero(), "indexing the same commit again cannot give an edge module selection left without a version a target")
		Expect(again).To(And(HaveField("Unchanged", true), HaveField("SnapshotID", first.SnapshotID)))
	})
})
