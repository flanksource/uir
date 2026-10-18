package indexer

import (
	"context"
	"os"
	"path/filepath"

	rpchttp "github.com/flanksource/clicky/rpc/http"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/tools/go/packages"
)

const (
	absentShop    = "example.org/absentshop"
	absentPackage = absentShop + "/audit"
	// absentShopFiles are the Go files of the indexed commit: main.go and audit/audit.go.
	absentShopFiles = 2
	proxyFailure    = `example.org/rates@v1.0.0: Get "https://proxy.golang.org/example.org/rates/@v/v1.0.0.zip": dial tcp: lookup proxy.golang.org: no such host`
)

// writeAbsentImportCommit registers a shop checkout that requires example.org/rates v1.0.0 from a file
// proxy and returns the options of its commit whose audit package imports example.org/rates/missing,
// a package v1.0.0 does not contain, the way a commit imports an API only an unreleased sibling has.
// The checkout's head lies one commit later.
func writeAbsentImportCommit(ctx context.Context, engine *Indexer) RevisionOptions {
	GinkgoHelper()
	base := canonicalTempDir()
	proxy, shop := filepath.Join(base, "proxy"), filepath.Join(base, "shop")
	useModuleProxy(proxy)
	writeModuleProxy(proxy, "example.org/rates", "v1.0.0", "module example.org/rates\n\ngo 1.26\n")
	writeModule(shop, absentShop, "\nrequire example.org/rates v1.0.0\n",
		"package shop\n\nimport source \"example.org/rates\"\n\nfunc Total() int { return source.Value() }\n")
	downloadModules(ctx, shop)
	gitOutput(ctx, shop, "init")
	commitPaths(ctx, shop, "test: first", ".")
	Expect(os.MkdirAll(filepath.Join(shop, "audit"), 0o755)).To(Succeed())
	writeFile(filepath.Join(shop, "audit", "audit.go"), "package audit\n\nimport \"example.org/rates/missing\"\n\nfunc Check() int { return missing.Value() }\n")
	commit := commitPaths(ctx, shop, "test: import a package the pinned version lacks", "audit")
	writeFile(filepath.Join(shop, "note.go"), "package shop\n\nfunc Note() string { return \"note\" }\n")
	commitPaths(ctx, shop, "test: move the head on", "note.go")
	indexOnce(ctx, engine, shop)
	return RevisionOptions{RootKey: absentShop, Checkout: shop, Commit: commit, Reason: storage.ReasonHistorical}
}

// failAuditLoads counts loads and adds an unreachable-proxy list error to the audit package of each.
func failAuditLoads(engine *Indexer) *int {
	loads := 0
	engine.loadPackages = func(config *packages.Config, patterns ...string) ([]*packages.Package, error) {
		loads++
		roots, err := packages.Load(config, patterns...)
		for _, pkg := range roots {
			if pkg.PkgPath == absentPackage {
				pkg.Errors = append(pkg.Errors, packages.Error{Kind: packages.ListError, Msg: proxyFailure})
			}
		}
		return roots, err
	}
	return &loads
}

func reuseCounters(timings *rpchttp.Timings, names ...string) map[string]int64 {
	counters := map[string]int64{}
	for _, name := range names {
		counters[name], _ = timings.Counter("reuse-check", name)
	}
	return counters
}

var _ = Describe("deterministic degraded coverage", func() {
	DescribeTable("reuses an exact repeat of a commit whose import its pinned module version lacks without loading packages",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			revision := writeAbsentImportCommit(ctx, engine)
			loads := countLoads(engine)
			timed, timings := rpchttp.WithTimings(ctx)
			first, err := engine.IndexRevision(timed, revision)
			Expect(err).ToNot(HaveOccurred())
			Expect(reuseCounters(timings, "miss-none")).To(Equal(map[string]int64{"miss-none": 1}))
			Expect(loadSnapshot(database, first.SnapshotID)).To(And(
				HaveField("Coverage", storage.CoverageSyntax), HaveField("CoverageDeterministic", HaveValue(BeTrue()))))

			timed, timings = rpchttp.WithTimings(ctx)
			again, err := engine.IndexRevision(timed, revision)
			Expect(err).ToNot(HaveOccurred())
			Expect(again).To(Equal(ModuleResult{
				RootKey: absentShop, Location: revision.Checkout, SnapshotID: first.SnapshotID, Files: absentShopFiles, ReusedFiles: absentShopFiles, Unchanged: true,
			}))
			Expect(*loads).To(Equal(1), "a coverage every error of which the commit's pinned modules decide is proven without type-checking")
			Expect(reuseCounters(timings, "reused", "miss-coverage")).To(Equal(map[string]int64{"reused": 1, "miss-coverage": 0}))

			timed, timings = rpchttp.WithTimings(ctx)
			withTests := revision
			withTests.IncludeTests = true
			_, err = engine.IndexRevision(timed, withTests)
			Expect(err).ToNot(HaveOccurred())
			Expect(reuseCounters(timings, "miss-configuration")).To(Equal(map[string]int64{"miss-configuration": 1}))
		},
		Entry("on SQLite", indexerSQLiteOptions),
		Entry("on PostgreSQL", indexerPostgresOptions),
	)

	It("extracts a repeat of a commit whose degraded coverage an infrastructure failure caused", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		revision := writeAbsentImportCommit(ctx, engine)
		loads := failAuditLoads(engine)
		first, err := engine.IndexRevision(ctx, revision)
		Expect(err).ToNot(HaveOccurred())
		Expect(loadSnapshot(database, first.SnapshotID)).To(HaveField("CoverageDeterministic", HaveValue(BeFalse())))

		timed, timings := rpchttp.WithTimings(ctx)
		again, err := engine.IndexRevision(timed, revision)
		Expect(err).ToNot(HaveOccurred())
		Expect(again).To(And(HaveField("Unchanged", true), HaveField("SnapshotID", first.SnapshotID)), "the identical re-extraction is not published twice")
		Expect(*loads).To(Equal(2), "an unreachable proxy is not a fact of the commit, so the repeat is type-checked")
		Expect(reuseCounters(timings, "miss-coverage")).To(Equal(map[string]int64{"miss-coverage": 1}))
	})

	It("extracts and reclassifies a repeat of a degraded snapshot published before coverage causes were recorded", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		revision := writeAbsentImportCommit(ctx, engine)
		loads := countLoads(engine)
		first, err := engine.IndexRevision(ctx, revision)
		Expect(err).ToNot(HaveOccurred())
		Expect(database.Model(&storage.ModuleSnapshot{}).Where("id = ?", first.SnapshotID).Update("coverage_deterministic", nil).Error).To(Succeed())

		timed, timings := rpchttp.WithTimings(ctx)
		reclassified, err := engine.IndexRevision(timed, revision)
		Expect(err).ToNot(HaveOccurred())
		Expect(*loads).To(Equal(2), "an unclassified degraded snapshot is not reusable without extracting")
		Expect(reuseCounters(timings, "miss-coverage")).To(Equal(map[string]int64{"miss-coverage": 1}))
		Expect(reclassified.SnapshotID).ToNot(Equal(first.SnapshotID), "the unclassified row does not stand in for a classified extraction")
		Expect(loadSnapshot(database, reclassified.SnapshotID)).To(HaveField("CoverageDeterministic", HaveValue(BeTrue())))

		again, err := engine.IndexRevision(ctx, revision)
		Expect(err).ToNot(HaveOccurred())
		Expect(again.SnapshotID).To(Equal(reclassified.SnapshotID))
		Expect(*loads).To(Equal(2))
	})
})
