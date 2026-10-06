package storage_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const (
	statsManifest = "module example.org/stats\n\ngo 1.26\n"
	statsA        = "package stats\n\nfunc A() int { return 1 }\n"
	statsAChanged = "package stats\n\nfunc A() int { return 3 }\n"
	statsB        = "package stats\n\nfunc B() int { return 2 }\n"
	statsC        = "package stats\n\nfunc C() int { return 4 }\n"
	statsKept     = "package kept\n\nfunc Kept() int { return 5 }\n"
)

// indexStats indexes the module at path for reason and returns its one snapshot id.
func indexStats(ctx context.Context, database *gorm.DB, path string, reason storage.SnapshotReason) uuid.UUID {
	GinkgoHelper()
	engine, err := indexer.New(database)
	Expect(err).ToNot(HaveOccurred())
	results, err := engine.IndexModules(ctx, indexer.ModuleOptions{Path: path, Reason: reason})
	Expect(err).ToNot(HaveOccurred())
	Expect(results).To(HaveLen(1))
	return uuid.MustParse(results[0].SnapshotID)
}

func writeStatsFile(path, content string) {
	GinkgoHelper()
	Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
}

// statsModule publishes example.org/stats with a.go, b.go, and the kept files, then changes a.go,
// deletes b.go, and adds c.go, and returns both snapshots.
func statsModule(ctx context.Context, database *gorm.DB, kept map[string]string) (string, uuid.UUID, uuid.UUID) {
	GinkgoHelper()
	module, err := filepath.EvalSymlinks(GinkgoT().TempDir())
	Expect(err).ToNot(HaveOccurred())
	writeStatsFile(filepath.Join(module, "go.mod"), statsManifest)
	writeStatsFile(filepath.Join(module, "a.go"), statsA)
	writeStatsFile(filepath.Join(module, "b.go"), statsB)
	for name, content := range kept {
		Expect(os.MkdirAll(filepath.Dir(filepath.Join(module, name)), 0o755)).To(Succeed())
		writeStatsFile(filepath.Join(module, name), content)
	}
	first := indexStats(ctx, database, module, storage.ReasonAdd)
	writeStatsFile(filepath.Join(module, "a.go"), statsAChanged)
	Expect(os.Remove(filepath.Join(module, "b.go"))).To(Succeed())
	writeStatsFile(filepath.Join(module, "c.go"), statsC)
	return module, first, indexStats(ctx, database, module, storage.ReasonReindex)
}

func recordedStats(database *gorm.DB, ids ...uuid.UUID) []storage.SnapshotStats {
	GinkgoHelper()
	stats := make([]storage.SnapshotStats, 0, len(ids))
	for _, id := range ids {
		var snapshot storage.ModuleSnapshot
		Expect(database.Where("id = ?", id).Take(&snapshot).Error).To(Succeed())
		recorded, err := snapshot.Stats()
		Expect(err).ToNot(HaveOccurred())
		stats = append(stats, recorded)
	}
	return stats
}

