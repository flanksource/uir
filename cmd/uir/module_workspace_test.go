package main

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("module workspace uses", func() {
	It("adds external go.work modules only with --all", func() {
		workspace := GinkgoT().TempDir()
		app := filepath.Join(workspace, "app")
		other := filepath.Join(workspace, "other")
		Expect(os.MkdirAll(app, 0o755)).To(Succeed())
		Expect(os.MkdirAll(other, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(app, "go.mod"), []byte("module example.org/app\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(other, "go.mod"), []byte("module example.org/other\n\ngo 1.26\n"), 0o644)).To(Succeed())
		canonicalApp, err := filepath.EvalSymlinks(app)
		Expect(err).ToNot(HaveOccurred())
		canonicalOther, err := filepath.EvalSymlinks(other)
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(workspace, "go.work"), []byte("go 1.26.0\n\nuse (\n./app\n./other\n)\n"), 0o644)).To(Succeed())
		selected, err := addPaths(addPathOptions{Path: app})
		Expect(err).ToNot(HaveOccurred())
		Expect(selected).To(Equal([]string{canonicalApp}))
		all, err := addPaths(addPathOptions{Path: app, All: true})
		Expect(err).ToNot(HaveOccurred())
		Expect(all).To(Equal([]string{canonicalApp, canonicalOther}))
		packageDir := filepath.Join(app, "internal")
		Expect(os.MkdirAll(packageDir, 0o755)).To(Succeed())
		packagePaths, err := addPaths(addPathOptions{Path: packageDir, All: true})
		Expect(err).ToNot(HaveOccurred())
		canonicalPackage, err := filepath.EvalSymlinks(packageDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(packagePaths).To(Equal([]string{canonicalPackage, canonicalOther}))
	})
})
