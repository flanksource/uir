package databasecatalog_test

import (
	"database/sql"
	"path/filepath"

	"github.com/flanksource/commons-db/models"
	"github.com/flanksource/uir/databasecatalog"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("database captures", func() {
	It("publishes successive SQLite schema snapshots and diffs their exact column types", func(ctx SpecContext) {
		root := GinkgoT().TempDir()
		sourcePath := filepath.Join(root, "source.db")
		source, err := sql.Open("sqlite", sourcePath)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(source.Close)
		_, err = source.ExecContext(ctx, "CREATE TABLE orders (id INTEGER PRIMARY KEY, name TEXT NOT NULL)")
		Expect(err).ToNot(HaveOccurred())
		index, err := storage.UirDB(ctx, storage.DBOptions{DSN: filepath.Join(root, "index.db")})
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { pool, err := index.DB(); Expect(err).ToNot(HaveOccurred()); Expect(pool.Close()).To(Succeed()) })
		connection := models.Connection{Name: "Local", Type: models.ConnectionTypeSQLite, URL: sourcePath}
		Expect(storage.CreateConnection(ctx, index, &connection)).To(Succeed())
		module := storage.DatabaseModule{ConnectionID: connection.ID, DatabaseName: "snapshot", Name: "Orders"}
		Expect(storage.CreateDatabaseModule(ctx, index, &module)).To(Succeed())
		first, err := databasecatalog.Capture(ctx, index, module)
		Expect(err).ToNot(HaveOccurred())
		Expect(first.SnapshotID).ToNot(BeEmpty())
		_, err = source.ExecContext(ctx, "ALTER TABLE orders ADD COLUMN amount DECIMAL(10,2)")
		Expect(err).ToNot(HaveOccurred())
		second, err := databasecatalog.Capture(ctx, index, module)
		Expect(err).ToNot(HaveOccurred())
		Expect(second.SnapshotID).ToNot(Equal(first.SnapshotID))
		result, err := databasecatalog.Diff(ctx, index, first.SnapshotID, second.SnapshotID, "", "")
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Summary.Added).To(Equal(1))
		Expect(result.Summary.Deleted).To(Equal(0))
		Expect(storage.DeleteDatabaseModule(ctx, index, module.ID)).To(Succeed())
		var snapshots int64
		Expect(index.Model(&storage.ModuleSnapshot{}).Count(&snapshots).Error).To(Succeed())
		Expect(snapshots).To(BeZero())
	})
})
