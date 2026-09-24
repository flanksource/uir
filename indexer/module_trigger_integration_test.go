package indexer

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// triggerOutcome is the part of a module result the snapshot trigger decides.
type triggerOutcome struct {
	Unchanged   bool
	HeadVersion int64
	ParsedFiles int
	ReusedFiles int
}

func outcomeOf(result ModuleResult) triggerOutcome {
	return triggerOutcome{Unchanged: result.Unchanged, HeadVersion: result.HeadVersion, ParsedFiles: result.ParsedFiles, ReusedFiles: result.ReusedFiles}
}

func loadSnapshot(database *gorm.DB, id string) storage.ModuleSnapshot {
	GinkgoHelper()
	var snapshot storage.ModuleSnapshot
	Expect(database.Where("id = ?", id).First(&snapshot).Error).To(Succeed())
	return snapshot
}

func indexOnce(ctx context.Context, engine *Indexer, path string) ModuleResult {
	GinkgoHelper()
	results, err := engine.IndexModules(ctx, ModuleOptions{Path: path})
	Expect(err).ToNot(HaveOccurred())
	Expect(results).To(HaveLen(1))
	return results[0]
}

func goSumFor(version string) string {
	return "example.org/dependency " + version + " h1:" + strings.Repeat("A", 43) + "=\n" +
		"example.org/dependency " + version + "/go.mod h1:" + strings.Repeat("B", 43) + "=\n"
}

var _ = Describe("snapshot trigger", func() {
	DescribeTable("publishes a snapshot with zero source deltas and reused documents when only go.sum changes",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeTwoPackageModule(workspace)
			writeFile(filepath.Join(workspace, "go.sum"), goSumFor("v1.0.0"))
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			first := indexOnce(ctx, engine, workspace)
			original := activeDocumentIDs(ctx, database, first.SnapshotID)

			writeFile(filepath.Join(workspace, "go.sum"), goSumFor("v1.1.0"))
			bumped := indexOnce(ctx, engine, workspace)
			Expect(outcomeOf(bumped)).To(Equal(triggerOutcome{HeadVersion: 2, ReusedFiles: 3}))
			Expect(countRows(database, &storage.SourceDelta{}, "snapshot_id = ?", bumped.SnapshotID)).To(BeZero())
			Expect(countRows(database, &storage.SourceRevision{}, "path_key IN ?", []string{"go.mod", "go.sum"})).To(BeZero(), "manifests enter the hash only")
			Expect(activeDocumentIDs(ctx, database, bumped.SnapshotID)).To(Equal(original))
			before, after := loadSnapshot(database, first.SnapshotID), loadSnapshot(database, bumped.SnapshotID)
			Expect(after.ContentSetHash).ToNot(Equal(before.ContentSetHash))
			Expect([]string{after.ConfigurationHash, after.ContextHash}).To(Equal([]string{before.ConfigurationHash, before.ContextHash}))

			again := indexOnce(ctx, engine, workspace)
			Expect(outcomeOf(again)).To(Equal(triggerOutcome{Unchanged: true, HeadVersion: 2, ReusedFiles: 3}))
			Expect(again.SnapshotID).To(Equal(bumped.SnapshotID))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("publishes a snapshot when the build variant changes the configuration hash",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeTwoPackageModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			GinkgoT().Setenv("CGO_ENABLED", "1")
			first := indexOnce(ctx, engine, workspace)

			GinkgoT().Setenv("CGO_ENABLED", "0")
			variant := indexOnce(ctx, engine, workspace)
			Expect(outcomeOf(variant)).To(Equal(triggerOutcome{HeadVersion: 2, ParsedFiles: 3}), "a new configuration re-keys every document")
			Expect(countRows(database, &storage.SourceDelta{}, "snapshot_id = ?", variant.SnapshotID)).To(BeZero())
			before, after := loadSnapshot(database, first.SnapshotID), loadSnapshot(database, variant.SnapshotID)
			Expect(after.ContentSetHash).To(Equal(before.ContentSetHash))
			Expect(after.ConfigurationHash).ToNot(Equal(before.ConfigurationHash))
			Expect(after.ContextHash).ToNot(Equal(before.ContextHash))

			again := indexOnce(ctx, engine, workspace)
			Expect(outcomeOf(again)).To(Equal(triggerOutcome{Unchanged: true, HeadVersion: 2, ReusedFiles: 3}))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("hashes a go.work and go.work.sum above the module into its content set",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			module := filepath.Join(workspace, "shop")
			writeTwoPackageModule(module)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			first := indexOnce(ctx, engine, module)

			writeFile(filepath.Join(workspace, "go.work"), "go 1.26\n\nuse ./shop\n")
			joined := indexOnce(ctx, engine, module)
			Expect(outcomeOf(joined)).To(Equal(triggerOutcome{HeadVersion: 2, ReusedFiles: 3}))
			Expect(loadSnapshot(database, joined.SnapshotID).ContentSetHash).ToNot(Equal(loadSnapshot(database, first.SnapshotID).ContentSetHash))

			writeFile(filepath.Join(workspace, "go.work.sum"), goSumFor("v1.0.0"))
			summed := indexOnce(ctx, engine, module)
			Expect(outcomeOf(summed)).To(Equal(triggerOutcome{HeadVersion: 3, ReusedFiles: 3}))
			Expect(countRows(database, &storage.SourceDelta{}, "snapshot_id IN ?", []string{joined.SnapshotID, summed.SnapshotID})).To(BeZero())
			Expect(countRows(database, &storage.SourceRevision{}, "path_key IN ?", []string{"go.work", "go.work.sum"})).To(BeZero())

			again := indexOnce(ctx, engine, module)
			Expect(outcomeOf(again)).To(Equal(triggerOutcome{Unchanged: true, HeadVersion: 3, ReusedFiles: 3}))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("publishes a snapshot when only the base snapshot's context hash differs for a module that imports a workspace sibling",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeSiblingWorkspace(workspace)
			shop := filepath.Join(workspace, "shop")
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			first := indexOnce(ctx, engine, shop)
			original := loadSnapshot(database, first.SnapshotID)
			Expect(database.Model(&storage.ModuleSnapshot{}).Where("id = ?", first.SnapshotID).
				Update("context_hash", strings.Repeat("0", 64)).Error).To(Succeed())

			rerun := indexOnce(ctx, engine, shop)
			Expect(outcomeOf(rerun)).To(Equal(triggerOutcome{HeadVersion: 2, ReusedFiles: 1}))
			Expect(countRows(database, &storage.SourceDelta{}, "snapshot_id = ?", rerun.SnapshotID)).To(BeZero())
			Expect(loadSnapshot(database, rerun.SnapshotID).ContextHash).To(Equal(original.ContextHash))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	It("fails publication when the Go toolchain environment cannot be read", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := GinkgoT().TempDir()
		writeTwoPackageModule(workspace)
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		GinkgoT().Setenv("PATH", "")
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: workspace})
		Expect(err).To(MatchError(ContainSubstring("read go env")))
		Expect(countRows(database, &storage.ModuleRoot{}, "1 = 1")).To(BeZero())
	})
})
