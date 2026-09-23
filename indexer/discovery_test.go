package indexer

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Workspace discovery", func() {
	It("includes module-path changes in the revision set", func(ctx SpecContext) {
		workspace := GinkgoT().TempDir()
		goMod := filepath.Join(workspace, "go.mod")
		Expect(os.WriteFile(goMod, []byte("module example.org/first\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n\nfunc Run() {}\n"), 0o644)).To(Succeed())

		first, err := discoverWorkspace(ctx, Options{ProjectKey: "example", Path: workspace, RootKey: "app"})
		Expect(err).ToNot(HaveOccurred())
		Expect(first.Roots[0].Files[0].PackagePath).To(Equal("example.org/first"))

		Expect(os.WriteFile(goMod, []byte("module example.org/second\n\ngo 1.26\n"), 0o644)).To(Succeed())
		second, err := discoverWorkspace(ctx, Options{ProjectKey: "example", Path: workspace, RootKey: "app"})
		Expect(err).ToNot(HaveOccurred())
		Expect(second.Roots[0].Files[0].PackagePath).To(Equal("example.org/second"))
		Expect(second.RevisionSetHash).ToNot(Equal(first.RevisionSetHash))
	})
})
