package indexer

import (
	"os"
	"path/filepath"

	"github.com/flanksource/commons-db/git"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pinned source indexing", func() {
	It("publishes an unborn staged tree without inventing a commit or a checkout head", func(ctx SpecContext) {
		root := canonicalTempDir()
		writeFile(filepath.Join(root, "go.mod"), "module example.org/initial-stage\n\ngo 1.26\n")
		writeFile(filepath.Join(root, "first.go"), "package initial\nfunc First() {}\n")
		gitOutput(ctx, root, "init", "-q")
		gitOutput(ctx, root, "add", "go.mod", "first.go")
		cache := &git.Cache{Root: filepath.Join(root, ".tmp", "sources")}
		source, err := cache.PrepareSource(ctx, git.SourceOptions{Directory: root, Kind: git.SourceStaged})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(source.Release)
		database := openIndexerSQLite(ctx)
		engine, err := New(database)
		Expect(err).NotTo(HaveOccurred())
		results, err := engine.IndexSource(ctx, SourceOptions{Source: source, Directory: source.Info().Dir})
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(HaveLen(1))
		var snapshot storage.ModuleSnapshot
		Expect(database.First(&snapshot, "id = ?", results[0].SnapshotID).Error).To(Succeed())
		Expect(snapshot.GitCommit).To(BeEmpty())
		Expect(snapshot.Revision).To(Equal("index-" + source.Info().TreeID))
		var heads int64
		Expect(database.Model(&storage.ModuleLocationHead{}).Count(&heads).Error).To(Succeed())
		Expect(heads).To(BeZero())
	})

	It("indexes captured staged bytes at the original location without advancing its head", func(ctx SpecContext) {
		root := canonicalTempDir()
		writeFile(filepath.Join(root, "go.mod"), "module example.org/pinned\n\ngo 1.26\n")
		writeFile(filepath.Join(root, "sample.go"), "package pinned\nfunc Head() {}\n")
		gitOutput(ctx, root, "init", "-q")
		commit := commitPaths(ctx, root, "initial", "go.mod", "sample.go")
		database := openIndexerSQLite(ctx)
		engine, err := New(database)
		Expect(err).NotTo(HaveOccurred())
		heads, err := engine.IndexModules(ctx, ModuleOptions{Path: root, Reason: storage.ReasonAdd})
		Expect(err).NotTo(HaveOccurred())
		staged := "package pinned\nfunc Staged() {}\n"
		writeFile(filepath.Join(root, "sample.go"), staged)
		gitOutput(ctx, root, "add", "sample.go")
		writeFile(filepath.Join(root, "sample.go"), "package pinned\nfunc Unstaged() {}\n")
		cache := &git.Cache{Root: filepath.Join(root, ".tmp", "sources")}
		source, err := cache.PrepareSource(ctx, git.SourceOptions{Directory: root, Kind: git.SourceStaged})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(source.Release)
		results, err := engine.IndexSource(ctx, SourceOptions{Source: source, Directory: source.Info().Dir})
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(HaveLen(1))
		Expect(results[0].Location).To(Equal(root))
		var snapshot storage.ModuleSnapshot
		Expect(database.First(&snapshot, "id = ?", results[0].SnapshotID).Error).To(Succeed())
		Expect(snapshot.Revision).To(Equal("index-" + source.Info().TreeID))
		Expect(snapshot.GitCommit).To(Equal(commit))
		Expect(snapshot.WorktreeState).To(Equal(storage.WorktreeDirty))
		var blob storage.SourceBlob
		Expect(database.First(&blob, "content_hash = ?", hashBytes([]byte(staged))).Error).To(Succeed())
		Expect(string(blob.Content)).To(Equal(staged))
		current, err := engine.CheckCurrent(ctx, root, CheckOptions{AllowIncomplete: true})
		Expect(err).To(HaveOccurred())
		Expect(current.SnapshotID).To(BeEmpty())
		var head storage.ModuleLocationHead
		Expect(database.First(&head).Error).To(Succeed())
		Expect(head.SnapshotID.String()).To(Equal(heads[0].SnapshotID))
		Expect(os.ReadFile(filepath.Join(root, "sample.go"))).To(BeEquivalentTo("package pinned\nfunc Unstaged() {}\n"))
		repeated, err := engine.IndexSource(ctx, SourceOptions{Source: source, Directory: source.Info().Dir})
		Expect(err).NotTo(HaveOccurred())
		Expect(repeated[0].SnapshotID).To(Equal(results[0].SnapshotID))
		Expect(repeated[0].Unchanged).To(BeTrue())
	})
})
