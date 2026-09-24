package indexer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func indexerSQLiteOptions() storage.DBOptions {
	return storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "index.db")}
}

func indexerPostgresOptions() storage.DBOptions {
	return storage.DBOptions{DSN: dbtest.ForGinkgo(dbtest.Options{Name: "uir_indexer_documents"}).DSN(), Schema: "uir_indexer_documents"}
}

func openIndexerDB(ctx context.Context, options storage.DBOptions) *gorm.DB {
	GinkgoHelper()
	database, err := storage.UirDB(ctx, options)
	Expect(err).To(Succeed())
	DeferCleanup(closeIndexerDB, database)
	return database
}

// writeTwoPackageModule writes example.org/shop with two files in its root package and one in ./tax.
func writeTwoPackageModule(workspace string) {
	GinkgoHelper()
	Expect(os.MkdirAll(filepath.Join(workspace, "tax"), 0o755)).To(Succeed())
	writeFile(filepath.Join(workspace, "go.mod"), "module example.org/shop\n\ngo 1.26\n")
	writeFile(filepath.Join(workspace, "cart.go"), "package shop\n\ntype Cart struct{ Items []string }\n\nfunc (c *Cart) Add(item string) { c.Items = append(c.Items, item) }\n")
	writeFile(filepath.Join(workspace, "checkout.go"), "package shop\n\nfunc Checkout(c *Cart) int { return len(c.Items) }\n")
	writeFile(filepath.Join(workspace, "tax", "tax.go"), "package tax\n\nfunc Rate() int { return 20 }\n")
}

func countRows(database *gorm.DB, model any, where string, args ...any) int64 {
	GinkgoHelper()
	var count int64
	Expect(database.Model(model).Where(where, args...).Count(&count).Error).To(Succeed())
	return count
}

func loadRevision(database *gorm.DB, id uuid.UUID) storage.SourceRevision {
	GinkgoHelper()
	var revision storage.SourceRevision
	Expect(database.Where("id = ?", id).First(&revision).Error).To(Succeed())
	return revision
}

func mustJSON(value any) storage.JSON {
	GinkgoHelper()
	encoded, err := json.Marshal(value)
	Expect(err).ToNot(HaveOccurred())
	return storage.JSON(encoded)
}

func activeDocumentIDs(ctx context.Context, database *gorm.DB, snapshotID string) map[string]uuid.UUID {
	GinkgoHelper()
	active, err := storage.ActiveDocuments(ctx, database, uuid.MustParse(snapshotID))
	Expect(err).ToNot(HaveOccurred())
	ids := map[string]uuid.UUID{}
	for path, document := range active {
		_, err := storage.DecodeDocument(document.Document, document.Source)
		Expect(err).ToNot(HaveOccurred(), path)
		ids[path] = document.Document.ID
	}
	return ids
}

