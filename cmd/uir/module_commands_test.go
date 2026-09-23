package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("root-level module commands", func() {
	It("registers add, list, get, query, and reindex at the root", func() {
		root := newRootCommand(&commandRuntime{})
		Expect(commandNames(root)).To(ContainElements("add", "list", "get", "query", "reindex"))
	})

	It("adds a module immediately and keeps its primary location on reindex", func(ctx context.Context) {
		database := openCommandDatabase(ctx)
		workspace := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.org/cli\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package cli\n\nfunc Run() {}\n"), 0o644)).To(Succeed())
		results, err := addModules(ctx, database, workspace, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(1))
		Expect(results[0].ParsedFiles).To(Equal(1))
		rows, err := listModuleRoots(ctx, database)
		Expect(err).ToNot(HaveOccurred())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].RootKey).To(Equal("example.org/cli"))
		Expect(rows[0].SnapshotID).To(Equal(results[0].SnapshotID))
		var primary storage.ModulePrimary
		Expect(database.First(&primary).Error).To(Succeed())
		again, err := addModules(ctx, database, workspace, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(again[0].Unchanged).To(BeTrue())
		Expect(again[0].SnapshotID).To(Equal(results[0].SnapshotID))
	})

})
