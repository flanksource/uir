package indexer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

var _ = Describe("module indexing", func() {
	It("publishes a child snapshot when Git revision changes without source changes", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		workspace := GinkgoT().TempDir()
		writeFile(filepath.Join(workspace, "go.mod"), "module example.org/revisions\n\ngo 1.26\n")
		writeFile(filepath.Join(workspace, "main.go"), "package revisions\n\nfunc Run() {}\n")
		for _, args := range [][]string{
			{"-C", workspace, "init", "-q"},
			{"-C", workspace, "add", "go.mod", "main.go"},
			{"-C", workspace, "-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "-qm", "initial"},
		} {
			Expect(exec.Command("git", args...).Run()).To(Succeed())
		}
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		first, err := engine.IndexModules(ctx, ModuleOptions{Path: workspace})
		Expect(err).ToNot(HaveOccurred())
		Expect(exec.Command("git", "-C", workspace, "-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "--allow-empty", "-qm", "new revision").Run()).To(Succeed())
		second, err := engine.IndexModules(ctx, ModuleOptions{Path: workspace})
		Expect(err).ToNot(HaveOccurred())
		Expect(second[0].Unchanged).To(BeFalse())
		Expect(second[0].ParsedFiles).To(BeZero())
		Expect(second[0].HeadVersion).To(Equal(int64(2)))
		var snapshot storage.ModuleSnapshot
		Expect(database.Where("id = ?", second[0].SnapshotID).First(&snapshot).Error).To(Succeed())
		Expect(snapshot.BaseSnapshotID).To(HaveValue(Equal(uuid.MustParse(first[0].SnapshotID))))
		var deltaCount int64
		Expect(database.Model(&storage.SourceDelta{}).Where("snapshot_id = ?", snapshot.ID).Count(&deltaCount).Error).To(Succeed())
		Expect(deltaCount).To(BeZero())
		Expect(snapshot.WorktreeState).To(Equal(storage.WorktreeClean))
		Expect(snapshot.Revision).To(HaveLen(40))

		writeFile(filepath.Join(workspace, "main.go"), "package revisions\n\nfunc Run() { Run() }\n")
		dirty, err := engine.IndexModules(ctx, ModuleOptions{Path: workspace})
		Expect(err).ToNot(HaveOccurred())
		var dirtySnapshot storage.ModuleSnapshot
		Expect(database.Where("id = ?", dirty[0].SnapshotID).First(&dirtySnapshot).Error).To(Succeed())
		Expect(dirtySnapshot.WorktreeState).To(Equal(storage.WorktreeDirty))
		Expect(dirtySnapshot.Revision).To(Equal(snapshot.Revision))
	})

	It("records a snapshot that indexes a Git-ignored Go file as dirty until the file is tracked", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		workspace := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(workspace, "hack"), 0o755)).To(Succeed())
		writeFile(filepath.Join(workspace, "go.mod"), "module example.org/ignored\n\ngo 1.26\n")
		writeFile(filepath.Join(workspace, ".gitignore"), "hack/\n")
		writeFile(filepath.Join(workspace, "main.go"), "package ignored\n\nfunc Run() {}\n")
		writeFile(filepath.Join(workspace, "hack", "tool.go"), "package hack\n\nfunc Tool() {}\n")
		commit := []string{"-C", workspace, "-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "-qm"}
		for _, args := range [][]string{
			{"-C", workspace, "init", "-q"},
			{"-C", workspace, "add", ".gitignore", "go.mod", "main.go"},
			append(commit, "initial"),
		} {
			Expect(exec.Command("git", args...).Run()).To(Succeed())
		}
		Expect(exec.Command("git", "-C", workspace, "status", "--porcelain").Output()).To(BeEmpty(), "Git reports no change for an ignored file")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		indexedState := func() storage.WorktreeState {
			GinkgoHelper()
			results, err := engine.IndexModules(ctx, ModuleOptions{Path: workspace})
			Expect(err).ToNot(HaveOccurred())
			Expect(results[0].Files).To(Equal(2), "discovery indexes the ignored file")
			var snapshot storage.ModuleSnapshot
			Expect(database.Where("id = ?", results[0].SnapshotID).First(&snapshot).Error).To(Succeed())
			return snapshot.WorktreeState
		}
		Expect(indexedState()).To(Equal(storage.WorktreeDirty))

		Expect(exec.Command("git", "-C", workspace, "add", "-f", "hack/tool.go").Run()).To(Succeed())
		Expect(exec.Command("git", append(commit, "track the tool")...).Run()).To(Succeed())
		Expect(indexedState()).To(Equal(storage.WorktreeClean))
	})

	It("infers an enclosing go.mod when adding a package directory", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		workspace := GinkgoT().TempDir()
		packageDir := filepath.Join(workspace, "internal", "worker")
		Expect(os.MkdirAll(packageDir, 0o755)).To(Succeed())
		writeFile(filepath.Join(workspace, "go.mod"), "module example.org/enclosing\n\ngo 1.26\n")
		writeFile(filepath.Join(packageDir, "worker.go"), "package worker\n\nfunc Run() {}\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		result, err := engine.IndexModules(ctx, ModuleOptions{Path: packageDir})
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(HaveLen(1))
		Expect(result[0].RootKey).To(Equal("example.org/enclosing"))
		Expect(result[0].Files).To(Equal(1))
		var snapshot storage.ModuleSnapshot
		Expect(database.Where("id = ?", result[0].SnapshotID).First(&snapshot).Error).To(Succeed())
		Expect(snapshot.WorktreeState).To(Equal(storage.WorktreeUnknown), "a module outside Git has no known worktree state")
		Expect(snapshot.Revision).To(BeEmpty())
	})

	It("rejects reindexing an unregistered module without creating a root", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		workspace := GinkgoT().TempDir()
		writeFile(filepath.Join(workspace, "go.mod"), "module example.org/new\n\ngo 1.26\n")
		writeFile(filepath.Join(workspace, "new.go"), "package new\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: workspace, ExistingOnly: true})
		Expect(err).To(MatchError(ContainSubstring("not registered")))
		var roots int64
		Expect(database.Model(&storage.ModuleRoot{}).Count(&roots).Error).To(Succeed())
		Expect(roots).To(BeZero())
	})

	DescribeTable("discovers nested modules and rolls back a failed workspace publication", func(ctx SpecContext, open func(context.Context) *gorm.DB) {
		database := open(ctx)
		workspace := GinkgoT().TempDir()
		plugin := filepath.Join(workspace, "plugin")
		Expect(os.MkdirAll(plugin, 0o755)).To(Succeed())
		writeFile(filepath.Join(workspace, "go.mod"), "module example.org/app\n\ngo 1.26\n")
		writeFile(filepath.Join(workspace, "main.go"), "package app\n\nfunc Run() {}\n")
		writeFile(filepath.Join(plugin, "go.mod"), "module example.org/plugin\n\ngo 1.26\n")
		writeFile(filepath.Join(plugin, "plugin.go"), "package plugin\n\nfunc Use() {\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: workspace})
		Expect(err).To(MatchError(ContainSubstring("parse Go source")))
		var roots int64
		Expect(database.Model(&storage.ModuleRoot{}).Count(&roots).Error).To(Succeed())
		Expect(roots).To(BeZero())
		writeFile(filepath.Join(plugin, "plugin.go"), "package plugin\n\nfunc Use() {}\n")
		results, err := engine.IndexModules(ctx, ModuleOptions{Path: workspace})
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))
		var parent, child storage.ModuleLocation
		Expect(database.Where("canonical_path = ?", results[0].Location).First(&parent).Error).To(Succeed())
		Expect(database.Where("canonical_path = ?", results[1].Location).First(&child).Error).To(Succeed())
		Expect(child.ParentLocationID).To(HaveValue(Equal(parent.ID)))
		Expect(child.MountPath).To(Equal("plugin"))
		Expect(results[0].RootKey).To(Equal("example.org/app"))
		Expect(results[1].RootKey).To(Equal("example.org/plugin"))
	}, Entry("SQLite", openIndexerSQLite), Entry("PostgreSQL", openIndexerPostgres))

	DescribeTable("keeps checkout heads separate and publishes source deltas", func(ctx SpecContext, open func(context.Context) *gorm.DB) {
		database := open(ctx)
		workspace := GinkgoT().TempDir()
		firstPath := filepath.Join(workspace, "main")
		secondPath := filepath.Join(workspace, "feature")
		Expect(os.MkdirAll(firstPath, 0o755)).To(Succeed())
		Expect(os.MkdirAll(secondPath, 0o755)).To(Succeed())
		for _, path := range []string{firstPath, secondPath} {
			writeFile(filepath.Join(path, "go.mod"), "module example.org/service\n\ngo 1.26\n")
			writeFile(filepath.Join(path, "service.go"), "package service\n\nfunc Run() {}\n")
		}
		writeFile(filepath.Join(firstPath, "obsolete.go"), "package service\n\nfunc Obsolete() {}\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		first, err := engine.IndexModules(ctx, ModuleOptions{Path: firstPath})
		Expect(err).ToNot(HaveOccurred())
		Expect(first).To(HaveLen(1))
		Expect(first[0].ParsedFiles).To(Equal(2))
		Expect(first[0].HeadVersion).To(Equal(int64(1)))
		second, err := engine.IndexModules(ctx, ModuleOptions{Path: secondPath})
		Expect(err).ToNot(HaveOccurred())
		Expect(second).To(HaveLen(1))
		// service.go's bytes are shared, but its package lost obsolete.go, so the package input hash
		// changed and the file gets a fresh document over the same source revision.
		Expect(second[0].ParsedFiles).To(Equal(1))
		Expect(second[0].ReusedFiles).To(Equal(0))
		Expect(second[0].HeadVersion).To(Equal(int64(1)))
		var root storage.ModuleRoot
		Expect(database.Where("root_key = ?", "example.org/service").First(&root).Error).To(Succeed())
		var serviceRevisions, serviceDocuments int64
		Expect(database.Model(&storage.SourceRevision{}).Where("root_id = ? AND path_key = ?", root.ID, "service.go").Count(&serviceRevisions).Error).To(Succeed())
		Expect(database.Model(&storage.Document{}).Where("root_id = ? AND path_key = ?", root.ID, "service.go").Count(&serviceDocuments).Error).To(Succeed())
		Expect([]int64{serviceRevisions, serviceDocuments}).To(Equal([]int64{1, 2}))
		var primary storage.ModulePrimary
		Expect(database.Where("root_id = ?", root.ID).First(&primary).Error).To(Succeed())
		var firstLocation storage.ModuleLocation
		Expect(database.Where("id = ?", primary.LocationID).First(&firstLocation).Error).To(Succeed())
		canonicalFirstPath, err := filepath.EvalSymlinks(firstPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(firstLocation.CanonicalPath).To(Equal(canonicalFirstPath))
		var heads []storage.ModuleLocationHead
		Expect(database.Where("root_id = ?", root.ID).Find(&heads).Error).To(Succeed())
		Expect(heads).To(HaveLen(2))
		var secondSnapshot storage.ModuleSnapshot
		Expect(database.Where("id = ?", second[0].SnapshotID).First(&secondSnapshot).Error).To(Succeed())
		Expect(secondSnapshot.BaseSnapshotID).To(HaveValue(Equal(uuid.MustParse(first[0].SnapshotID))))
		var deltas []storage.SourceDelta
		Expect(database.Where("snapshot_id = ?", secondSnapshot.ID).Find(&deltas).Error).To(Succeed())
		Expect(deltas).To(ConsistOf(storage.SourceDelta{
			SnapshotID: secondSnapshot.ID, RootID: root.ID, PathKey: "obsolete.go", Operation: storage.SourceDelete,
		}))
		sources, err := storage.EffectiveSources(ctx, database, secondSnapshot.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(sources).To(HaveKey("service.go"))
		Expect(sources).ToNot(HaveKey("obsolete.go"))
		writeFile(filepath.Join(secondPath, "service.go"), "package service\n\nfunc Run() {}\nfunc Added() {}\n")
		third, err := engine.IndexModules(ctx, ModuleOptions{Path: secondPath})
		Expect(err).ToNot(HaveOccurred())
		Expect(third[0].ParsedFiles).To(Equal(1))
		Expect(third[0].HeadVersion).To(Equal(int64(2)))
		var thirdSnapshot storage.ModuleSnapshot
		Expect(database.Where("id = ?", third[0].SnapshotID).First(&thirdSnapshot).Error).To(Succeed())
		Expect(thirdSnapshot.BaseSnapshotID).To(HaveValue(Equal(secondSnapshot.ID)))
		Expect(database.Where("snapshot_id = ?", thirdSnapshot.ID).Find(&deltas).Error).To(Succeed())
		Expect(deltas).To(HaveLen(1))
		Expect(deltas[0].PathKey).To(Equal("service.go"))
	}, Entry("SQLite", openIndexerSQLite), Entry("PostgreSQL", openIndexerPostgres))
})
