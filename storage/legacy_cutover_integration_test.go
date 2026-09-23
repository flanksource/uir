package storage_test

import (
	"context"
	"path/filepath"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var legacyTables = []string{
	"uir_relationships", "uir_fields", "uir_node_locations", "uir_nodes",
	"uir_sources", "uir_roots", "uir_project_heads", "uir_snapshots", "uir_projects",
}

var _ = Describe("legacy project cutover", func() {
	DescribeTable("discards only legacy project tables",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			database := openDB(ctx, config)
			for _, name := range legacyTables {
				if !database.Migrator().HasTable(name) {
					Expect(database.Exec("CREATE TABLE " + name + " (id TEXT PRIMARY KEY)").Error).To(Succeed())
				}
			}
			Expect(database.Exec("CREATE TABLE external_sentinel (id TEXT PRIMARY KEY)").Error).To(Succeed())
			Expect(database.Exec("INSERT INTO external_sentinel (id) VALUES (?)", "keep-me").Error).To(Succeed())

			cutover := openDB(ctx, config)
			for _, name := range legacyTables {
				Expect(cutover.Migrator().HasTable(name)).To(BeFalse(), name)
			}
			var count int64
			Expect(cutover.Table("external_sentinel").Where("id = ?", "keep-me").Count(&count).Error).To(Succeed())
			Expect(count).To(Equal(int64(1)))
			Expect(cutover.Migrator().HasTable(&storage.ModuleRoot{})).To(BeTrue())
		},
		Entry("SQLite", func() storage.DBOptions { return storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "cutover.db")} }),
		Entry("PostgreSQL", func() storage.DBOptions {
			return storage.DBOptions{DSN: dbtest.ForGinkgo(dbtest.Options{Name: "uir_legacy_cutover"}).DSN(), Schema: "uir_legacy_cutover"}
		}),
	)

	It("refuses to discard a legacy table referenced by an external SQLite table", func(ctx SpecContext) {
		config := storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "referenced.db")}
		database := openDB(ctx, config)
		if !database.Migrator().HasTable("uir_projects") {
			Expect(database.Exec("CREATE TABLE uir_projects (id TEXT PRIMARY KEY)").Error).To(Succeed())
		}
		Expect(database.Exec("CREATE TABLE external_reference (id TEXT PRIMARY KEY, project_id TEXT REFERENCES uir_projects(id))").Error).To(Succeed())
		_, err := storage.UirDB(context.Background(), config)
		Expect(err).To(MatchError(ContainSubstring("external_reference")))
		Expect(database.Migrator().HasTable("uir_projects")).To(BeTrue())
	})
})