var _ = Describe("snapshot stats", func() {
	DescribeTable("backfills the stats of rows published before they were recorded exactly as publication records them",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			database := openDB(ctx, config)
			_, first, second := statsModule(ctx, database, nil)
			published := recordedStats(database, first, second)
			Expect(published[0]).To(And(HaveField("FileCount", int64(2)), HaveField("SourceBytes", int64(len(statsA)+len(statsB))),
				HaveField("FilesAdded", 2), HaveField("FilesChanged", 0), HaveField("FilesDeleted", 0)))
			Expect(published[1]).To(And(HaveField("FileCount", int64(2)), HaveField("SourceBytes", int64(len(statsAChanged)+len(statsC))),
				HaveField("FilesAdded", 1), HaveField("FilesChanged", 1), HaveField("FilesDeleted", 1)))
			Expect(published[1].SymbolCount).To(BeNumerically(">", 0))
			Expect(published[1].SymbolsChanged).To(BeNumerically(">", 0))

			Expect(database.Exec("UPDATE snapshots SET file_count = NULL, symbol_count = NULL, occurrence_count = NULL, source_bytes = NULL, " +
				"files_added = NULL, files_changed = NULL, files_deleted = NULL, symbols_changed = NULL").Error).To(Succeed())
			reopened := openDB(ctx, config)

			Expect(recordedStats(reopened, first, second)).To(Equal(published))
		},
		Entry("SQLite", sqliteOptions("stats.db")),
		Entry("PostgreSQL", postgresOptions("uir_snapshot_stats")),
	)

	DescribeTable("records at publication the stats a full re-derivation of the published snapshot computes",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			_, first, second := statsModule(ctx, database, map[string]string{"kept/kept.go": statsKept})
			var keptDocuments int64
			Expect(database.Model(&storage.Document{}).Where("path_key = ?", "kept/kept.go").Count(&keptDocuments).Error).To(Succeed())
			Expect(keptDocuments).To(Equal(int64(1)), "the second snapshot reuses the first one's document for the unchanged package")
			published := recordedStats(database, first, second)
			Expect(published[1]).To(And(HaveField("FileCount", int64(3)), HaveField("SourceBytes", int64(len(statsAChanged)+len(statsC)+len(statsKept))),
				HaveField("FilesAdded", 1), HaveField("FilesChanged", 1), HaveField("FilesDeleted", 1)))

			rederived := make([]storage.SnapshotStats, 0, 2)
			for _, id := range []uuid.UUID{first, second} {
				derived, err := storage.DeriveSnapshotStatsOptions(ctx, database, id)
				Expect(err).ToNot(HaveOccurred())
				stats, err := storage.RecordSnapshotStats(ctx, database, id, derived)
				Expect(err).ToNot(HaveOccurred())
				rederived = append(rederived, stats)
			}

			Expect(published).To(Equal(rederived))
		},
		Entry("SQLite", sqliteOptions("rederived.db")),
		Entry("PostgreSQL", postgresOptions("uir_snapshot_stats_rederived")),
	)

	It("refuses to record stats without base sources or files, instead of counting them as empty", func(ctx SpecContext) {
		database := openDB(ctx, sqliteOptions("missing-inputs.db")())
		_, _, second := statsModule(ctx, database, nil)
		published := recordedStats(database, second)

		_, withoutBase := storage.RecordSnapshotStats(ctx, database, second, storage.SnapshotStatsOptions{Files: map[string]storage.SnapshotFile{}})
		_, withoutFiles := storage.RecordSnapshotStats(ctx, database, second, storage.SnapshotStatsOptions{Base: map[string]storage.SourceRevision{}})

		Expect(withoutBase).To(MatchError(ContainSubstring("base sources and files are required")))
		Expect(withoutFiles).To(MatchError(ContainSubstring("base sources and files are required")))
		Expect(recordedStats(database, second)).To(Equal(published))
	})

	DescribeTable("recognises a duplicate key, and only a duplicate key, as a unique violation",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			_, first, _ := statsModule(ctx, database, nil)
			var head storage.ModuleLocationHead
			Expect(database.Where("snapshot_id IN (SELECT id FROM snapshots WHERE id <> ?)", first).Take(&head).Error).To(Succeed())

			duplicate := database.Create(&storage.ModuleLocationHead{RootID: head.RootID, LocationID: head.LocationID, SnapshotID: head.SnapshotID, Version: 9}).Error
			dangling := database.Create(&storage.ModuleLocationHead{RootID: head.RootID, LocationID: uuid.New(), SnapshotID: head.SnapshotID, Version: 1}).Error

			Expect(duplicate).To(HaveOccurred())
			Expect(storage.IsUniqueViolation(duplicate)).To(BeTrue(), duplicate.Error())
			Expect(dangling).To(HaveOccurred())
			Expect(storage.IsUniqueViolation(dangling)).To(BeFalse(), dangling.Error())
			Expect(storage.IsUniqueViolation(errors.New("duplicate key value violates unique constraint"))).To(BeFalse())
		},
		Entry("SQLite", sqliteOptions("unique.db")),
		Entry("PostgreSQL", postgresOptions("uir_unique_violation")),
	)
})
