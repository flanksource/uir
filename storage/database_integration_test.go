package storage_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

var tableNames = []string{
	"uir_projects",
	"uir_snapshots",
	"uir_project_heads",
	"uir_roots",
	"uir_sources",
	"uir_nodes",
	"uir_node_locations",
	"uir_fields",
	"uir_relationships",
}

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

	Context("with SQLite", func() {
		It("accepts an explicit sqlite URL", func(ctx SpecContext) {
			database := openDB(ctx, storage.DBOptions{DSN: "sqlite://" + filepath.Join(GinkgoT().TempDir(), "uir.sqlite")})
			Expect(database.Dialector.Name()).To(Equal("sqlite"))
			Expect(database.Migrator().HasTable(&storage.Project{})).To(BeTrue())
		})

		It("applies the HCL schema idempotently with enforced foreign keys", func(ctx SpecContext) {
			options := storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "uir.db")}
			database := openDB(ctx, options)
			Expect(database.Dialector.Name()).To(Equal("sqlite"))
			assertSchema(database)

			var foreignKeys, busyTimeout int
			var journalMode string
			Expect(database.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error).ToNot(HaveOccurred())
			Expect(database.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error).ToNot(HaveOccurred())
			Expect(database.Raw("PRAGMA journal_mode").Scan(&journalMode).Error).ToNot(HaveOccurred())
			Expect(map[string]any{
				"foreign_keys": foreignKeys,
				"busy_timeout": busyTimeout,
				"journal_mode": strings.ToLower(journalMode),
			}).To(Equal(map[string]any{
				"foreign_keys": 1,
				"busy_timeout": 5000,
				"journal_mode": "wal",
			}))

			second := openDB(ctx, options)
			assertSchema(second)
		})
	})

	Context("with PostgreSQL", func() {
		It("applies the HCL schema idempotently in the selected schema", func(ctx SpecContext) {
			testDatabase := dbtest.ForGinkgo(dbtest.Options{Name: "uir_storage"})
			options := storage.DBOptions{
				DSN:    testDatabase.DSN(),
				Schema: "uir_storage",
			}
			database := openDB(ctx, options)
			Expect(database.Dialector.Name()).To(Equal("postgres"))

			var currentSchema string
			Expect(database.Raw("SELECT current_schema()").Scan(&currentSchema).Error).ToNot(HaveOccurred())
			Expect(currentSchema).To(Equal(options.Schema))
			assertSchema(database)

			second := openDB(ctx, options)
			assertSchema(second)
		})
	})
})

func openDB(ctx context.Context, options storage.DBOptions) *gorm.DB {
	GinkgoHelper()
	database, err := storage.UirDB(ctx, options)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		sqlDB, dbErr := database.DB()
		Expect(dbErr).ToNot(HaveOccurred())
		Expect(sqlDB.Close()).To(Succeed())
	})
	return database
}

