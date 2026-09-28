package storage_test

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"time"

	commonsdb "github.com/flanksource/commons-db/db"
	commonsmigrate "github.com/flanksource/commons-db/migrate"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// preHandleMigrations is the schema of 04–06 as the last release before compact symbol handles declared
// it: no modules, snapshots, or documents ordinal, no symbols handle, and symbol_postings keyed by
// document uuid, symbol id, and role name.
//
//go:embed testdata/prehandle/*.hcl
var preHandleMigrations embed.FS

// preHandleIndexTables are the pre-handle tables the cutover empties; the registration tables keep
// their rows.
var (
	preHandleIndexTables = []string{
		"snapshots", "location_heads", "source_revisions", "source_deltas", "symbols", "documents", "symbol_postings", "package_coverage",
	}
	preHandleRegistrationTables = []string{"modules", "locations", "primary_locations"}
)

const legacyModule = "example.org/legacy"

// preHandleCheckouts are two checkouts of legacyModule on disk, registered in the pre-handle database:
// primary is the root's primary location with a published head, secondary a second location without one.
type preHandleCheckouts struct{ primary, secondary string }

func newPreHandleCheckouts() preHandleCheckouts {
	GinkgoHelper()
	return preHandleCheckouts{primary: goModuleCheckout(), secondary: goModuleCheckout()}
}

func goModuleCheckout() string {
	GinkgoHelper()
	directory, err := filepath.EvalSymlinks(GinkgoT().TempDir())
	Expect(err).ToNot(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module "+legacyModule+"\n\ngo 1.26\n"), 0o600)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(directory, "legacy.go"), []byte("package legacy\n\n// Main is indexed again after the cutover.\nfunc Main() {}\n"), 0o600)).To(Succeed())
	return directory
}

// openPreHandleDatabase builds a database with the pre-handle schema: three registered roots, the
// legacy root with both checkouts, and one row in every index table.
func openPreHandleDatabase(ctx context.Context, options storage.DBOptions, checkouts preHandleCheckouts) *gorm.DB {
	GinkgoHelper()
	migration := []commonsmigrate.Option{commonsmigrate.WithDir("testdata/prehandle"), commonsmigrate.WithName("uir")}
	connection := options.DSN
	if options.Schema != "" {
		migration = append(migration, commonsmigrate.WithSchema(options.Schema))
		var err error
		connection, err = commonsmigrate.ConnectionForSchema(options.DSN, options.Schema)
		Expect(err).ToNot(HaveOccurred())
	}
	Expect(commonsmigrate.Apply(ctx, options.DSN, preHandleMigrations, migration...)).To(Succeed())
	database, err := commonsdb.NewGorm(connection, commonsdb.DefaultGormConfig())
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		sqlDB, dbErr := database.DB()
		Expect(dbErr).ToNot(HaveOccurred())
		Expect(sqlDB.Close()).To(Succeed())
	})
	populatePreHandle(database, checkouts)
	return database
}

