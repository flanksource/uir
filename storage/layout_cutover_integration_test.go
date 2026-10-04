package storage_test

import (
	"context"
	"embed"
	"time"

	commonsdb "github.com/flanksource/commons-db/db"
	commonsmigrate "github.com/flanksource/commons-db/migrate"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// h64aMigrations is the schema 04–09 as the last release before H64b declared it: the symbol handle
// packed kind into 3 bits and local into 32, symbols.kind was checked against the eight Go kinds, and
// there was no symbol_kinds or symbol_layout.
//
//go:embed testdata/h64a/*.hcl
var h64aMigrations embed.FS

// h64aIndexTables are the H64a index tables the cutover empties; h64aKeptTables keep their rows.
var (
	h64aIndexTables = []string{
		"symbol_deltas", "symbol_postings", "snapshot_dependencies", "package_coverage", "documents", "symbols", "symbol_packages",
		"symbol_modules", "source_deltas", "source_revisions", "location_heads", "snapshots", "source_blobs",
	}
	h64aKeptTables = []string{"modules", "locations", "primary_locations", "task_runs"}
)

// h64aHandle packs a handle in the H64a layout: 0 | module 11 | package 16 | visibility 1 | kind 3 | local 32.
func h64aHandle(module, pkg, visibility, kind, local int64) int64 {
	return module<<52 | pkg<<36 | visibility<<35 | kind<<32 | local
}

// openH64aDatabase builds a database with the H64a schema: two registered roots, the legacy root with
// both checkouts, a finished task run, and one row in every index table.
func openH64aDatabase(ctx context.Context, options storage.DBOptions, checkouts preHandleCheckouts) *gorm.DB {
	GinkgoHelper()
	migration := []commonsmigrate.Option{commonsmigrate.WithDir("testdata/h64a"), commonsmigrate.WithName("uir"), commonsmigrate.WithRebuilds()}
	connection := options.DSN
	if options.Schema != "" {
		migration = append(migration, commonsmigrate.WithSchema(options.Schema))
		var err error
		connection, err = commonsmigrate.ConnectionForSchema(options.DSN, options.Schema)
		Expect(err).ToNot(HaveOccurred())
	}
	Expect(commonsmigrate.Apply(ctx, options.DSN, h64aMigrations, migration...)).To(Succeed())
	database, err := commonsdb.NewGorm(connection, commonsdb.DefaultGormConfig())
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		sqlDB, dbErr := database.DB()
		Expect(dbErr).ToNot(HaveOccurred())
		Expect(sqlDB.Close()).To(Succeed())
	})
	populateH64a(database, checkouts)
	return database
}