func assertSchema(database *gorm.DB) {
	GinkgoHelper()
	models := []any{
		&storage.Project{},
		&storage.Snapshot{},
		&storage.ProjectHead{},
		&storage.Root{},
		&storage.Source{},
		&storage.Node{},
		&storage.NodeLocation{},
		&storage.Field{},
		&storage.Relationship{},
	}
	for index, table := range tableNames {
		Expect(database.Migrator().HasTable(table)).To(BeTrue(), table)
		Expect(database.Migrator().HasTable(models[index])).To(BeTrue(), fmt.Sprintf("%T must map to %s", models[index], table))
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	project := storage.Project{
		ID: uuid.New(), ProjectKey: "project-" + uuid.NewString(), Name: "Example project",
		Properties: storage.JSON(`{"team":"platform"}`), CreatedAt: now, UpdatedAt: now,
	}
	Expect(database.Create(&project).Error).ToNot(HaveOccurred())

	snapshot := storage.Snapshot{
		ID: uuid.New(), ProjectID: project.ID, State: storage.SnapshotBuilding,
		RevisionSetHash: "revision-hash", ConfigurationHash: "configuration-hash",
		ExtractorVersion: "v1", PayloadSchema: "v1", DocumentPayload: storage.JSON(`{}`),
		StartedAt: now, Properties: storage.JSON(`{}`),
	}
	Expect(database.Create(&snapshot).Error).ToNot(HaveOccurred())
	Expect(database.Create(&storage.ProjectHead{
		ProjectID: project.ID, SnapshotID: snapshot.ID, Version: 0, ActivatedAt: now,
	}).Error).ToNot(HaveOccurred())

	rootA := storage.Root{
		ID: uuid.New(), SnapshotID: snapshot.ID, RootKey: "root-a", Kind: "git", MountPath: "",
		ContentSetHash: "root-a-hash", PathCase: "sensitive", NormalizationVersion: "v1", Properties: storage.JSON(`{}`),
	}
	rootB := storage.Root{
		ID: uuid.New(), SnapshotID: snapshot.ID, RootKey: "root-b", Kind: "git", MountPath: "vendor/example",
		ContentSetHash: "root-b-hash", PathCase: "sensitive", NormalizationVersion: "v1", Properties: storage.JSON(`{}`),
	}
	Expect(database.Create(&rootA).Error).ToNot(HaveOccurred())
	Expect(database.Create(&rootB).Error).ToNot(HaveOccurred())

	sourceA := storage.Source{
		ID: uuid.New(), RootID: rootA.ID, PathKey: "main.go", DisplayPath: "main.go", Kind: "file",
		Language: "go", ContentHash: "source-hash", SizeBytes: 128, Properties: storage.JSON(`{}`),
	}
	nodeB := storage.Node{
		ID: uuid.New(), SnapshotID: snapshot.ID, RootID: rootB.ID, ChildSlot: "types", Ordinal: 0,
		NodeType: "record", IdentityKey: "identity-key", SymbolKey: "example.Record", Module: "example",
		Package: "example", TypeName: "Record", Language: "go", Traits: storage.JSON(`[]`),
		PayloadSchema: "v1", Payload: storage.JSON(`{"recordType":"table"}`), SemanticHash: "semantic-hash", CreatedAt: now,
	}
	Expect(database.Create(&sourceA).Error).ToNot(HaveOccurred())
	Expect(database.Create(&nodeB).Error).ToNot(HaveOccurred())

	err := database.Create(&storage.NodeLocation{
		ID: uuid.New(), RootID: rootB.ID, NodeID: nodeB.ID, SourceID: sourceA.ID,
		Role: "declaration", Ordinal: 0, IsPrimary: true,
	}).Error
	Expect(err).To(HaveOccurred(), "a location must not join a node and source from different roots")

	sourceB := storage.Source{
		ID: uuid.New(), RootID: rootB.ID, PathKey: "record.go", DisplayPath: "record.go", Kind: "file",
		Language: "go", ContentHash: "record-source-hash", SizeBytes: 256, Properties: storage.JSON(`{}`),
	}
	Expect(database.Create(&sourceB).Error).ToNot(HaveOccurred())
	Expect(database.Create(&storage.NodeLocation{
		ID: uuid.New(), RootID: rootB.ID, NodeID: nodeB.ID, SourceID: sourceB.ID,
		Role: "declaration", Ordinal: 0, IsPrimary: true,
	}).Error).ToNot(HaveOccurred())
	Expect(database.Create(&storage.Field{
		NodeID: nodeB.ID, Role: "column", Label: "Name", FieldType: "string", NativeType: "text",
		EnumValues: storage.JSON(`[]`), TypeRef: storage.JSON(`{}`), DefaultValue: storage.JSON(`null`),
		Validation: storage.JSON(`{}`), Visibility: "public",
	}).Error).ToNot(HaveOccurred())
	Expect(database.Create(&storage.Relationship{
		ID: uuid.New(), SnapshotID: snapshot.ID, FromRootID: rootB.ID, FromNodeID: nodeB.ID,
		EdgeKey: "depends-on:external", ToProjectKey: &project.ProjectKey, ToRootKey: &rootA.RootKey,
		ToIdentityKey: "external-identity", ToSymbolKey: "external.Symbol", ToIdentifier: storage.JSON(`{"type":"Symbol"}`),
		RelationshipType: "depends_on", SourceID: &sourceB.ID, StatementPath: "body.0", Text: "external.Symbol",
		Payload: storage.JSON(`{}`),
	}).Error).ToNot(HaveOccurred())

	var storedProject storage.Project
	Expect(database.First(&storedProject, "id = ?", project.ID).Error).ToNot(HaveOccurred())
	Expect(storedProject.ProjectKey).To(Equal(project.ProjectKey))
	Expect(storedProject.Properties).To(MatchJSON(`{"team":"platform"}`))

	orphan := storage.Snapshot{
		ID: uuid.New(), ProjectID: uuid.New(), State: storage.SnapshotBuilding,
		RevisionSetHash: "revision-hash", ConfigurationHash: "configuration-hash",
		ExtractorVersion: "v1", PayloadSchema: "v1", DocumentPayload: storage.JSON(`{}`),
		StartedAt: now, Properties: storage.JSON(`{}`),
	}
	err = database.Create(&orphan).Error
	Expect(err).To(HaveOccurred(), "orphan snapshot must violate its project foreign key")

	Expect(database.Delete(&project).Error).ToNot(HaveOccurred())
	var snapshots int64
	Expect(database.Table("uir_snapshots").Where("id = ?", snapshot.ID).Count(&snapshots).Error).ToNot(HaveOccurred())
	Expect(snapshots).To(BeZero(), fmt.Sprintf("snapshot %s must cascade with project %s", snapshot.ID, project.ID))
}
