package indexer

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"gorm.io/gorm"
)

var _ = Describe("Incremental indexing", func() {
	DescribeTable("re-resolves a reused imported call when root candidates change", func(ctx SpecContext, open func(context.Context) *gorm.DB) {
		database := open(ctx)
		workspace := GinkgoT().TempDir()
		writeFile(filepath.Join(workspace, "go.mod"), "module example.org/app\n\ngo 1.26\n")
		writeFile(filepath.Join(workspace, "main.go"), "package app\n\nimport \"example.org/lib\"\n\nfunc Run() { lib.Do() }\n")
		lib := filepath.Join(workspace, "lib")
		Expect(os.MkdirAll(filepath.Join(lib, ".git"), 0o755)).To(Succeed())
		writeFile(filepath.Join(lib, "go.mod"), "module example.org/lib\n\ngo 1.26\n")
		writeFile(filepath.Join(lib, "lib.go"), "package lib\n\nfunc Do() {}\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		options := Options{ProjectKey: "root-changes", Path: workspace, RootKey: "app"}

		first, err := engine.Index(ctx, options)
		Expect(err).ToNot(HaveOccurred())
		firstCall := storedCall(database, first.SnapshotID)
		Expect(firstCall.ToNodeID).ToNot(BeNil())
		Expect(firstCall.ToRootKey).To(HaveValue(Equal("app/lib")))

		copyRoot := filepath.Join(workspace, "libcopy")
		Expect(os.MkdirAll(filepath.Join(copyRoot, ".git"), 0o755)).To(Succeed())
		writeFile(filepath.Join(copyRoot, "go.mod"), "module example.org/lib\n\ngo 1.26\n")
		writeFile(filepath.Join(copyRoot, "lib.go"), "package lib\n\nfunc Do() {}\n")
		second, err := engine.Index(ctx, options)
		Expect(err).ToNot(HaveOccurred())
		Expect(second.ReusedFiles).To(Equal(2))
		Expect(second.ParsedFiles).To(Equal(1))
		ambiguous := storedCall(database, second.SnapshotID)
		Expect(ambiguous.ToNodeID).To(BeNil())
		Expect(ambiguous.ToRootKey).To(BeNil())

		Expect(os.Remove(filepath.Join(lib, "lib.go"))).To(Succeed())
		third, err := engine.Index(ctx, options)
		Expect(err).ToNot(HaveOccurred())
		Expect(third.ReusedFiles).To(Equal(2))
		resolved := storedCall(database, third.SnapshotID)
		Expect(resolved.ToNodeID).ToNot(BeNil())
		Expect(resolved.ToRootKey).To(HaveValue(Equal("app/libcopy")))
	}, Entry("SQLite", openIndexerSQLite), Entry("PostgreSQL", openIndexerPostgres))

	DescribeTable("publishes immutable snapshots with isolated Git roots",
		func(ctx SpecContext, open func(context.Context) *gorm.DB) {
			database := open(ctx)
			workspace := newGoWorkspace()
			projectKey := "project-" + uuid.NewString()
			servicePath := filepath.Join(workspace, "service.go")

			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			first, err := RunTask(ctx, engine, Options{
				ProjectKey: projectKey, ProjectName: "Example", Path: workspace, RootKey: "app",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(first).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ProjectKey": Equal(projectKey), "HeadVersion": BeEquivalentTo(1), "Roots": Equal(2),
				"Files": Equal(5), "ParsedFiles": Equal(5), "ReusedFiles": BeZero(), "Unchanged": BeFalse(),
			}))
			Expect(first.SnapshotID).ToNot(BeEmpty())
			assertRootSymbols(database, first.SnapshotID, map[string][]string{
				"app":        {"Name", "Notify", "Platform", "Run", "Service", "Stop"},
				"app/plugin": {"Run", "Service"},
			})
			var pluginRoot storage.Root
			Expect(database.Where("snapshot_id = ? AND root_key = ?", first.SnapshotID, "app/plugin").First(&pluginRoot).Error).ToNot(HaveOccurred())
			Expect(pluginRoot.Kind).To(Equal("git-submodule"))
			Expect(pluginRoot.SubmodulePath).To(HaveValue(Equal("plugin")))
			var platformLocations int64
			Expect(database.Table("uir_node_locations AS location").
				Joins("JOIN uir_nodes AS node ON node.id = location.node_id").
				Where("node.snapshot_id = ? AND node.method = ?", first.SnapshotID, "Platform").
				Count(&platformLocations).Error).ToNot(HaveOccurred())
			Expect(platformLocations).To(Equal(int64(2)))
			var resolvedCalls int64
			Expect(database.Model(&storage.Relationship{}).
				Where("snapshot_id = ? AND relationship_type = ? AND to_node_id IS NOT NULL", first.SnapshotID, uir.RelationshipTypeCall).
				Count(&resolvedCalls).Error).ToNot(HaveOccurred())
			Expect(resolvedCalls).To(Equal(int64(3)))
			var unresolvedCalls int64
			Expect(database.Model(&storage.Relationship{}).
				Where("snapshot_id = ? AND relationship_type = ? AND to_node_id IS NULL", first.SnapshotID, uir.RelationshipTypeCall).
				Count(&unresolvedCalls).Error).ToNot(HaveOccurred())
			Expect(unresolvedCalls).To(Equal(int64(1)))

			unchanged, err := engine.Index(ctx, Options{
				ProjectKey: projectKey, Path: workspace, RootKey: "app",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(unchanged.SnapshotID).To(Equal(first.SnapshotID))
			Expect(unchanged.Unchanged).To(BeTrue())
			Expect(unchanged.ParsedFiles).To(BeZero())

			writeFile(servicePath, `package acme

import "example.org/acme/helper"

type Service struct{ Name string }

func (s *Service) Stop() {
	helper.Notify()
}
`)
			second, err := engine.Index(ctx, Options{
				ProjectKey: projectKey, Path: workspace, RootKey: "app",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(second.SnapshotID).ToNot(Equal(first.SnapshotID))
			Expect(second).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"HeadVersion": BeEquivalentTo(2), "ParsedFiles": Equal(1), "ReusedFiles": Equal(4), "Unchanged": BeFalse(),
			}))
			assertRootSymbols(database, second.SnapshotID, map[string][]string{
				"app":        {"Name", "Notify", "Platform", "Service", "Stop"},
				"app/plugin": {"Run", "Service"},
			})

			Expect(os.Remove(filepath.Join(workspace, "helper", "helper.go"))).To(Succeed())
			third, err := engine.Index(ctx, Options{
				ProjectKey: projectKey, Path: workspace, RootKey: "app",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(third.HeadVersion).To(Equal(int64(3)))
			Expect(third.Files).To(Equal(4))
			var copiedFields int64
			Expect(database.Table("uir_fields AS field").
				Joins("JOIN uir_nodes AS node ON node.id = field.node_id").
				Where("node.snapshot_id = ? AND node.field = ?", third.SnapshotID, "Name").
				Count(&copiedFields).Error).ToNot(HaveOccurred())
			Expect(copiedFields).To(Equal(int64(1)))
			assertRootSymbols(database, third.SnapshotID, map[string][]string{
				"app":        {"Name", "Platform", "Service", "Stop"},
				"app/plugin": {"Run", "Service"},
			})
			Expect(database.Model(&storage.Relationship{}).
				Where("snapshot_id = ? AND relationship_type = ? AND to_node_id IS NULL", third.SnapshotID, uir.RelationshipTypeCall).
				Count(&unresolvedCalls).Error).ToNot(HaveOccurred())
			Expect(unresolvedCalls).To(Equal(int64(3)))
		},
		Entry("SQLite", openIndexerSQLite),
		Entry("PostgreSQL", openIndexerPostgres),
	)
})

func storedCall(database *gorm.DB, snapshotID string) storage.Relationship {
	GinkgoHelper()
	var relationship storage.Relationship
	Expect(database.Where("snapshot_id = ? AND relationship_type = ?", snapshotID, uir.RelationshipTypeCall).
		First(&relationship).Error).ToNot(HaveOccurred())
	return relationship
}

func openIndexerSQLite(ctx context.Context) *gorm.DB {
	GinkgoHelper()
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "index.db")})
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(closeIndexerDB, database)
	return database
}

