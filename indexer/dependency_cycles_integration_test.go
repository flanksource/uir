package indexer

import (
	"archive/zip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("dependency cycles", func() {
	It("publishes and reuses both snapshots of a local module cycle", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := GinkgoT().TempDir()
		first, second := filepath.Join(workspace, "first"), filepath.Join(workspace, "second")
		Expect(os.MkdirAll(first, 0o755)).To(Succeed())
		Expect(os.MkdirAll(second, 0o755)).To(Succeed())
		writeFile(filepath.Join(workspace, "go.work"), "go 1.26\n\nuse (\n\t./first\n\t./second\n)\n")
		writeFile(filepath.Join(first, "go.mod"), "module example.org/first\n\ngo 1.26\n\nrequire example.org/second v0.0.0\n")
		writeFile(filepath.Join(second, "go.mod"), "module example.org/second\n\ngo 1.26\n\nrequire example.org/first v0.0.0\n")
		writeFile(filepath.Join(first, "first.go"), "package first\n\nfunc First() {}\n")
		writeFile(filepath.Join(second, "second.go"), "package second\n\nfunc Second() {}\n")
		for _, args := range [][]string{{"init"}, {"add", "first", "second", "go.work"}, {"-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "-m", "test: cyclic modules"}} {
			output, err := exec.CommandContext(ctx, "git", append([]string{"-C", workspace}, args...)...).CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(output))
		}
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		indexed := indexOnce(ctx, engine, first)
		firstID := uuid.MustParse(indexed.SnapshotID)
		firstEdges, err := storage.ModuleDependencies(ctx, database, firstID)
		Expect(err).ToNot(HaveOccurred())
		Expect(firstEdges.Items).To(HaveLen(1))
		Expect(firstEdges.Items[0].TargetSnapshotID).ToNot(BeNil(), "%+v", firstEdges.Items[0])
		secondID := *firstEdges.Items[0].TargetSnapshotID
		secondEdges, err := storage.ModuleDependencies(ctx, database, secondID)
		Expect(err).ToNot(HaveOccurred())
		Expect(secondEdges.Items).To(HaveLen(1))
		Expect(secondEdges.Items[0].TargetSnapshotID).To(Equal(&firstID))
		Expect(loadSnapshot(database, indexed.SnapshotID).WorktreeState).To(Equal(storage.WorktreeClean))
		again := indexOnce(ctx, engine, first)
		Expect(again.Unchanged).To(BeTrue())
		Expect(again.SnapshotID).To(Equal(indexed.SnapshotID))
		writeFile(filepath.Join(second, "second.go"), "package second\n\nfunc Second() int { return 2 }\n")
		changed := indexOnce(ctx, engine, first)
		Expect(changed.SnapshotID).ToNot(Equal(indexed.SnapshotID))
		changedEdges, err := storage.ModuleDependencies(ctx, database, uuid.MustParse(changed.SnapshotID))
		Expect(err).ToNot(HaveOccurred())
		Expect(changedEdges.Items[0].TargetSnapshotID).ToNot(Equal(&secondID))
		updatedSecond := *changedEdges.Items[0].TargetSnapshotID
		updatedSecondEdges, err := storage.ModuleDependencies(ctx, database, updatedSecond)
		Expect(err).ToNot(HaveOccurred())
		changedID := uuid.MustParse(changed.SnapshotID)
		Expect(updatedSecondEdges.Items[0].TargetSnapshotID).To(Equal(&changedID))
		Expect(secondEdges.Items[0].TargetSnapshotID).To(Equal(&firstID))
		Expect(loadSnapshot(database, changed.SnapshotID).WorktreeState).To(Equal(storage.WorktreeDirty))
	})

	It("propagates a changed three-module cycle to its dependent parent", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := GinkgoT().TempDir()
		modules := []struct{ name, requirement string }{
			{"parent", "first"}, {"first", "second"}, {"second", "third"}, {"third", "first"},
		}
		work := "go 1.26\n\nuse (\n"
		for _, module := range modules {
			work += "\t./" + module.name + "\n"
			path := filepath.Join(workspace, module.name)
			Expect(os.MkdirAll(path, 0o755)).To(Succeed())
			writeFile(filepath.Join(path, "go.mod"), fmt.Sprintf("module example.org/%s\n\ngo 1.26\n\nrequire example.org/%s v0.0.0\n", module.name, module.requirement))
			writeFile(filepath.Join(path, "source.go"), "package "+module.name+"\n\nfunc Value() int { return 1 }\n")
		}
		writeFile(filepath.Join(workspace, "go.work"), work+")\n")
		for _, args := range [][]string{{"init"}, {"add", "parent", "first", "second", "third", "go.work"}, {"-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "-m", "test: module graph"}} {
			output, err := exec.CommandContext(ctx, "git", append([]string{"-C", workspace}, args...)...).CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(output))
		}
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		parent := indexOnce(ctx, engine, filepath.Join(workspace, "parent"))
		original := map[string]uuid.UUID{}
		current := uuid.MustParse(parent.SnapshotID)
		for _, module := range modules {
			original[module.name] = current
			edges, err := storage.ModuleDependencies(ctx, database, current)
			Expect(err).ToNot(HaveOccurred())
			Expect(edges.Items).To(HaveLen(1))
			Expect(edges.Items[0].TargetSnapshotID).ToNot(BeNil())
			current = *edges.Items[0].TargetSnapshotID
		}
		Expect(current).To(Equal(original["first"]))
		writeFile(filepath.Join(workspace, "third", "source.go"), "package third\n\nfunc Value() int { return 2 }\n")
		changed := indexOnce(ctx, engine, filepath.Join(workspace, "parent"))
		Expect(changed.SnapshotID).ToNot(Equal(parent.SnapshotID))
		current = uuid.MustParse(changed.SnapshotID)
		for _, module := range modules {
			Expect(current).ToNot(Equal(original[module.name]))
			Expect(loadSnapshot(database, current.String()).WorktreeState).To(Equal(storage.WorktreeDirty))
			edges, err := storage.ModuleDependencies(ctx, database, current)
			Expect(err).ToNot(HaveOccurred())
			Expect(edges.Items).To(HaveLen(1))
			Expect(edges.Items[0].TargetSnapshotID).ToNot(BeNil())
			current = *edges.Items[0].TargetSnapshotID
		}
	})

	It("does not publish any cycle member when extraction fails", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := GinkgoT().TempDir()
		first, second := filepath.Join(workspace, "first"), filepath.Join(workspace, "second")
		Expect(os.MkdirAll(first, 0o755)).To(Succeed())
		Expect(os.MkdirAll(second, 0o755)).To(Succeed())
		writeFile(filepath.Join(workspace, "go.work"), "go 1.26\n\nuse (\n\t./first\n\t./second\n)\n")
		writeFile(filepath.Join(first, "go.mod"), "module example.org/first\n\ngo 1.26\n\nrequire example.org/second v0.0.0\n")
		writeFile(filepath.Join(second, "go.mod"), "module example.org/second\n\ngo 1.26\n\nrequire example.org/first v0.0.0\n")
		writeFile(filepath.Join(first, "source.go"), "package first\n\nfunc Value() {}\n")
		writeFile(filepath.Join(second, "source.go"), "package second\n\nfunc Broken(\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: first, ExactLocation: first, Reason: storage.ReasonAdd})
		Expect(err).To(HaveOccurred())
		var snapshots int64
		Expect(database.Model(&storage.ModuleSnapshot{}).Count(&snapshots).Error).To(Succeed())
		Expect(snapshots).To(BeZero())
	})

	It("preserves parent locations for nested modules in a cycle", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		parent := canonicalTempDir()
		child := filepath.Join(parent, "plugin")
		Expect(os.MkdirAll(child, 0o755)).To(Succeed())
		writeFile(filepath.Join(parent, "go.work"), "go 1.26\n\nuse (\n\t.\n\t./plugin\n)\n")
		writeFile(filepath.Join(parent, "go.mod"), "module example.org/app\n\ngo 1.26\n\nrequire example.org/plugin v0.0.0\n")
		writeFile(filepath.Join(child, "go.mod"), "module example.org/plugin\n\ngo 1.26\n\nrequire example.org/app v0.0.0\n")
		writeFile(filepath.Join(parent, "main.go"), "package app\n\nfunc Run() {}\n")
		writeFile(filepath.Join(child, "plugin.go"), "package plugin\n\nfunc Use() {}\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		results, err := engine.IndexModules(ctx, ModuleOptions{Path: parent, Reason: storage.ReasonAdd})
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))
		var parentLocation, childLocation storage.ModuleLocation
		Expect(database.Where("canonical_path = ?", parent).Take(&parentLocation).Error).To(Succeed())
		Expect(database.Where("canonical_path = ?", child).Take(&childLocation).Error).To(Succeed())
		Expect(childLocation.ParentLocationID).To(Equal(&parentLocation.ID))
	})

	It("links a cycle between registered tagged module versions without moving heads", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		base := canonicalTempDir()
		proxy := filepath.Join(base, "proxy")
		first, second := filepath.Join(base, "first"), filepath.Join(base, "second")
		for _, fixture := range []struct {
			module, other, directory string
		}{
			{"example.org/first", "example.org/second", first},
			{"example.org/second", "example.org/first", second},
		} {
			Expect(os.MkdirAll(fixture.directory, 0o755)).To(Succeed())
			manifest := fmt.Sprintf("module %s\n\ngo 1.26\n\nrequire %s v1.0.0\n", fixture.module, fixture.other)
			writeFile(filepath.Join(fixture.directory, "go.mod"), manifest)
			writeFile(filepath.Join(fixture.directory, "source.go"), "package source\n\nfunc Value() int { return 1 }\n")
			writeModuleProxy(proxy, fixture.module, "v1.0.0", manifest)
		}
		GinkgoT().Setenv("GOPROXY", "file://"+proxy)
		GinkgoT().Setenv("GOSUMDB", "off")
		GinkgoT().Setenv("GOWORK", "off")
		for _, checkout := range []string{first, second} {
			for _, args := range [][]string{{"mod", "download", "all"}} {
				command := exec.CommandContext(ctx, "go", args...)
				command.Dir = checkout
				output, err := command.CombinedOutput()
				Expect(err).ToNot(HaveOccurred(), string(output))
			}
			for _, args := range [][]string{{"init"}, {"add", "go.mod", "go.sum", "source.go"}, {"-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "-m", "test: tagged module"}, {"tag", "v1.0.0"}} {
				output, err := exec.CommandContext(ctx, "git", append([]string{"-C", checkout}, args...)...).CombinedOutput()
				Expect(err).ToNot(HaveOccurred(), string(output))
			}
		}
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		firstHead := indexOnce(ctx, engine, first)
		secondHead := indexOnce(ctx, engine, second)
		version, err := engine.versionedSnapshot(ctx, "example.org/first", "v1.0.0", false)
		Expect(err).ToNot(HaveOccurred())
		firstEdges, err := storage.ModuleDependencies(ctx, database, version.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(firstEdges.Items).To(HaveLen(1))
		Expect(firstEdges.Items[0].TargetSnapshotID).ToNot(BeNil(), "%+v", firstEdges.Items[0])
		secondEdges, err := storage.ModuleDependencies(ctx, database, *firstEdges.Items[0].TargetSnapshotID)
		Expect(err).ToNot(HaveOccurred())
		Expect(secondEdges.Items).To(HaveLen(1))
		Expect(secondEdges.Items[0].TargetSnapshotID).To(Equal(&version.ID))
		Expect(loadSnapshot(database, firstHead.SnapshotID).ID).ToNot(Equal(version.ID))
		Expect(loadSnapshot(database, secondHead.SnapshotID).ID).ToNot(Equal(*firstEdges.Items[0].TargetSnapshotID))
		again, err := engine.versionedSnapshot(ctx, "example.org/first", "v1.0.0", false)
		Expect(err).ToNot(HaveOccurred())
		Expect(again.ID).To(Equal(version.ID))
		for _, expected := range []struct {
			module, checkout, snapshot string
		}{{"example.org/first", first, firstHead.SnapshotID}, {"example.org/second", second, secondHead.SnapshotID}} {
			head, found, err := loadHead(ctx, database, discoveredRoot{RootKey: expected.module, LocalPath: expected.checkout})
			Expect(err).ToNot(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(head.ID.String()).To(Equal(expected.snapshot))
		}
		writeFile(filepath.Join(second, "source.go"), "package source\n\nfunc Value() int { return 2 }\n")
		for _, args := range [][]string{{"add", "source.go"}, {"-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "-m", "test: update tagged dependency"}, {"tag", "-f", "v1.0.0"}} {
			output, err := exec.CommandContext(ctx, "git", append([]string{"-C", second}, args...)...).CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(output))
		}
		moved, err := engine.versionedSnapshot(ctx, "example.org/first", "v1.0.0", false)
		Expect(err).ToNot(HaveOccurred())
		Expect(moved.ID).ToNot(Equal(version.ID))
		movedEdges, err := storage.ModuleDependencies(ctx, database, moved.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(movedEdges.Items[0].TargetSnapshotID).ToNot(Equal(firstEdges.Items[0].TargetSnapshotID))
		movedSecondEdges, err := storage.ModuleDependencies(ctx, database, *movedEdges.Items[0].TargetSnapshotID)
		Expect(err).ToNot(HaveOccurred())
		Expect(movedSecondEdges.Items[0].TargetSnapshotID).To(Equal(&moved.ID))
		oldEdges, err := storage.ModuleDependencies(ctx, database, version.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(oldEdges.Items[0].TargetSnapshotID).To(Equal(firstEdges.Items[0].TargetSnapshotID))
	})
})

func writeModuleProxy(directory, module, version, manifest string) {
	GinkgoHelper()
	versionDirectory := filepath.Join(directory, filepath.FromSlash(module), "@v")
	Expect(os.MkdirAll(versionDirectory, 0o755)).To(Succeed())
	writeFile(filepath.Join(versionDirectory, version+".mod"), manifest)
	writeFile(filepath.Join(versionDirectory, version+".info"), fmt.Sprintf(`{"Version":%q,"Time":"2020-01-01T00:00:00Z"}`, version))
	writeFile(filepath.Join(versionDirectory, "list"), version+"\n")
	archive, err := os.Create(filepath.Join(versionDirectory, version+".zip"))
	Expect(err).ToNot(HaveOccurred())
	writer := zip.NewWriter(archive)
	for name, content := range map[string]string{"go.mod": manifest, "source.go": "package source\n\nfunc Value() int { return 1 }\n"} {
		entry, err := writer.Create(module + "@" + version + "/" + name)
		Expect(err).ToNot(HaveOccurred())
		_, err = entry.Write([]byte(content))
		Expect(err).ToNot(HaveOccurred())
	}
	Expect(writer.Close()).To(Succeed())
	Expect(archive.Close()).To(Succeed())
}
