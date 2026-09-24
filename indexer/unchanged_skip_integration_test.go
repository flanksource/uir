package indexer

import (
	"context"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/tools/go/packages"
)

// countLoads makes the engine count its go/packages loads.
func countLoads(engine *Indexer) *int {
	loads := 0
	engine.loadPackages = func(config *packages.Config, patterns ...string) ([]*packages.Package, error) {
		loads++
		return packages.Load(config, patterns...)
	}
	return &loads
}

// writeSiblingWorkspace writes a go.work over example.org/pricing and example.org/shop, whose only
// file imports pricing, so shop's context depends on pricing's export shape.
func writeSiblingWorkspace(workspace string) {
	GinkgoHelper()
	for _, module := range []string{"pricing", "shop"} {
		Expect(os.MkdirAll(filepath.Join(workspace, module), 0o755)).To(Succeed())
		writeFile(filepath.Join(workspace, module, "go.mod"), "module example.org/"+module+"\n\ngo 1.26\n")
	}
	writeFile(filepath.Join(workspace, "go.work"), "go 1.26\n\nuse (\n\t./pricing\n\t./shop\n)\n")
	writeFile(filepath.Join(workspace, "pricing", "pricing.go"), "package pricing\n\nfunc Base() int { return 10 }\n")
	writeFile(filepath.Join(workspace, "shop", "cart.go"), "package shop\n\nimport \"example.org/pricing\"\n\nfunc Total(items int) int { return items * pricing.Base() }\n")
}

// indexWorkspace indexes both modules of the sibling workspace and returns their results by root key.
func indexWorkspace(ctx context.Context, engine *Indexer, workspace string) map[string]ModuleResult {
	GinkgoHelper()
	results, err := engine.IndexModules(ctx, ModuleOptions{Path: workspace})
	Expect(err).ToNot(HaveOccurred())
	byRoot := map[string]ModuleResult{}
	for _, result := range results {
		byRoot[result.RootKey] = result
	}
	Expect(byRoot).To(HaveLen(2))
	return byRoot
}

var _ = Describe("unchanged run", func() {
	DescribeTable("reuses the head without loading packages when revision, content set and configuration are unchanged",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeTwoPackageModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			loads := countLoads(engine)
			first := indexOnce(ctx, engine, workspace)
			Expect(*loads).To(Equal(1))

			again := indexOnce(ctx, engine, workspace)
			Expect(outcomeOf(again)).To(Equal(triggerOutcome{Unchanged: true, HeadVersion: 1, ReusedFiles: 3}))
			Expect(again.SnapshotID).To(Equal(first.SnapshotID))
			Expect(*loads).To(Equal(1), "an unchanged run must not type-check")

			writeFile(filepath.Join(workspace, "checkout.go"), "package shop\n\nfunc Checkout(c *Cart) int { return len(c.Items) + 0 }\n")
			edited := indexOnce(ctx, engine, workspace)
			Expect(outcomeOf(edited)).To(Equal(triggerOutcome{HeadVersion: 2, ParsedFiles: 2, ReusedFiles: 1}))
			Expect(*loads).To(Equal(2))

			forced, err := engine.IndexModules(ctx, ModuleOptions{Path: workspace, Force: true})
			Expect(err).ToNot(HaveOccurred())
			Expect(forced).To(HaveLen(1))
			Expect(forced[0].Unchanged).To(BeFalse())
			Expect(*loads).To(Equal(3), "force always type-checks")
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("type-checks a module that imports a workspace sibling and republishes it when the sibling's export shape changes",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeSiblingWorkspace(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			loads := countLoads(engine)
			first := indexWorkspace(ctx, engine, workspace)
			Expect(*loads).To(Equal(2))

			again := indexWorkspace(ctx, engine, workspace)
			Expect(outcomeOf(again["example.org/pricing"])).To(Equal(triggerOutcome{Unchanged: true, HeadVersion: 1, ReusedFiles: 1}))
			Expect(outcomeOf(again["example.org/shop"])).To(Equal(triggerOutcome{Unchanged: true, HeadVersion: 1, ReusedFiles: 1}))
			Expect(*loads).To(Equal(3), "only shop, which imports a sibling, is type-checked")

			writeFile(filepath.Join(workspace, "pricing", "pricing.go"), "package pricing\n\nfunc Base() int { return 11 }\n")
			body := indexWorkspace(ctx, engine, workspace)
			Expect(outcomeOf(body["example.org/pricing"])).To(Equal(triggerOutcome{HeadVersion: 2, ParsedFiles: 1}))
			Expect(outcomeOf(body["example.org/shop"])).To(Equal(triggerOutcome{Unchanged: true, HeadVersion: 1, ReusedFiles: 1}), "a sibling body change keeps shop's context")
			Expect(*loads).To(Equal(5))

			writeFile(filepath.Join(workspace, "pricing", "pricing.go"), "package pricing\n\nfunc Base() int { return 11 }\n\nfunc Discount() int { return 1 }\n")
			shaped := indexWorkspace(ctx, engine, workspace)
			Expect(outcomeOf(shaped["example.org/pricing"])).To(Equal(triggerOutcome{HeadVersion: 3, ParsedFiles: 1}))
			Expect(outcomeOf(shaped["example.org/shop"])).To(Equal(triggerOutcome{HeadVersion: 2, ParsedFiles: 1}), "a sibling export change re-keys shop's document")
			Expect(*loads).To(Equal(7))
			before, after := loadSnapshot(database, first["example.org/shop"].SnapshotID), loadSnapshot(database, shaped["example.org/shop"].SnapshotID)
			Expect([]string{after.ContentSetHash, after.ConfigurationHash}).To(Equal([]string{before.ContentSetHash, before.ConfigurationHash}))
			Expect(after.ContextHash).ToNot(Equal(before.ContextHash))
			Expect(countRows(database, &storage.SourceDelta{}, "snapshot_id = ?", shaped["example.org/shop"].SnapshotID)).To(BeZero())
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	It("fails when a go.work use directory of an otherwise unchanged module has no go.mod", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := GinkgoT().TempDir()
		writeSiblingWorkspace(workspace)
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		indexWorkspace(ctx, engine, workspace)
		Expect(os.Remove(filepath.Join(workspace, "pricing", "go.mod"))).To(Succeed())
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: filepath.Join(workspace, "shop")})
		Expect(err).To(MatchError(And(ContainSubstring("read go.work use"), ContainSubstring(filepath.Join("pricing", "go.mod")))))
	})
})