func openIndexerPostgres(ctx context.Context) *gorm.DB {
	GinkgoHelper()
	testDatabase := dbtest.ForGinkgo(dbtest.Options{Name: "uir_indexer"})
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: testDatabase.DSN(), Schema: "uir_indexer"})
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(closeIndexerDB, database)
	return database
}

func closeIndexerDB(database *gorm.DB) {
	GinkgoHelper()
	sqlDB, err := database.DB()
	Expect(err).ToNot(HaveOccurred())
	Expect(sqlDB.Close()).To(Succeed())
}

func newGoWorkspace() string {
	GinkgoHelper()
	workspace := GinkgoT().TempDir()
	Expect(os.Mkdir(filepath.Join(workspace, ".git"), 0o755)).To(Succeed())
	writeFile(filepath.Join(workspace, "go.mod"), "module example.org/acme\n\ngo 1.26\n")
	writeFile(filepath.Join(workspace, ".gitmodules"), "[submodule \"plugin\"]\n\tpath = plugin\n\turl = https://example.org/plugin.git\n")
	writeFile(filepath.Join(workspace, "service.go"), `package acme

import "example.org/acme/helper"

type Service struct{ Name string }

func (s *Service) Run() {
	helper.Notify()
	s.Stop()
}

func (s *Service) Stop() {}
`)
	Expect(os.MkdirAll(filepath.Join(workspace, "helper"), 0o755)).To(Succeed())
	writeFile(filepath.Join(workspace, "helper", "helper.go"), "package helper\n\nfunc Notify() {}\n")
	platform := "package acme\n\nimport \"example.org/acme/helper\"\n\nfunc Platform() { helper.Notify() }\n"
	writeFile(filepath.Join(workspace, "platform_linux.go"), platform)
	writeFile(filepath.Join(workspace, "platform_darwin.go"), platform)
	plugin := filepath.Join(workspace, "plugin")
	Expect(os.MkdirAll(filepath.Join(plugin, ".git"), 0o755)).To(Succeed())
	writeFile(filepath.Join(plugin, "go.mod"), "module example.org/acme\n\ngo 1.26\n")
	writeFile(filepath.Join(plugin, "service.go"), "package acme\n\ntype Service struct{}\n\nfunc (s *Service) Run() {}\n")
	return workspace
}

func writeFile(path, content string) {
	GinkgoHelper()
	Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
	modified := time.Now().Add(time.Second)
	Expect(os.Chtimes(path, modified, modified)).To(Succeed())
}

func assertRootSymbols(database *gorm.DB, snapshotID string, expected map[string][]string) {
	GinkgoHelper()
	for rootKey, names := range expected {
		var actual []string
		Expect(database.Table("uir_nodes AS node").
			Select("CASE WHEN node.method <> '' THEN node.method WHEN node.field <> '' THEN node.field ELSE node.type_name END").
			Joins("JOIN uir_roots AS root ON root.id = node.root_id").
			Where("node.snapshot_id = ? AND root.root_key = ? AND node.node_type <> ?", snapshotID, rootKey, uir.NodeTypePackage).
			Order("1").Scan(&actual).Error).ToNot(HaveOccurred())
		Expect(actual).To(Equal(names), rootKey)
	}
}
