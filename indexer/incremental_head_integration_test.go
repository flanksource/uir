package indexer

import (
	"path/filepath"

	rpchttp "github.com/flanksource/clicky/rpc/http"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/tools/go/packages"
)

var _ = Describe("incremental head extraction", func() {
	It("renders only the changed package and matches a full extraction", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		writeTwoPackageModule(workspace)
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		first := indexOnce(ctx, engine, workspace)
		original := activeDocumentIDs(ctx, database, first.SnapshotID)
		writeFile(filepath.Join(workspace, "tax", "tax.go"), "package tax\n\nfunc Rate() int { return 21 }\n")

		timed, timings := rpchttp.WithTimings(ctx)
		second := indexOnce(timed, engine, workspace)
		rendered, _ := timings.Counter("render-documents", "rendered")
		Expect(rendered).To(Equal(int64(1)))
		current := activeDocumentIDs(ctx, database, second.SnapshotID)
		Expect(current["cart.go"]).To(Equal(original["cart.go"]))
		Expect(current["checkout.go"]).To(Equal(original["checkout.go"]))
		Expect(current["tax/tax.go"]).ToNot(Equal(original["tax/tax.go"]))
		full := openIndexerDB(ctx, indexerSQLiteOptions())
		fullEngine, err := New(full)
		Expect(err).ToNot(HaveOccurred())
		Expect(activeContents(ctx, database, second.SnapshotID)).To(Equal(activeContents(ctx, full, indexOnce(ctx, fullEngine, workspace).SnapshotID)))
		timed, timings = rpchttp.WithTimings(ctx)
		_, err = engine.IndexModules(timed, ModuleOptions{Path: workspace, Force: true, Reason: storage.ReasonReindex})
		Expect(err).ToNot(HaveOccurred())
		rendered, _ = timings.Counter("render-documents", "rendered")
		Expect(rendered).To(Equal(int64(3)), "force verifies every file")
	})

	It("rejects a publication whose reused package base moved during extraction", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		writeTwoPackageModule(workspace)
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		indexOnce(ctx, engine, workspace)
		writeFile(filepath.Join(workspace, "tax", "tax.go"), "package tax\n\nfunc Rate() int { return 21 }\n")
		other, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		load := engine.loadPackages
		var winner ModuleResult
		var counts map[string]int64
		engine.loadPackages = func(config *packages.Config, patterns ...string) ([]*packages.Package, error) {
			loaded, err := load(config, patterns...)
			if err == nil {
				winner = indexOnce(ctx, other, workspace)
				counts = tableCounts(database)
			}
			return loaded, err
		}
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: workspace, Reason: storage.ReasonReindex})
		Expect(err).To(MatchError(ContainSubstring(ErrIndexInputsChanged.Error())))
		Expect(tableCounts(database)).To(Equal(counts))
		head, found, err := loadHead(ctx, database, discoveredRoot{RootKey: "example.org/shop", LocalPath: workspace})
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(head.ID.String()).To(Equal(winner.SnapshotID))
	})

	DescribeTable("refreshes unchanged package documents when diagnostic context changes", func(ctx SpecContext, nextDiagnostic string) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		writeTwoPackageModule(workspace)
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		load := engine.loadPackages
		diagnostic := "incomplete type information"
		engine.loadPackages = func(config *packages.Config, patterns ...string) ([]*packages.Package, error) {
			loaded, err := load(config, patterns...)
			for _, pkg := range loaded {
				if pkg.PkgPath == "example.org/shop" && diagnostic != "" {
					pkg.Errors = append(pkg.Errors, packages.Error{Kind: packages.TypeError, Msg: diagnostic})
				}
			}
			return loaded, err
		}
		first := indexOnce(ctx, engine, workspace)
		Expect(loadSnapshot(database, first.SnapshotID).Coverage).To(Equal(storage.CoveragePartial))
		diagnostic = nextDiagnostic
		writeFile(filepath.Join(workspace, "tax", "tax.go"), "package tax\n\nfunc Rate() int { return 21 }\n")
		second := indexOnce(ctx, engine, workspace)
		full := openIndexerDB(ctx, indexerSQLiteOptions())
		fullEngine, err := New(full)
		Expect(err).ToNot(HaveOccurred())
		fullEngine.loadPackages = engine.loadPackages
		Expect(activeContents(ctx, database, second.SnapshotID)).To(Equal(activeContents(ctx, full, indexOnce(ctx, fullEngine, workspace).SnapshotID)))
	}, Entry("coverage improves", ""), Entry("diagnostics change within partial coverage", "updated type information"))
})
