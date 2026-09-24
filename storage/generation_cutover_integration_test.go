package storage_test

import (
	"context"
	"path/filepath"
	"time"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

var previousGenerationTables = []string{
	"uir_source_deltas", "uir_module_location_heads", "uir_module_primaries",
	"uir_source_revisions", "uir_module_snapshots", "uir_module_locations", "uir_module_roots",
}

// previousGenerationDDL recreates the uir_-prefixed module-root generation, parents first, with the
// references that force a child-first drop, and one row per table so every drop removes data.
var previousGenerationDDL = []string{
	"CREATE TABLE uir_module_roots (id TEXT PRIMARY KEY)",
	"CREATE TABLE uir_module_locations (id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES uir_module_roots(id), parent_location_id TEXT REFERENCES uir_module_locations(id))",
	"CREATE TABLE uir_module_snapshots (id TEXT PRIMARY KEY, location_id TEXT NOT NULL REFERENCES uir_module_locations(id), base_snapshot_id TEXT REFERENCES uir_module_snapshots(id), state TEXT NOT NULL)",
	"CREATE TABLE uir_module_location_heads (location_id TEXT PRIMARY KEY REFERENCES uir_module_locations(id), snapshot_id TEXT NOT NULL REFERENCES uir_module_snapshots(id))",
	"CREATE TABLE uir_module_primaries (root_id TEXT PRIMARY KEY REFERENCES uir_module_roots(id), location_id TEXT NOT NULL REFERENCES uir_module_locations(id))",
	"CREATE TABLE uir_source_revisions (id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES uir_module_roots(id), projection TEXT NOT NULL)",
	"CREATE TABLE uir_source_deltas (snapshot_id TEXT NOT NULL REFERENCES uir_module_snapshots(id), path_key TEXT NOT NULL, revision_id TEXT REFERENCES uir_source_revisions(id), PRIMARY KEY (snapshot_id, path_key))",
	"INSERT INTO uir_module_roots (id) VALUES ('root')",
	"INSERT INTO uir_module_locations (id, root_id, parent_location_id) VALUES ('parent', 'root', NULL), ('child', 'root', 'parent')",
	"INSERT INTO uir_module_snapshots (id, location_id, base_snapshot_id, state) VALUES ('base', 'parent', NULL, 'ready'), ('next', 'child', 'base', 'ready')",
	"INSERT INTO uir_module_location_heads (location_id, snapshot_id) VALUES ('child', 'next')",
	"INSERT INTO uir_module_primaries (root_id, location_id) VALUES ('root', 'parent')",
	"INSERT INTO uir_source_revisions (id, root_id, projection) VALUES ('revision', 'root', '{}')",
	"INSERT INTO uir_source_deltas (snapshot_id, path_key, revision_id) VALUES ('next', 'main.go', 'revision')",
}

func createPreviousGeneration(database *gorm.DB) {
	GinkgoHelper()
	for _, statement := range previousGenerationDDL {
		Expect(database.Exec(statement).Error).To(Succeed(), statement)
	}
}

var _ = Describe("previous-generation module cutover", func() {
	cutoverSQLite := func() storage.DBOptions {
		return storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "generation.db")}
	}
	cutoverPostgres := func() storage.DBOptions {
		return storage.DBOptions{DSN: dbtest.ForGinkgo(dbtest.Options{Name: "uir_generation_cutover"}).DSN(), Schema: "uir_generation_cutover"}
	}

	DescribeTable("drops the uir_-prefixed module tables and keeps the base schema and unmanaged tables",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			database := openDB(ctx, config)
			createPreviousGeneration(database)
			Expect(database.Exec("CREATE TABLE external_sentinel (id TEXT PRIMARY KEY)").Error).To(Succeed())

			cutover := openDB(ctx, config)
			for _, name := range previousGenerationTables {
				Expect(cutover.Migrator().HasTable(name)).To(BeFalse(), name)
			}
			assertModuleSchema(cutover)
			Expect(cutover.Migrator().HasTable("external_sentinel")).To(BeTrue())
			root := storage.ModuleRoot{ID: uuid.New(), RootKey: "example.org/rebuilt", Name: "rebuilt", CreatedAt: time.Now().UTC()}
			Expect(cutover.Create(&root).Error).To(Succeed())
		},
		Entry("SQLite", cutoverSQLite),
		Entry("PostgreSQL", cutoverPostgres),
	)

	DescribeTable("refuses the whole cutover when an unmanaged table references a previous-generation table",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			database := openDB(ctx, config)
			createPreviousGeneration(database)
			Expect(database.Exec("CREATE TABLE external_reference (id TEXT PRIMARY KEY, root_id TEXT REFERENCES uir_module_roots(id))").Error).To(Succeed())

			_, err := storage.UirDB(context.Background(), config)
			Expect(err).To(MatchError(ContainSubstring("uir_module_roots")))
			for _, name := range previousGenerationTables {
				Expect(database.Migrator().HasTable(name)).To(BeTrue(), name)
			}
		},
		Entry("SQLite", cutoverSQLite),
		Entry("PostgreSQL", cutoverPostgres),
	)
})
