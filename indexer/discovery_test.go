package indexer

import (
	"os"
	"os/exec"
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

	It("respects nested Git ignore rules for sources and module roots", func(ctx SpecContext) {
		workspace := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(workspace, "ignored"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(workspace, "internal"), 0o755)).To(Succeed())
		writeFile(filepath.Join(workspace, "go.mod"), "module example.org/app\n\ngo 1.26\n")
		writeFile(filepath.Join(workspace, ".gitignore"), "ignored/\n*.gen.go\n")
		writeFile(filepath.Join(workspace, "main.go"), "package app\n")
		writeFile(filepath.Join(workspace, "main.gen.go"), "package app\n")
		writeFile(filepath.Join(workspace, "ignored", "go.mod"), "module example.org/ignored\n\ngo 1.26\n")
		writeFile(filepath.Join(workspace, "ignored", "ignored.go"), "package ignored\n")
		writeFile(filepath.Join(workspace, "internal", ".gitignore"), "skip.go\n!keep.gen.go\n")
		writeFile(filepath.Join(workspace, "internal", "skip.go"), "package internal\n")
		writeFile(filepath.Join(workspace, "internal", "keep.gen.go"), "package internal\n")
		writeFile(filepath.Join(workspace, "internal", "plain.go"), "package internal\n")
		Expect(exec.Command("git", "-C", workspace, "init", "-q").Run()).To(Succeed())

		roots, err := discoverModules(ctx, workspace, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(roots).To(HaveLen(1))
		keys := make([]string, 0, len(roots[0].Files))
		for _, file := range roots[0].Files {
			keys = append(keys, file.PathKey)
		}
		Expect(keys).To(Equal([]string{"internal/keep.gen.go", "internal/plain.go", "main.go"}))
	})

	It("discovers a nested Git module inside an untracked directory", func(ctx SpecContext) {
		workspace := GinkgoT().TempDir()
		nested := filepath.Join(workspace, "nested")
		Expect(os.Mkdir(nested, 0o755)).To(Succeed())
		writeFile(filepath.Join(workspace, "go.mod"), "module example.org/app\n\ngo 1.26\n")
		writeFile(filepath.Join(workspace, "main.go"), "package app\n")
		writeFile(filepath.Join(nested, "go.mod"), "module example.org/nested\n\ngo 1.26\n")
		writeFile(filepath.Join(nested, "nested.go"), "package nested\n")
		Expect(exec.Command("git", "-C", workspace, "init", "-q").Run()).To(Succeed())
		Expect(exec.Command("git", "-C", nested, "init", "-q").Run()).To(Succeed())

		roots, err := discoverModules(ctx, workspace, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(roots).To(HaveLen(2))
		Expect([]string{roots[0].RootKey, roots[1].RootKey}).To(Equal([]string{"example.org/app", "example.org/nested"}))
		Expect(roots[1].Files).To(HaveLen(1))
	})
})
