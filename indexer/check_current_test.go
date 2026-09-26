package indexer

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("checking an indexed checkout", func() {
	It("returns the current snapshot and rejects changed source and manifests", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		root := GinkgoT().TempDir()
		module := filepath.Join(root, "go.mod")
		source := filepath.Join(root, "main.go")
		writeFile(module, "module example.org/current\n\ngo 1.26\n")
		writeFile(source, "package current\n\nfunc Run() {}\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		indexed, err := engine.IndexModules(ctx, ModuleOptions{Path: root, IncludeTests: true})
		Expect(err).ToNot(HaveOccurred())
		current, err := engine.CheckCurrent(ctx, root, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(current.SnapshotID).To(Equal(indexed[0].SnapshotID))
		writeFile(source, "package current\n\nfunc Run() { Run() }\n")
		_, err = engine.CheckCurrent(ctx, root, true)
		Expect(err).To(MatchError(ContainSubstring("stale")))
		writeFile(source, "package current\n\nfunc Run() {}\n")
		Expect(os.WriteFile(module, []byte("module example.org/current\n\ngo 1.26\n\n// changed\n"), 0o644)).To(Succeed())
		_, err = engine.CheckCurrent(ctx, root, true)
		Expect(err).To(MatchError(ContainSubstring("stale")))
	})

	It("requires an indexed head with complete coverage", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		root := GinkgoT().TempDir()
		writeFile(filepath.Join(root, "go.mod"), "module example.org/coverage\n\ngo 1.26\n")
		writeFile(filepath.Join(root, "main.go"), "package coverage\n\nvar _ = missing\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		_, err = engine.CheckCurrent(ctx, root, true)
		Expect(err).To(MatchError(ContainSubstring("no indexed head")))
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: root, IncludeTests: true})
		Expect(err).ToNot(HaveOccurred())
		_, err = engine.CheckCurrent(ctx, root, true)
		Expect(err).To(MatchError(ContainSubstring("incomplete")))
	})
})
