package indexer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("module dependency snapshots", func() {
	It("records an unresolved declared dependency when its selected version is unavailable", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		module := GinkgoT().TempDir()
		writeFile(filepath.Join(module, "go.mod"), "module example.org/shop\n\ngo 1.26\n\nrequire example.org/pricing v1.0.0\n")
		writeFile(filepath.Join(module, "shop.go"), "package shop\n")
		GinkgoT().Setenv("GOPROXY", "off")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		indexed := indexOnce(ctx, engine, module)
		dependencies, err := storage.ModuleDependencies(ctx, database, uuid.MustParse(indexed.SnapshotID))
		Expect(err).ToNot(HaveOccurred())
		Expect(dependencies.Captured).To(BeTrue())
		Expect(dependencies.Items).To(HaveLen(1))
		Expect(dependencies.Items[0].ModulePath).To(Equal("example.org/pricing"))
		Expect(dependencies.Items[0].DeclaredVersion).To(Equal("v1.0.0"))
		Expect(dependencies.Items[0].TargetSnapshotID).To(BeNil())
		Expect(dependencies.Items[0].UnresolvedReason).ToNot(BeEmpty())
	})

	It("links a workspace requirement and dirties the parent when its local target changes", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := GinkgoT().TempDir()
		shop, pricing := filepath.Join(workspace, "shop"), filepath.Join(workspace, "pricing")
		Expect(os.MkdirAll(shop, 0o755)).To(Succeed())
		Expect(os.MkdirAll(pricing, 0o755)).To(Succeed())
		writeFile(filepath.Join(workspace, "go.work"), "go 1.26\n\nuse ./shop\n\nreplace example.org/pricing => ./pricing\n")
		writeFile(filepath.Join(shop, "go.mod"), "module example.org/shop\n\ngo 1.26\n\nrequire example.org/pricing v0.0.0\n")
		writeFile(filepath.Join(shop, "shop.go"), "package shop\n\nimport \"example.org/pricing\"\n\nfunc Total() int { return pricing.Price() }\n")
		writeFile(filepath.Join(pricing, "go.mod"), "module example.org/pricing\n\ngo 1.26\n")
		writeFile(filepath.Join(pricing, "pricing.go"), "package pricing\n\nfunc Price() int { return 1 }\n")
		for _, args := range [][]string{{"init"}, {"add", "shop", "pricing", "go.work"}, {"-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "-m", "test: initial modules"}} {
			output, err := exec.CommandContext(ctx, "git", append([]string{"-C", workspace}, args...)...).CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(output))
		}
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		first := indexOnce(ctx, engine, shop)
		original, err := storage.ModuleDependencies(ctx, database, uuid.MustParse(first.SnapshotID))
		Expect(err).ToNot(HaveOccurred())
		Expect(original.Items).To(HaveLen(1))
		Expect(original.Items[0].TargetSnapshotID).ToNot(BeNil(), "%+v", original.Items[0])
		Expect(original.Items[0].ReplacePath).To(Equal("./pricing"))
		Expect(loadSnapshot(database, first.SnapshotID).WorktreeState).To(Equal(storage.WorktreeClean))

		writeFile(filepath.Join(pricing, "pricing.go"), "package pricing\n\nfunc Price() int { return 2 }\n")
		changed := indexOnce(ctx, engine, shop)
		linked, err := storage.ModuleDependencies(ctx, database, uuid.MustParse(changed.SnapshotID))
		Expect(err).ToNot(HaveOccurred())
		Expect(linked.Items[0].TargetSnapshotID).ToNot(Equal(original.Items[0].TargetSnapshotID))
		Expect(loadSnapshot(database, changed.SnapshotID).WorktreeState).To(Equal(storage.WorktreeDirty))
		Expect(loadSnapshot(database, changed.SnapshotID).Revision).To(HavePrefix("git-"))
		Expect(loadSnapshot(database, changed.SnapshotID).Revision).To(ContainSubstring("-dirty-"))
		Expect(strings.HasSuffix(loadSnapshot(database, changed.SnapshotID).Revision, "Z")).To(BeTrue())
		writeFile(filepath.Join(pricing, "pricing.go"), "package pricing\n\nfunc Price() int { return 3 }\n")
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		content, err := pipeline.ReadModuleSource(ctx, linked.Items[0].TargetSnapshotID.String(), "pricing.go")
		Expect(err).ToNot(HaveOccurred())
		Expect(content.Origin).To(Equal("snapshot"))
		Expect(content.Content).To(ContainSubstring("return 2"))
	})

	It("keeps syntax and unresolved declarations when a historical commit cannot load packages", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		checkout := canonicalTempDir()
		writeFile(filepath.Join(checkout, "go.mod"), "module example.org/history\n\ngo 1.26\n")
		writeFile(filepath.Join(checkout, "history.go"), "package history\n\nfunc Present() {}\n")
		for _, args := range [][]string{{"init"}, {"add", "go.mod", "history.go"}, {"-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "-m", "test: base"}} {
			output, err := exec.CommandContext(ctx, "git", append([]string{"-C", checkout}, args...)...).CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(output))
		}
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		indexOnce(ctx, engine, checkout)
		writeFile(filepath.Join(checkout, "go.mod"), "module example.org/history\n\ngo 1.26\n\nrequire example.org/missing v1.0.0\nreplace example.org/missing => ../missing\n")
		writeFile(filepath.Join(checkout, "history.go"), "package history\n\nimport \"example.org/missing\"\n\nfunc Present() { missing.Use() }\n")
		for _, args := range [][]string{{"add", "go.mod", "history.go"}, {"-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "-m", "test: missing dependency"}} {
			output, err := exec.CommandContext(ctx, "git", append([]string{"-C", checkout}, args...)...).CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(output))
		}
		commit, err := exec.CommandContext(ctx, "git", "-C", checkout, "rev-parse", "HEAD").Output()
		Expect(err).ToNot(HaveOccurred())
		GinkgoT().Setenv("GOPROXY", "off")
		indexed, err := engine.IndexRevision(ctx, RevisionOptions{RootKey: "example.org/history", Checkout: checkout, Commit: strings.TrimSpace(string(commit)), Reason: storage.ReasonHistorical})
		Expect(err).ToNot(HaveOccurred())
		Expect(loadSnapshot(database, indexed.SnapshotID).Coverage).To(Equal(storage.CoverageSyntax))
		dependencies, err := storage.ModuleDependencies(ctx, database, uuid.MustParse(indexed.SnapshotID))
		Expect(err).ToNot(HaveOccurred())
		Expect(dependencies.Items).To(HaveLen(1))
		Expect(dependencies.Items[0].ReplacePath).To(Equal("../missing"))
		Expect(dependencies.Items[0].TargetSnapshotID).To(BeNil())
		Expect(dependencies.Items[0].UnresolvedReason).ToNot(BeEmpty())
	})

	It("indexes a registered local tag as an immutable selected-version snapshot", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		checkout := GinkgoT().TempDir()
		writeFile(filepath.Join(checkout, "go.mod"), "module example.org/pricing\n\ngo 1.26\n")
		writeFile(filepath.Join(checkout, "pricing.go"), "package pricing\n\nfunc Price() int { return 1 }\n")
		for _, args := range [][]string{{"init"}, {"add", "go.mod", "pricing.go"}, {"-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "-m", "test: tagged module"}, {"tag", "v1.2.3"}} {
			output, err := exec.CommandContext(ctx, "git", append([]string{"-C", checkout}, args...)...).CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(output))
		}
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		indexOnce(ctx, engine, checkout)
		version, err := engine.versionedSnapshot(ctx, "example.org/pricing", "v1.2.3", false)
		Expect(err).ToNot(HaveOccurred())
		Expect(version.ModuleVersion).To(Equal("v1.2.3"))
		Expect(version.WorktreeState).To(Equal(storage.WorktreeClean))
		Expect(version.GitCommit).To(HaveLen(40))
		again, err := engine.versionedSnapshot(ctx, "example.org/pricing", "v1.2.3", false)
		Expect(err).ToNot(HaveOccurred())
		Expect(again.ID).To(Equal(version.ID))
	})
})