func populatePreHandle(database *gorm.DB, checkouts preHandleCheckouts) {
	GinkgoHelper()
	now := time.Now().UTC().Truncate(time.Second)
	root, zeta, alpha := uuid.New(), uuid.New(), uuid.New()
	primary, secondary, zetaLocation := uuid.New(), uuid.New(), uuid.New()
	snapshot, revision, document := uuid.New(), uuid.New(), uuid.New()
	const canonicalKey = legacyModule + ".Main"
	symbol := digest(canonicalKey)
	location := "INSERT INTO locations (id, root_id, canonical_path, mount_path, kind, created_at) VALUES (?, ?, ?, ?, ?, ?)"
	for _, row := range [][]any{
		{"INSERT INTO modules (id, root_key, name, created_at) VALUES (?, ?, ?, ?)", zeta, "example.org/zeta", "zeta", now.Add(-time.Hour)},
		{"INSERT INTO modules (id, root_key, name, created_at) VALUES (?, ?, ?, ?)", root, legacyModule, "legacy", now},
		{"INSERT INTO modules (id, root_key, name, created_at) VALUES (?, ?, ?, ?)", alpha, "example.org/alpha", "alpha", now},
		{location, primary, root, checkouts.primary, "", "module", now},
		{location, secondary, root, checkouts.secondary, "", "module", now},
		{location, zetaLocation, zeta, "/workspace/zeta", "", "git", now},
		{"INSERT INTO primary_locations (root_id, location_id) VALUES (?, ?)", root, primary},
		{"INSERT INTO primary_locations (root_id, location_id) VALUES (?, ?)", zeta, zetaLocation},
		{"INSERT INTO snapshots (id, root_id, location_id, revision, worktree_state, content_set_hash, configuration_hash, context_hash, coverage, package_count, diagnostics, started_at, completed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			snapshot, root, primary, "", "unknown", digest("content"), digest("configuration"), digest("context"), "indexed", 1, storage.JSON(`[]`), now, now},
		{"INSERT INTO location_heads (root_id, location_id, snapshot_id, version) VALUES (?, ?, ?, ?)", root, primary, snapshot, 1},
		{"INSERT INTO source_revisions (id, root_id, path_key, content_hash, package_path, size_bytes) VALUES (?, ?, ?, ?, ?, ?)", revision, root, "legacy.go", digest("legacy.go"), legacyModule, 12},
		{"INSERT INTO source_deltas (snapshot_id, root_id, path_key, revision_id, operation) VALUES (?, ?, ?, ?, ?)", snapshot, root, "legacy.go", revision, "set"},
		{"INSERT INTO symbols (id, identity_version, canonical_key, module_key, package_path, kind, name, search_name, visibility, parameter_types) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			symbol, 1, canonicalKey, legacyModule, legacyModule, "func", "Main", "main", "exported", storage.JSON(`[]`)},
		{"INSERT INTO documents (id, root_id, path_key, source_revision_id, package_path, input_hash, indexer_version, coverage, symbol_count, occurrence_count, content) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			document, root, "legacy.go", revision, legacyModule, digest("input"), "legacy", "indexed", 1, 1, storage.JSON(`{"version":1}`)},
		{"INSERT INTO symbol_postings (document_id, root_id, symbol_id, role, occurrence_count) VALUES (?, ?, ?, ?, ?)", document, root, symbol, "definition", 1},
		{"INSERT INTO package_coverage (snapshot_id, root_id, package_path, input_hash, export_shape_hash, coverage, file_count, diagnostics) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			snapshot, root, legacyModule, digest("input"), digest("shape"), "indexed", 1, storage.JSON(`[]`)},
	} {
		Expect(database.Exec(row[0].(string), row[1:]...).Error).To(Succeed(), row[0])
	}
}

func rowCounts(database *gorm.DB, tables []string) map[string]int64 {
	GinkgoHelper()
	counts := make(map[string]int64, len(tables))
	for _, table := range tables {
		var count int64
		Expect(database.Table(table).Count(&count).Error).To(Succeed(), table)
		counts[table] = count
	}
	return counts
}

// rootOrdinals maps each registered root key to its ordinal.
func rootOrdinals(database *gorm.DB) map[string]int32 {
	GinkgoHelper()
	var roots []storage.ModuleRoot
	Expect(database.Order("ordinal").Find(&roots).Error).To(Succeed())
	ordinals := make(map[string]int32, len(roots))
	for _, root := range roots {
		ordinals[root.RootKey] = root.Ordinal
	}
	return ordinals
}

// reindex runs `uir reindex` on one registered checkout and returns its one result.
func reindex(ctx context.Context, database *gorm.DB, checkout string) indexer.ModuleResult {
	GinkgoHelper()
	engine, err := indexer.New(database)
	Expect(err).ToNot(HaveOccurred())
	results, err := engine.IndexModules(ctx, indexer.ModuleOptions{Path: checkout, ExistingOnly: true})
	Expect(err).ToNot(HaveOccurred())
	Expect(results).To(HaveLen(1))
	return results[0]
}

var _ = Describe("pre-handle generation cutover", func() {
	DescribeTable("discards the pre-handle index, keeps registrations with dense ordinals, and reindexes them",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			checkouts := newPreHandleCheckouts()
			legacy := openPreHandleDatabase(ctx, config, checkouts)
			Expect(legacy.Exec("CREATE TABLE external_sentinel (id TEXT PRIMARY KEY)").Error).To(Succeed())
			Expect(legacy.Exec("INSERT INTO external_sentinel (id) VALUES (?)", "keep-me").Error).To(Succeed())

			cutover := openDB(ctx, config)
			assertModuleSchema(cutover)
			Expect(rowCounts(cutover, preHandleIndexTables)).To(HaveEach(BeZero()))
			Expect(rowCounts(cutover, preHandleRegistrationTables)).To(Equal(map[string]int64{"modules": 3, "locations": 3, "primary_locations": 2}))
			Expect(rootOrdinals(cutover)).To(Equal(map[string]int32{"example.org/zeta": 1, "example.org/alpha": 2, legacyModule: 3}),
				"ordinals follow created_at, then root_key")
			if cutover.Name() == "sqlite" {
				var foreignKeys int
				Expect(cutover.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error).To(Succeed())
				Expect(foreignKeys).To(Equal(1), "the cutover restores foreign key enforcement it suspends for the drop")
			}

			Expect(reindex(ctx, cutover, checkouts.secondary)).To(And(HaveField("ParsedFiles", 1), HaveField("HeadVersion", int64(1)), HaveField("Unchanged", false)),
				"a checkout whose primary has no head either is indexed in full")
			Expect(reindex(ctx, cutover, checkouts.primary)).To(And(HaveField("ReusedFiles", 1), HaveField("HeadVersion", int64(1)), HaveField("Unchanged", false)),
				"the primary publishes its first head, reusing the identical document the secondary stored")
			Expect(rowCounts(cutover, []string{"modules", "locations"})).To(Equal(map[string]int64{"modules": 3, "locations": 3}), "reindex registered nothing new")

			root := storage.ModuleRoot{ID: uuid.New(), RootKey: "example.org/rebuilt", Name: "rebuilt", CreatedAt: time.Now().UTC()}
			Expect(storage.CreateModuleRoot(ctx, cutover, &root)).To(Succeed())
			Expect(root.Ordinal).To(Equal(int32(4)))

			reopened := openDB(ctx, config)
			Expect(rowCounts(reopened, []string{"modules", "snapshots", "external_sentinel"})).To(Equal(map[string]int64{"modules": 4, "snapshots": 2, "external_sentinel": 1}),
				"reopening a current database discards nothing")
		},
		Entry("SQLite", sqliteOptions("prehandle.db")),
		Entry("PostgreSQL", postgresOptions("uir_prehandle_cutover")),
	)

	DescribeTable("refuses the whole cutover when an unmanaged table references a discarded pre-handle table",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			legacy := openPreHandleDatabase(ctx, config, newPreHandleCheckouts())
			Expect(legacy.Exec("CREATE TABLE external_reference (id TEXT PRIMARY KEY, symbol_id TEXT REFERENCES symbols(id))").Error).To(Succeed())

			database, err := storage.UirDB(ctx, config)
			Expect(database).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("pre-handle table symbols")))
			Expect(rowCounts(legacy, preHandleIndexTables)).To(HaveEach(Equal(int64(1))))
			Expect(legacy.Migrator().HasColumn("modules", "ordinal")).To(BeFalse(), "the refused cutover left the pre-handle schema unmigrated")
		},
		Entry("SQLite", sqliteOptions("prehandle-referenced.db")),
		Entry("PostgreSQL", postgresOptions("uir_prehandle_refused")),
	)

	DescribeTable("refuses to open a pre-handle database read-only",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			openPreHandleDatabase(ctx, config, newPreHandleCheckouts())
			database, err := storage.OpenReadOnly(ctx, config)
			Expect(database).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("database predates compact symbol handles; open it read-write once or run uir reindex")))
		},
		Entry("SQLite", sqliteOptions("prehandle-readonly.db")),
		Entry("PostgreSQL", postgresOptions("uir_prehandle_readonly")),
	)
})
