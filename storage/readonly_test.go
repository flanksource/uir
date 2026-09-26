package storage_test

import (
	"path/filepath"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("opening an existing index read-only", func() {
	It("does not create a missing SQLite database and refuses writes", func(ctx SpecContext) {
		path := filepath.Join(GinkgoT().TempDir(), "index.db")
		_, err := storage.OpenReadOnly(ctx, storage.DBOptions{DSN: path})
		Expect(err).To(HaveOccurred())
		writer, err := storage.UirDB(ctx, storage.DBOptions{DSN: path})
		Expect(err).ToNot(HaveOccurred())
		connection, err := writer.DB()
		Expect(err).ToNot(HaveOccurred())
		Expect(connection.Close()).To(Succeed())
		reader, err := storage.OpenReadOnly(ctx, storage.DBOptions{DSN: path})
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			sqlDB, dbErr := reader.DB()
			Expect(dbErr).ToNot(HaveOccurred())
			Expect(sqlDB.Close()).To(Succeed())
		})
		Expect(reader.Exec("INSERT INTO modules (id, root_key, name) VALUES (?, ?, ?)",
			"00000000-0000-0000-0000-000000000001", "example.org/write", "write").Error).To(HaveOccurred())
	})
})
