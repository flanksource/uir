package storage_test

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

var _ = Describe("UirDB", func() {
	DescribeTable("rejects invalid configuration",
		func(options storage.DBOptions, message string) {
			database, err := storage.UirDB(context.Background(), options)
			Expect(database).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("a missing DSN", storage.DBOptions{}, "DSN is required"),
		Entry("an unsupported DSN scheme", storage.DBOptions{DSN: "mysql://localhost/uir"}, `unsupported database scheme "mysql"`),
		Entry("a SQLite schema", storage.DBOptions{DSN: "sqlite://state/uir.db", Schema: "main"}, "do not support schema"),
	)

	It("initializes SQLite twice with enforced foreign keys", func(ctx SpecContext) {
		options := storage.DBOptions{DSN: "sqlite://" + filepath.Join(GinkgoT().TempDir(), "uir.db")}
		for range 2 {
			database := openDB(ctx, options)
			Expect(database.Dialector.Name()).To(Equal("sqlite"))
			assertModuleSchema(database)
			var foreignKeys, busyTimeout int
			var journalMode string
			Expect(database.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error).To(Succeed())
			Expect(database.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error).To(Succeed())
			Expect(database.Raw("PRAGMA journal_mode").Scan(&journalMode).Error).To(Succeed())
			Expect(foreignKeys).To(Equal(1))
			Expect(busyTimeout).To(Equal(5000))
			Expect(strings.ToLower(journalMode)).To(Equal("wal"))
		}
	})

	It("initializes PostgreSQL twice in the selected schema", func(ctx SpecContext) {
		options := storage.DBOptions{DSN: dbtest.ForGinkgo(dbtest.Options{Name: "uir_storage"}).DSN(), Schema: "uir_storage"}
		for range 2 {
			database := openDB(ctx, options)
			Expect(database.Dialector.Name()).To(Equal("postgres"))
			var currentSchema string
			Expect(database.Raw("SELECT current_schema()").Scan(&currentSchema).Error).To(Succeed())
			Expect(currentSchema).To(Equal(options.Schema))
			assertModuleSchema(database)
		}
	})
})

func openDB(ctx context.Context, options storage.DBOptions) *gorm.DB {
	GinkgoHelper()
	database, err := storage.UirDB(ctx, options)
	Expect(err).To(Succeed())
	DeferCleanup(func() {
		sqlDB, dbErr := database.DB()
		Expect(dbErr).To(Succeed())
		Expect(sqlDB.Close()).To(Succeed())
	})
	return database
}