var _ = Describe("document publication", func() {
	DescribeTable("publishes one document per file under its package input hash and reuses it by key",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeTwoPackageModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			first, err := engine.IndexModules(ctx, ModuleOptions{Path: workspace})
			Expect(err).ToNot(HaveOccurred())
			Expect([]int{first[0].ParsedFiles, first[0].ReusedFiles}).To(Equal([]int{3, 0}))
			var snapshot storage.ModuleSnapshot
			Expect(database.Where("id = ?", first[0].SnapshotID).First(&snapshot).Error).To(Succeed())
			Expect(snapshot.Coverage).To(Equal(storage.CoverageIndexed))
			Expect(snapshot.PackageCount).To(Equal(2))
			Expect(snapshot.ContextHash).To(HaveLen(64))
			Expect(snapshot.Diagnostics).To(MatchJSON(`[]`))
			var coverage []storage.PackageCoverage
			Expect(database.Where("snapshot_id = ?", snapshot.ID).Order("package_path").Find(&coverage).Error).To(Succeed())
			Expect(coverage).To(HaveLen(2))
			Expect([]string{coverage[0].PackagePath, coverage[1].PackagePath}).To(Equal([]string{"example.org/shop", "example.org/shop/tax"}))
			Expect([]int{coverage[0].FileCount, coverage[1].FileCount}).To(Equal([]int{2, 1}))
			for _, row := range coverage {
				Expect(row.Coverage).To(Equal(storage.CoverageIndexed))
				Expect(row.ExportShapeHash).To(HaveValue(HaveLen(64)))
				Expect(row.Diagnostics).To(MatchJSON(`[]`))
			}
			original := activeDocumentIDs(ctx, database, first[0].SnapshotID)
			Expect(original).To(HaveLen(3))

			forced, err := engine.IndexModules(ctx, ModuleOptions{Path: workspace, Force: true})
			Expect(err).ToNot(HaveOccurred())
			Expect(forced[0].ParsedFiles).To(Equal(3), "force re-extracts and verifies every document")
			Expect(countRows(database, &storage.Document{}, "1 = 1")).To(Equal(int64(3)), "a re-run must not duplicate documents")
			Expect(activeDocumentIDs(ctx, database, forced[0].SnapshotID)).To(Equal(original))
			var forcedSnapshot storage.ModuleSnapshot
			Expect(database.Where("id = ?", forced[0].SnapshotID).First(&forcedSnapshot).Error).To(Succeed())
			Expect(forcedSnapshot.ContextHash).To(Equal(snapshot.ContextHash))

			writeFile(filepath.Join(workspace, "checkout.go"), "package shop\n\nfunc Checkout(c *Cart) int { return 2 * len(c.Items) }\n")
			edited, err := engine.IndexModules(ctx, ModuleOptions{Path: workspace})
			Expect(err).ToNot(HaveOccurred())
			Expect([]int{edited[0].ParsedFiles, edited[0].ReusedFiles}).To(Equal([]int{2, 1}), "an edit re-extracts its whole package")
			current := activeDocumentIDs(ctx, database, edited[0].SnapshotID)
			Expect(current["tax/tax.go"]).To(Equal(original["tax/tax.go"]))
			Expect(current["cart.go"]).ToNot(Equal(original["cart.go"]))
			Expect(countRows(database, &storage.Document{}, "1 = 1")).To(Equal(int64(5)))
			Expect(activeDocumentIDs(ctx, database, first[0].SnapshotID)).To(Equal(original), "history keeps its documents")
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("refreshes the derived columns of an existing symbol row on reindex",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeTwoPackageModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			_, err = engine.IndexModules(ctx, ModuleOptions{Path: workspace})
			Expect(err).ToNot(HaveOccurred())
			stale := map[string]any{"search_name": "check_out", "visibility": "internal"}
			Expect(database.Model(&storage.Symbol{}).Where("name = ?", "Checkout").Updates(stale).Error).To(Succeed(),
				"a row written under an earlier normalization")

			_, err = engine.IndexModules(ctx, ModuleOptions{Path: workspace, Force: true})
			Expect(err).ToNot(HaveOccurred())
			var symbol storage.Symbol
			Expect(database.Where("name = ?", "Checkout").First(&symbol).Error).To(Succeed())
			Expect([]string{symbol.SearchName, symbol.Visibility}).To(Equal([]string{"checkout", "exported"}))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	It("refuses a forced re-extraction whose document differs under the same indexer version", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := GinkgoT().TempDir()
		writeTwoPackageModule(workspace)
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: workspace})
		Expect(err).ToNot(HaveOccurred())
		var document storage.Document
		Expect(database.Where("path_key = ?", "tax/tax.go").First(&document).Error).To(Succeed())
		var content storage.DocumentContent
		content, err = storage.DecodeDocument(document, loadRevision(database, document.SourceRevisionID))
		Expect(err).ToNot(HaveOccurred())
		content.Symbols[0].Shape = "func Rate() string"
		Expect(database.Model(&document).Update("content", mustJSON(content)).Error).To(Succeed())

		_, err = engine.IndexModules(ctx, ModuleOptions{Path: workspace, Force: true})
		Expect(err).To(MatchError(ContainSubstring(`document for "tax/tax.go" under input hash`)))
		Expect(err).To(MatchError(ContainSubstring("changed without an indexer version change")))
	})

	DescribeTable("reindexes cleanly after discarding a previous-generation database",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			legacy := openIndexerDB(ctx, config)
			for _, statement := range []string{
				"CREATE TABLE uir_module_roots (id TEXT PRIMARY KEY)",
				"CREATE TABLE uir_module_snapshots (id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES uir_module_roots(id), state TEXT NOT NULL)",
				"INSERT INTO uir_module_roots (id) VALUES ('old')",
				"INSERT INTO uir_module_snapshots (id, root_id, state) VALUES ('old', 'old', 'ready')",
			} {
				Expect(legacy.Exec(statement).Error).To(Succeed(), statement)
			}
			database := openIndexerDB(ctx, config)
			Expect(database.Migrator().HasTable("uir_module_snapshots")).To(BeFalse())
			Expect(database.Migrator().HasTable("uir_module_roots")).To(BeFalse())
			workspace := GinkgoT().TempDir()
			writeTwoPackageModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			results, err := engine.IndexModules(ctx, ModuleOptions{Path: workspace})
			Expect(err).ToNot(HaveOccurred())
			Expect(activeDocumentIDs(ctx, database, results[0].SnapshotID)).To(HaveLen(3))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)
})
