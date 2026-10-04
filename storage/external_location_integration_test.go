package storage_test

import (
	"context"
	"strings"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// narrowedCheck is a check external publication widened: its SQLite definition text now and before, the
// value it added, and its previous expression.
type narrowedCheck struct{ table, name, current, previous, added, expression string }

var narrowedChecks = []narrowedCheck{
	{"locations", "locations_kind_check", "'git-submodule', 'external')", "'git-submodule')", "'external'",
		"kind IN ('module', 'git', 'git-submodule')"},
	{"snapshots", "snapshots_reason_check", "'dependency-cycle', 'import')", "'dependency-cycle')", "'import'",
		"reason IN ('unknown', 'add', 'reindex', 'refactor', 'local-dependency', 'versioned-dependency', 'historical', 'dependency-cycle')"},
}

// narrowChecks puts the two checks back to what an earlier uir created, without touching any row: on
// SQLite through the table definitions in sqlite_master, which the next connection reads, on PostgreSQL
// by replacing the constraints.
func narrowChecks(database *gorm.DB) {
	GinkgoHelper()
	for _, check := range narrowedChecks {
		if database.Name() != "sqlite" {
			var definition string
			Expect(database.Raw("SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = ?", check.name).Scan(&definition).Error).To(Succeed())
			Expect(definition).To(ContainSubstring(check.added), check.name)
			Expect(database.Exec("ALTER TABLE " + check.table + " DROP CONSTRAINT " + check.name).Error).To(Succeed())
			Expect(database.Exec("ALTER TABLE " + check.table + " ADD CONSTRAINT " + check.name + " CHECK (" + check.expression + ")").Error).To(Succeed())
			continue
		}
		var definition string
		Expect(database.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", check.table).Scan(&definition).Error).To(Succeed())
		Expect(definition).To(ContainSubstring(check.current), check.name)
		Expect(database.Exec("PRAGMA writable_schema = ON").Error).To(Succeed())
		Expect(database.Exec("UPDATE sqlite_master SET sql = ? WHERE type = 'table' AND name = ?",
			strings.Replace(definition, check.current, check.previous, 1), check.table).Error).To(Succeed())
		Expect(database.Exec("PRAGMA writable_schema = OFF").Error).To(Succeed())
	}
}

func closeStorageDB(database *gorm.DB) {
	GinkgoHelper()
	sqlDB, err := database.DB()
	Expect(err).To(Succeed())
	Expect(sqlDB.Close()).To(Succeed())
}

var _ = Describe("external publication storage", func() {
	DescribeTable("widens the location kind and snapshot reason checks of an existing index, keeping its rows",
		func(ctx context.Context, options func() storage.DBOptions) {
			config := options()
			database, err := storage.UirDB(ctx, config)
			Expect(err).To(Succeed())
			fixture := newModuleFixture(database, "/workspace/service")
			snapshot := publishedSnapshot(fixture.location, nil, "main", 0, fixture.now)
			Expect(database.Create(&snapshot).Error).To(Succeed())
			Expect(database.Create(&storage.ModulePrimary{RootID: fixture.root.ID, LocationID: fixture.location.ID}).Error).To(Succeed())
			Expect(database.Create(&storage.ModuleLocationHead{RootID: fixture.root.ID, LocationID: fixture.location.ID, SnapshotID: snapshot.ID, Version: 1}).Error).To(Succeed())
			narrowChecks(database)
			kept := []string{"modules", "locations", "primary_locations", "snapshots", "location_heads"}
			before := rowCounts(database, kept)
			closeStorageDB(database)

			upgraded := openDB(ctx, config)
			Expect(rowCounts(upgraded, kept)).To(Equal(before))
			now := time.Now().UTC()
			external := storage.ModuleLocation{ID: uuid.New(), RootID: fixture.root.ID, CanonicalPath: "acme://rules/service", Kind: storage.LocationExternal, CreatedAt: now}
			Expect(upgraded.Create(&external).Error).To(Succeed(), "an external location is accepted")
			imported := publishedSnapshot(external, nil, "rules-1", 0, fixture.now)
			imported.Reason = storage.ReasonImport
			Expect(upgraded.Create(&imported).Error).To(Succeed(), "an import snapshot is accepted")
			unknown := storage.ModuleLocation{ID: uuid.New(), RootID: fixture.root.ID, CanonicalPath: "/elsewhere", Kind: "remote", CreatedAt: now}
			Expect(upgraded.Create(&unknown).Error).To(HaveOccurred(), "the widened check still refuses an unknown kind")
		},
		Entry("SQLite", sqliteOptions("widen-external.db")),
		Entry("PostgreSQL", postgresOptions("uir_widen_external")),
	)
})
