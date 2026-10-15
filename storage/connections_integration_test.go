package storage_test

import (
	"path/filepath"

	"github.com/flanksource/commons-db/models"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("connection and database module storage", func() {
	It("keeps one connection for multiple database modules and blocks deletion until they are removed", func(ctx SpecContext) {
		database := openDB(ctx, storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "connections.db")})
		connection := models.Connection{Name: "Warehouse", Type: models.ConnectionTypePostgres, URL: "postgres://db.example/", Password: "op://vault/item/password"}
		Expect(storage.CreateConnection(ctx, database, &connection)).To(Succeed())
		first := storage.DatabaseModule{ConnectionID: connection.ID, DatabaseName: "orders", Name: "Orders"}
		second := storage.DatabaseModule{ConnectionID: connection.ID, DatabaseName: "billing", Name: "Billing"}
		Expect(storage.CreateDatabaseModule(ctx, database, &first)).To(Succeed())
		Expect(storage.CreateDatabaseModule(ctx, database, &second)).To(Succeed())
		Expect(first.RootKey).To(Equal("database/" + first.ID.String()))
		loaded, err := storage.GetConnection(ctx, database, connection.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(loaded.Password).To(Equal(connection.Password))
		modules, err := storage.ListDatabaseModules(ctx, database)
		Expect(err).ToNot(HaveOccurred())
		Expect(modules).To(HaveLen(2))
		Expect(storage.DeleteConnection(ctx, database, connection.ID)).To(MatchError(ContainSubstring("2 database modules")))
		Expect(storage.DeleteDatabaseModule(ctx, database, first.ID)).To(Succeed())
		Expect(storage.DeleteDatabaseModule(ctx, database, second.ID)).To(Succeed())
		Expect(storage.DeleteConnection(ctx, database, connection.ID)).To(Succeed())
	})
})
