package indexer

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("module discovery", func() {
	It("uses the go.mod module path as the root identity", func(ctx SpecContext) {
		workspace := GinkgoT().TempDir()
		goMod := filepath.Join(workspace, "go.mod")
		Expect(os.WriteFile(goMod, []byte("module example.org/first\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n\nfunc Run() {}\n"), 0o644)).To(Succeed())

		first, err := discoverModules(ctx, workspace, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(first).To(HaveLen(1))
		Expect(first[0].RootKey).To(Equal("example.org/first"))
		Expect(first[0].Files[0].PackagePath).To(Equal("example.org/first"))

		Expect(os.WriteFile(goMod, []byte("module example.org/second\n\ngo 1.26\n"), 0o644)).To(Succeed())
		second, err := discoverModules(ctx, workspace, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(second[0].RootKey).To(Equal("example.org/second"))
		Expect(second[0].Files[0].PackagePath).To(Equal("example.org/second"))
		Expect(second[0].ContentSetHash).ToNot(Equal(first[0].ContentSetHash))
	})
})