func populateH64a(database *gorm.DB, checkouts preHandleCheckouts) {
	GinkgoHelper()
	now := time.Now().UTC().Truncate(time.Second)
	root, zeta := uuid.New(), uuid.New()
	primary, secondary, zetaLocation := uuid.New(), uuid.New(), uuid.New()
	snapshot, revision, document := uuid.New(), uuid.New(), uuid.New()
	const canonicalKey = legacyModule + ".Main"
	symbol, handle := digest(canonicalKey), h64aHandle(2, 0, 1, 2, 0)
	location := "INSERT INTO locations (id, root_id, canonical_path, mount_path, kind, created_at) VALUES (?, ?, ?, ?, ?, ?)"
	for _, row := range [][]any{
		{"INSERT INTO modules (id, root_key, name, created_at, ordinal) VALUES (?, ?, ?, ?, ?)", zeta, "example.org/zeta", "zeta", now, 1},
		{"INSERT INTO modules (id, root_key, name, created_at, ordinal) VALUES (?, ?, ?, ?, ?)", root, legacyModule, "legacy", now, 2},
		{location, primary, root, checkouts.primary, "", "module", now},
		{location, secondary, root, checkouts.secondary, "", "module", now},
		{location, zetaLocation, zeta, "/workspace/zeta", "", "git", now},
		{"INSERT INTO primary_locations (root_id, location_id) VALUES (?, ?)", root, primary},
		{"INSERT INTO primary_locations (root_id, location_id) VALUES (?, ?)", zeta, zetaLocation},
		{"INSERT INTO task_runs (id, name, kind, status, labels, error, snapshot_ids, meta, snapshots, saved_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			"run-1", "index", "index", "success", storage.JSON(`{}`), "", storage.JSON(`[]`), storage.JSON(`{}`), storage.JSON(`[]`), now},
		{"INSERT INTO snapshots (id, root_id, location_id, revision, worktree_state, content_set_hash, configuration_hash, context_hash, coverage, package_count, diagnostics, started_at, completed_at, ordinal, reason) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			snapshot, root, primary, "", "unknown", digest("content"), digest("configuration"), digest("context"), "indexed", 1, storage.JSON(`[]`), now, now, 1, "add"},
		{"INSERT INTO location_heads (root_id, location_id, snapshot_id, version) VALUES (?, ?, ?, ?)", root, primary, snapshot, 1},
		{"INSERT INTO snapshot_dependencies (snapshot_id, module_path, declared_version, indirect, replace_path, replace_version, selected_version, unresolved_reason) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			snapshot, "example.org/dependency", "v1.0.0", false, "", "", "v1.0.0", "not indexed"},
		{"INSERT INTO source_blobs (content_hash, content) VALUES (?, ?)", digest("legacy.go"), []byte("package legacy\n")},
		{"INSERT INTO source_revisions (id, root_id, path_key, content_hash, package_path, size_bytes) VALUES (?, ?, ?, ?, ?, ?)", revision, root, "legacy.go", digest("legacy.go"), legacyModule, 12},
		{"INSERT INTO source_deltas (snapshot_id, root_id, path_key, revision_id, operation) VALUES (?, ?, ?, ?, ?)", snapshot, root, "legacy.go", revision, "set"},
		{"INSERT INTO symbol_modules (number, module_key) VALUES (?, ?)", 2, legacyModule},
		{"INSERT INTO symbol_packages (module_number, number, package_path) VALUES (?, ?, ?)", 2, 0, legacyModule},
		{"INSERT INTO symbols (id, identity_version, canonical_key, module_key, package_path, kind, name, search_name, visibility, parameter_types, handle) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			symbol, 1, canonicalKey, legacyModule, legacyModule, "func", "Main", "main", "exported", storage.JSON(`[]`), handle},
		{"INSERT INTO documents (id, root_id, path_key, source_revision_id, package_path, input_hash, indexer_version, coverage, symbol_count, occurrence_count, content, ordinal) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			document, root, "legacy.go", revision, legacyModule, digest("input"), "h64a", "indexed", 1, 1, storage.JSON(`{"version":3}`), 1},
		{"INSERT INTO symbol_postings (document_ordinal, root_ordinal, symbol_handle, role, occurrence_count) VALUES (?, ?, ?, ?, ?)", 1, 2, handle, 0, 1},
		{"INSERT INTO symbol_deltas (snapshot_ordinal, root_ordinal, symbol_handle, operation, shape_fp, body_fp) VALUES (?, ?, ?, ?, ?, ?)", 1, 2, handle, "set", 1, 2},
		{"INSERT INTO package_coverage (snapshot_id, root_id, package_path, input_hash, export_shape_hash, coverage, file_count, diagnostics) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			snapshot, root, legacyModule, digest("input"), digest("shape"), "indexed", 1, storage.JSON(`[]`)},
	} {
		Expect(database.Exec(row[0].(string), row[1:]...).Error).To(Succeed(), row[0])
	}
}

// handleLayout is the layout a database records.
func handleLayout(database *gorm.DB) string {
	GinkgoHelper()
	var layouts []string
	Expect(database.Model(&storage.SymbolLayout{}).Pluck("layout", &layouts).Error).To(Succeed())
	Expect(layouts).To(HaveLen(1))
	return layouts[0]
}

var _ = Describe("H64a layout cutover", func() {
	DescribeTable("discards the H64a index, keeps registrations and task runs, records H64b, and reindexes",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			checkouts := newPreHandleCheckouts()
			legacy := openH64aDatabase(ctx, config, checkouts)
			Expect(rowCounts(legacy, h64aIndexTables)).To(HaveEach(Equal(int64(1))))

			cutover := openDB(ctx, config)
			assertModuleSchema(cutover)
			Expect(rowCounts(cutover, h64aIndexTables)).To(HaveEach(BeZero()))
			Expect(rowCounts(cutover, h64aKeptTables)).To(Equal(map[string]int64{"modules": 2, "locations": 3, "primary_locations": 2, "task_runs": 1}))
			Expect(rootOrdinals(cutover)).To(Equal(map[string]int32{"example.org/zeta": 1, legacyModule: 2}), "registered roots keep their ordinals")
			Expect(handleLayout(cutover)).To(Equal(symbolhandle.Layout))
			Expect(storedKinds(cutover)).To(HaveLen(int(symbolhandle.MaxBuiltinKind)))
			if cutover.Name() == "sqlite" {
				var foreignKeys int
				Expect(cutover.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error).To(Succeed())
				Expect(foreignKeys).To(Equal(1), "the cutover restores foreign key enforcement it suspends for the drop")
			}

			Expect(reindex(ctx, cutover, checkouts.primary)).To(And(HaveField("ParsedFiles", 1), HaveField("HeadVersion", int64(1)), HaveField("Unchanged", false)))
			var symbol storage.Symbol
			Expect(cutover.Where("package_path = ? AND name = ?", legacyModule, "Main").First(&symbol).Error).To(Succeed())
			fields, err := symbolhandle.Unpack(symbol.Handle)
			Expect(err).ToNot(HaveOccurred())
			Expect(fields.Kind).To(Equal(symbolhandle.KindFunc), "reindex packs H64b handles")

			reindexed := rowCounts(cutover, append(append([]string{}, h64aIndexTables...), h64aKeptTables...))
			Expect(reindexed).To(HaveKeyWithValue("snapshots", int64(1)))
			Expect(rowCounts(openDB(ctx, config), append(append([]string{}, h64aIndexTables...), h64aKeptTables...))).To(Equal(reindexed),
				"reopening an H64b database discards nothing")
		},
		Entry("SQLite", sqliteOptions("h64a.db")),
		Entry("PostgreSQL", postgresOptions("uir_h64a_cutover")),
	)

	DescribeTable("refuses the whole cutover when an unmanaged table references a discarded H64a table",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			legacy := openH64aDatabase(ctx, config, newPreHandleCheckouts())
			Expect(legacy.Exec("CREATE TABLE external_reference (id TEXT PRIMARY KEY, symbol_id TEXT REFERENCES symbols(id))").Error).To(Succeed())

			database, err := storage.UirDB(ctx, config)
			Expect(database).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("H64a table symbols")))
			Expect(rowCounts(legacy, h64aIndexTables)).To(HaveEach(Equal(int64(1))))
			Expect(legacy.Migrator().HasTable("symbol_layout")).To(BeFalse(), "the refused cutover left the H64a schema unmigrated")
		},
		Entry("SQLite", sqliteOptions("h64a-referenced.db")),
		Entry("PostgreSQL", postgresOptions("uir_h64a_refused")),
	)

	DescribeTable("refuses to open an H64a database read-only",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			openH64aDatabase(ctx, config, newPreHandleCheckouts())
			database, err := storage.OpenReadOnly(ctx, config)
			Expect(database).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("database predates symbol handle layout H64b; open it read-write once or run uir reindex")))
		},
		Entry("SQLite", sqliteOptions("h64a-readonly.db")),
		Entry("PostgreSQL", postgresOptions("uir_h64a_readonly")),
	)

	DescribeTable("refuses a database that records a layout this version does not write",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			database := openDB(ctx, config)
			Expect(database.Model(&storage.SymbolLayout{}).Where("id = 1").Update("layout", "H64z").Error).To(Succeed())

			_, err := storage.UirDB(ctx, config)
			Expect(err).To(MatchError(ContainSubstring(`database records symbol handle layout "H64z"; this uir writes "H64b"`)))
			_, err = storage.OpenReadOnly(ctx, config)
			Expect(err).To(MatchError(ContainSubstring(`database records symbol handle layout "H64z"; this uir writes "H64b"`)))
		},
		Entry("SQLite", sqliteOptions("layout-unknown.db")),
		Entry("PostgreSQL", postgresOptions("uir_layout_unknown")),
	)
})
