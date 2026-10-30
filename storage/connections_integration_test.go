package storage_test

import (
	dbcontext "github.com/flanksource/commons-db/context"
	"github.com/flanksource/commons-db/models"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("connection and database module storage", func() {
	DescribeTable("stores a commons-db connection the way its service writes one and lets the foreign key protect dependent modules",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			connection := models.Connection{Name: "Cache", Type: models.ConnectionTypeRedis, URL: "redis://cache.example:6379"}
			Expect(database.WithContext(ctx).Create(&connection).Error).To(Succeed())
			Expect(connection.ID).ToNot(Equal(uuid.Nil))
			module := storage.DatabaseModule{ConnectionID: connection.ID, DatabaseName: "orders", Name: "Orders"}
			Expect(storage.CreateDatabaseModule(ctx, database, &module)).To(Succeed())
			loaded, err := storage.GetConnection(ctx, database, connection.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(loaded.CreatedAt).ToNot(BeZero())
			Expect(database.WithContext(ctx).Delete(&models.Connection{}, "id = ?", connection.ID).Error).To(HaveOccurred())
			_, err = storage.GetConnection(ctx, database, connection.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(storage.DeleteDatabaseModule(ctx, database, module.ID)).To(Succeed())
			Expect(database.WithContext(ctx).Delete(&models.Connection{}, "id = ?", connection.ID).Error).To(Succeed())
		},
		Entry("SQLite", sqliteOptions("connections.db")),
		Entry("PostgreSQL", postgresOptions("uir_connections")),
	)

	DescribeTable("keeps the properties commons-db reads its runtime settings from",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			runtime := dbcontext.NewContext(ctx).WithDB(database, nil)
			Expect(dbcontext.UpdateProperty(runtime, "connection.health.timeout", "5s")).To(Succeed())
			Expect(dbcontext.UpdateProperty(runtime, "connection.health.timeout", "10s")).To(Succeed())
			Expect(runtime.Properties()).To(HaveKeyWithValue("connection.health.timeout", "10s"))
		},
		Entry("SQLite", sqliteOptions("properties.db")),
		Entry("PostgreSQL", postgresOptions("uir_properties")),
	)

	DescribeTable("upgrades a connections table that still restricts the type to the previous nine",
		func(ctx SpecContext, options func() storage.DBOptions, previous []string) {
			opened := options()
			database := openDB(ctx, opened)
			legacy := uuid.New()
			for _, statement := range previous {
				Expect(database.Exec(statement).Error).To(Succeed(), statement)
			}
			Expect(database.Exec(`INSERT INTO connections (id, name, namespace, type, url, username, password, certificate, insecure_tls, created_at, updated_at)
				VALUES (?, 'warehouse', '', 'postgres', 'postgres://db.example/', '', '', '', false, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, legacy).Error).To(Succeed())
			Expect(database.Create(&models.Connection{Name: "Cache", Type: models.ConnectionTypeRedis, URL: "redis://cache.example:6379"}).Error).To(HaveOccurred())

			upgraded := openDB(ctx, opened)
			Expect(upgraded.WithContext(ctx).Create(&models.Connection{Name: "Cache", Type: models.ConnectionTypeRedis, URL: "redis://cache.example:6379"}).Error).To(Succeed())
			kept, err := storage.GetConnection(ctx, upgraded, legacy)
			Expect(err).ToNot(HaveOccurred())
			Expect(kept.Name).To(Equal("warehouse"))
		},
		Entry("SQLite", sqliteOptions("previous-connections.db"), []string{
			"PRAGMA foreign_keys = OFF",
			"CREATE TABLE `previous_connections` (`id` text NOT NULL, `name` text NOT NULL, `namespace` text NOT NULL, `type` text NOT NULL, `url` text NOT NULL, " +
				"`username` text NOT NULL, `password` text NOT NULL, `certificate` text NOT NULL, `properties` text NULL, `insecure_tls` boolean NOT NULL, " +
				"`created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, PRIMARY KEY (`id`), CONSTRAINT `connections_name_check` CHECK (length(name) > 0), " +
				"CONSTRAINT `connections_type_check` CHECK (type IN ('git', 'github', 'azure_devops', 'gitlab', 'postgres', 'sql_server', 'sqlite', 's3', 'mysql')), " +
				"CONSTRAINT `connections_properties_json` CHECK (json_valid(\"properties\")))",
			"DROP TABLE connections",
			"ALTER TABLE `previous_connections` RENAME TO `connections`",
			"CREATE UNIQUE INDEX `connections_namespace_name_key` ON `connections` (`namespace`, `name`)",
			"PRAGMA foreign_keys = ON",
		}),
		Entry("PostgreSQL", postgresOptions("uir_previous_connections"), []string{
			"ALTER TABLE connections ALTER COLUMN id DROP DEFAULT, ALTER COLUMN created_at DROP DEFAULT, ALTER COLUMN updated_at DROP DEFAULT, DROP COLUMN source, DROP COLUMN created_by",
			"ALTER TABLE connections ADD CONSTRAINT connections_type_check CHECK (type IN ('git', 'github', 'azure_devops', 'gitlab', 'postgres', 'sql_server', 'sqlite', 's3', 'mysql'))",
		}),
	)
})
