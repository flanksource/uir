package indexer

import (
	"os"
	"path/filepath"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type snapshotSummary struct {
	Kind   storage.SnapshotKind
	Reason storage.SnapshotReason
}

var _ = Describe("snapshot reasons", func() {
	It("records why, when, and as what each snapshot of a checkout was published", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		checkout := canonicalTempDir()
		gitOutput(ctx, checkout, "init")
		writeFile(filepath.Join(checkout, "go.mod"), "module example.org/pricing\n\ngo 1.26\n")
		writeFile(filepath.Join(checkout, "pricing.go"), "package pricing\n\nfunc Price() int { return 1 }\n")
		tagged := commitPaths(ctx, checkout, "test: tagged", "go.mod", "pricing.go")
		gitOutput(ctx, checkout, "tag", "v1.0.0")
		writeFile(filepath.Join(checkout, "pricing.go"), "package pricing\n\nfunc Price() int { return 2 }\n")
		historical := commitPaths(ctx, checkout, "test: historical", "pricing.go")
		writeFile(filepath.Join(checkout, "pricing.go"), "package pricing\n\nfunc Price() int { return 3 }\n")
		commitPaths(ctx, checkout, "test: head", "pricing.go")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())

		added := indexOnce(ctx, engine, checkout)
		writeFile(filepath.Join(checkout, "pricing.go"), "package pricing\n\nfunc Price() int { return 4 }\n")
		reindexed, err := engine.IndexModules(ctx, ModuleOptions{Path: checkout, ExistingOnly: true, Reason: storage.ReasonReindex})
		Expect(err).ToNot(HaveOccurred())
		past, err := engine.IndexRevision(ctx, RevisionOptions{RootKey: "example.org/pricing", Checkout: checkout, Commit: historical, Reason: storage.ReasonHistorical})
		Expect(err).ToNot(HaveOccurred())
		version, err := engine.versionedSnapshot(ctx, "example.org/pricing", "v1.0.0", false)
		Expect(err).ToNot(HaveOccurred())
		Expect(version.GitCommit).To(Equal(tagged))

		listed, _, err := storage.ModuleSnapshots(ctx, database, storage.ModuleSnapshotListOptions{RootKey: "example.org/pricing", Location: checkout})
		Expect(err).ToNot(HaveOccurred())
		summaries := map[string]snapshotSummary{}
		for _, row := range listed {
			summaries[row.ID.String()] = snapshotSummary{Kind: row.Kind, Reason: row.Reason}
			Expect(row.IndexStartedAt).ToNot(BeNil())
			Expect(*row.IndexStartedAt).To(BeTemporally("<=", row.CompletedAt))
			Expect(row.TaskRunID).To(BeNil(), "no task run published these")
		}
		Expect(summaries).To(Equal(map[string]snapshotSummary{
			added.SnapshotID:        {Kind: storage.SnapshotKindHead, Reason: storage.ReasonAdd},
			reindexed[0].SnapshotID: {Kind: storage.SnapshotKindHead, Reason: storage.ReasonReindex},
			past.SnapshotID:         {Kind: storage.SnapshotKindHistorical, Reason: storage.ReasonHistorical},
			version.ID.String():     {Kind: storage.SnapshotKindVersioned, Reason: storage.ReasonVersionedDependency},
		}))
	})

	It("records a local dependency indexed on the way as one", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		shop, pricing := filepath.Join(workspace, "shop"), filepath.Join(workspace, "pricing")
		Expect(os.MkdirAll(shop, 0o755)).To(Succeed())
		Expect(os.MkdirAll(pricing, 0o755)).To(Succeed())
		writeFile(filepath.Join(workspace, "go.work"), "go 1.26\n\nuse ./shop\n\nreplace example.org/pricing => ./pricing\n")
		writeFile(filepath.Join(shop, "go.mod"), "module example.org/shop\n\ngo 1.26\n\nrequire example.org/pricing v0.0.0\n")
		writeFile(filepath.Join(shop, "shop.go"), "package shop\n\nimport \"example.org/pricing\"\n\nfunc Total() int { return pricing.Price() }\n")
		writeFile(filepath.Join(pricing, "go.mod"), "module example.org/pricing\n\ngo 1.26\n")
		writeFile(filepath.Join(pricing, "pricing.go"), "package pricing\n\nfunc Price() int { return 1 }\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())

		indexOnce(ctx, engine, shop)

		var reasons []string
		Expect(database.Table("snapshots AS snapshot").Joins("JOIN modules AS root ON root.id = snapshot.root_id").
			Order("root.root_key").Pluck("snapshot.reason", &reasons).Error).To(Succeed())
		Expect(reasons).To(Equal([]string{string(storage.ReasonLocalDependency), string(storage.ReasonAdd)}))
	})

	It("rejects an index without a reason", func(ctx SpecContext) {
		engine, err := New(openIndexerDB(ctx, indexerSQLiteOptions()))
		Expect(err).ToNot(HaveOccurred())
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: canonicalTempDir()})
		Expect(err).To(MatchError(ContainSubstring("reason")))
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: canonicalTempDir(), Reason: storage.ReasonHistorical})
		Expect(err).To(MatchError(ContainSubstring("not one of add, reindex, refactor, local-dependency")))
	})
})
