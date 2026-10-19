package indexer

import (
	"path/filepath"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("metadata-only commits", func() {
	DescribeTable("publishes a distinct commit without loading packages and preserves every stored fact", func(ctx SpecContext, historical bool) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		writeChainModule(workspace)
		gitOutput(ctx, workspace, "init")
		firstCommit := commitPaths(ctx, workspace, "test: initial sources", ".")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		first := indexOnce(ctx, engine, workspace)
		if historical {
			first, err = engine.IndexRevision(ctx, RevisionOptions{RootKey: chainModule, Checkout: workspace, Commit: firstCommit, Reason: storage.ReasonHistorical})
			Expect(err).ToNot(HaveOccurred())
		}
		head, found, err := loadHead(ctx, database, discoveredRoot{RootKey: chainModule, LocalPath: workspace})
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		writeFile(filepath.Join(workspace, "README.md"), "Updated documentation.\n")
		secondCommit := commitPaths(ctx, workspace, "docs: update readme", "README.md")
		engine, err = New(database)
		Expect(err).ToNot(HaveOccurred())
		loads := countLoads(engine)
		counts := tableCounts(database)
		var second ModuleResult
		if historical {
			second, err = engine.IndexRevision(ctx, RevisionOptions{RootKey: chainModule, Checkout: workspace, Commit: secondCommit, Reason: storage.ReasonHistorical})
			Expect(err).ToNot(HaveOccurred())
		} else {
			second = indexOnce(ctx, engine, workspace)
		}
		Expect(*loads).To(BeZero(), "persisted extraction identity proves this commit's facts even after a new Indexer is created")
		Expect(second).To(And(Not(HaveField("SnapshotID", first.SnapshotID)), HaveField("Unchanged", false), HaveField("ParsedFiles", 0), HaveField("ReusedFiles", 6)))
		snapshot := loadSnapshot(database, second.SnapshotID)
		Expect(snapshot.GitCommit).To(Equal(secondCommit))
		Expect(snapshot.BaseSnapshotID).To(HaveValue(Equal(uuid.MustParse(first.SnapshotID))))
		Expect(activeDocumentIDs(ctx, database, second.SnapshotID)).To(Equal(activeDocumentIDs(ctx, database, first.SnapshotID)))
		Expect(activeContents(ctx, database, second.SnapshotID)).To(Equal(activeContents(ctx, database, first.SnapshotID)))
		Expect(effectiveImplements(ctx, database, second.SnapshotID)).To(ConsistOf(effectiveImplements(ctx, database, first.SnapshotID)))
		beforeSymbols, err := storage.EffectiveSymbols(ctx, database, uuid.MustParse(first.SnapshotID))
		Expect(err).ToNot(HaveOccurred())
		afterSymbols, err := storage.EffectiveSymbols(ctx, database, snapshot.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(afterSymbols).To(Equal(beforeSymbols))
		Expect(countRows(database, &storage.SourceDelta{}, "snapshot_id = ?", snapshot.ID)).To(BeZero())
		for _, model := range []any{&storage.DocumentDelta{}, &storage.SymbolDelta{}, &storage.ImplementsDelta{}} {
			Expect(countRows(database, model, "snapshot_ordinal = ?", snapshot.Ordinal)).To(BeZero())
		}
		current := tableCounts(database)
		for _, table := range []string{"source_revisions", "documents", "symbols", "symbol_postings"} {
			Expect(current[table]).To(Equal(counts[table]), table)
		}
		newHead, _, err := loadHead(ctx, database, discoveredRoot{RootKey: chainModule, LocalPath: workspace})
		Expect(err).ToNot(HaveOccurred())
		if historical {
			Expect(newHead.ID).To(Equal(head.ID))
		} else {
			Expect(newHead.ID).To(Equal(snapshot.ID))
		}
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: workspace, Force: true, Reason: storage.ReasonReindex})
		Expect(err).ToNot(HaveOccurred())
		Expect(*loads).To(Equal(1), "force still verifies all documents against a fresh extraction")
	}, Entry("head", false), Entry("historical", true))

	DescribeTable("loads packages when a new commit changes extraction inputs", func(ctx SpecContext, path, content string) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		writeTwoPackageModule(workspace)
		gitOutput(ctx, workspace, "init")
		commitPaths(ctx, workspace, "test: initial sources", ".")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		first := indexOnce(ctx, engine, workspace)
		writeFile(filepath.Join(workspace, path), content)
		commitPaths(ctx, workspace, "test: change extraction inputs", path)
		loads := countLoads(engine)
		second := indexOnce(ctx, engine, workspace)
		Expect(*loads).To(Equal(1))
		Expect(second.SnapshotID).ToNot(Equal(first.SnapshotID))
	},
		Entry("source", "tax/tax.go", "package tax\n\nfunc Rate() int { return 21 }\n"),
		Entry("manifest with unchanged selected versions", "go.mod", "module example.org/shop\n\ngo 1.26\n\n// Updated build manifest.\n"),
	)

	It("rechecks degraded coverage on a metadata-only commit", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		writeTwoPackageModule(workspace)
		gitOutput(ctx, workspace, "init")
		commitPaths(ctx, workspace, "test: initial sources", ".")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		first := indexOnce(ctx, engine, workspace)
		Expect(database.Model(&storage.ModuleSnapshot{}).Where("id = ?", first.SnapshotID).Update("coverage", storage.CoveragePartial).Error).To(Succeed())
		writeFile(filepath.Join(workspace, "README.md"), "Updated documentation.\n")
		commitPaths(ctx, workspace, "docs: update readme", "README.md")
		loads := countLoads(engine)
		second := indexOnce(ctx, engine, workspace)
		Expect(*loads).To(Equal(1))
		Expect(loadSnapshot(database, second.SnapshotID).Coverage).To(Equal(storage.CoverageIndexed))
	})
})
