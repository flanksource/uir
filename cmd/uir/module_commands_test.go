package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/query"
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

	It("returns the references envelope with positioned rows, declarations, symbols, and partial coverage", func(ctx context.Context) {
		database := openCommandDatabase(ctx)
		workspace := writeReferencesModule()
		indexed, err := addModules(ctx, database, workspace, false)
		Expect(err).ToNot(HaveOccurred())
		result, err := queryModules(ctx, database, moduleQueryOptions{Expression: `references of node where type = "Store" and method = "Save"`, RootKey: referencesRoot})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Operation).To(Equal(query.OperationReferences))
		Expect(result.Total).To(Equal(1))
		Expect(result.Matches).To(HaveLen(1))
		match := result.Matches[0]
		Expect(match.Symbol).To(Equal("method:"+referencesRoot+"/app:Run#()"), "symbol stays the enclosing declaration's symbol key")
		Expect(match.Source).To(Equal("app/app.go:5:28"))
		Expect(match.Root).To(Equal(referencesRoot))
		Expect(match.SnapshotID).To(Equal(indexed[0].SnapshotID))
		Expect([]any{match.Path, *match.Line, *match.Column, *match.EndLine, *match.EndColumn, match.Role, match.Coverage, match.Dispatch}).
			To(Equal([]any{"app/app.go", 5, 28, 5, 32, "call", "indexed", false}))
		Expect(match.SymbolID).To(Equal(result.Symbols[0].ID))
		Expect(match.EnclosingID).To(HaveLen(64))
		Expect(match.EnclosingKey).To(ContainSubstring(`"Run"`))
		Expect(result.Declarations).To(HaveLen(1))
		declaration := result.Declarations[0]
		Expect([]any{declaration.Kind, declaration.Path, *declaration.Line, *declaration.Column, declaration.Role, declaration.SymbolID}).
			To(Equal([]any{"definition", "store/store.go", 5, 14, "definition", match.SymbolID}))
		Expect(result.Symbols).To(ConsistOf(And(
			HaveField("Kind", "method"), HaveField("Owner", "Store"), HaveField("Name", "Save"), HaveField("Visibility", "exported"),
			HaveField("ModuleKey", referencesRoot), HaveField("PackagePath", referencesRoot+"/store"),
		)))
		Expect(result.Coverage).To(ConsistOf(query.ModuleCoverage{
			RootKey: referencesRoot, Location: match.Location, SnapshotID: match.SnapshotID, PackagePath: referencesRoot + "/broken", Coverage: "partial", Diagnostics: 1,
		}))
		Expect(result.Stages).To(ContainElement(query.ResolutionStage{Name: "coverage", Value: "incomplete: 1 package is not fully indexed (1 partial)"}))
	})

	It("returns search rows in the same envelope with every collection present", func(ctx context.Context) {
		database := openCommandDatabase(ctx)
		_, err := addModules(ctx, database, writeReferencesModule(), false)
		Expect(err).ToNot(HaveOccurred())
		result, err := queryModules(ctx, database, moduleQueryOptions{Expression: `search "Store.Sa"`, RootKey: referencesRoot})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Operation).To(Equal(query.OperationSearch))
		Expect(result.Total).To(Equal(1))
		Expect(result.Matches).To(ConsistOf(And(
			HaveField("Kind", "symbol"), HaveField("Path", "store/store.go"), HaveField("Role", "definition"), HaveField("Source", "store/store.go:5:14"),
		)))
		Expect(result.Declarations).ToNot(BeNil())
		Expect(result.Symbols).ToNot(BeNil())
		Expect(result.Coverage).To(HaveLen(1))
	})
})

const referencesRoot = "example.org/refs"

// writeReferencesModule writes a module whose app package calls store.Store.Save and whose broken
// package does not type-check, so its coverage is partial.
func writeReferencesModule() string {
	GinkgoHelper()
	workspace := GinkgoT().TempDir()
	files := map[string]string{
		"go.mod":           "module " + referencesRoot + "\n\ngo 1.26\n",
		"store/store.go":   "package store\n\ntype Store struct{}\n\nfunc (Store) Save() {}\n",
		"app/app.go":       "package app\n\nimport \"" + referencesRoot + "/store\"\n\nfunc Run() { store.Store{}.Save() }\n",
		"broken/broken.go": "package broken\n\nfunc Wrong() int { return \"text\" }\n",
	}
	for path, content := range files {
		Expect(os.MkdirAll(filepath.Dir(filepath.Join(workspace, path)), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspace, path), []byte(content), 0o644)).To(Succeed())
	}
	return workspace
}
