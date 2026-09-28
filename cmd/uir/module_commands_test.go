package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("root-level module commands", func() {
	It("registers add, list, get, query, and reindex at the root", func() {
		root := newRootCommand(&commandRuntime{})
		Expect(commandNames(root)).To(ContainElements("add", "list", "get", "query", "reindex"))
	})
	It("rejects a path combined with reindex all", func(ctx context.Context) {
		root := newRootCommand(&commandRuntime{})
		root.SetArgs([]string{"reindex", "--all", "/checkout"})
		Expect(root.ExecuteContext(ctx)).To(MatchError(ContainSubstring("reindex --all does not accept a path")))
	})
	It("rejects force combined with reindex all", func(ctx context.Context) {
		root := newRootCommand(&commandRuntime{})
		root.SetArgs([]string{"reindex", "--all", "--force"})
		Expect(root.ExecuteContext(ctx)).To(MatchError(ContainSubstring("reindex --all does not accept --force")))
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

	It("warns for an unscoped query with a missing head and rejects explicit scopes", func(ctx context.Context) {
		database := openCommandDatabase(ctx)
		now := time.Now().UTC()
		root := storage.ModuleRoot{ID: uuid.New(), RootKey: "example.org/registered", Name: "registered", CreatedAt: now}
		Expect(storage.CreateModuleRoot(ctx, database, &root)).To(Succeed())
		checkout, err := filepath.EvalSymlinks(GinkgoT().TempDir())
		Expect(err).ToNot(HaveOccurred())
		location := storage.ModuleLocation{ID: uuid.New(), RootID: root.ID, CanonicalPath: checkout, Kind: "module", CreatedAt: now}
		Expect(database.Create(&location).Error).To(Succeed())
		Expect(database.Create(&storage.ModulePrimary{RootID: root.ID, LocationID: location.ID}).Error).To(Succeed())

		rows, err := listModuleRoots(ctx, database)
		Expect(err).ToNot(HaveOccurred())
		Expect(rows).To(Equal([]moduleRootRow{{RootKey: root.RootKey, Name: root.Name, Location: location.CanonicalPath}}),
			"a root the pre-handle cutover kept is listed with no snapshot until it is reindexed")
		result, err := queryModules(ctx, database, moduleQueryOptions{Expression: "registered.Run"})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Total).To(Equal(0))
		Expect(result.Warnings).To(Equal([]query.MissingHeadWarning{{RootKey: root.RootKey, Location: checkout,
			Message: "registered checkout has no indexed head; run `uir reindex --all`"}}))
		for _, options := range []moduleQueryOptions{{Expression: "registered.Run", RootKey: root.RootKey}, {Expression: "registered.Run", Location: location.CanonicalPath}} {
			_, err = queryModules(ctx, database, options)
			Expect(err).To(MatchError(ContainSubstring("is registered but not indexed; run `uir reindex`")), "%+v", options)
		}
	})

	It("reindexes only registered checkouts without heads when all is requested", func(ctx context.Context) {
		database := openCommandDatabase(ctx)
		workspace := GinkgoT().TempDir()
		for _, name := range []string{"indexed", "missing"} {
			checkout := filepath.Join(workspace, name)
			Expect(os.Mkdir(checkout, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module example.org/"+name+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(checkout, "main.go"), []byte("package "+name+"\n\nfunc Run() {}\n"), 0o644)).To(Succeed())
			if name == "indexed" {
				_, err := addModules(ctx, database, checkout, false)
				Expect(err).ToNot(HaveOccurred())
				continue
			}
			root := storage.ModuleRoot{ID: uuid.New(), RootKey: "example.org/missing", Name: name, CreatedAt: time.Now().UTC()}
			Expect(storage.CreateModuleRoot(ctx, database, &root)).To(Succeed())
			canonical, err := filepath.EvalSymlinks(checkout)
			Expect(err).ToNot(HaveOccurred())
			location := storage.ModuleLocation{ID: uuid.New(), RootID: root.ID, CanonicalPath: canonical, Kind: "module", CreatedAt: time.Now().UTC()}
			Expect(database.Create(&location).Error).To(Succeed())
			Expect(database.Create(&storage.ModulePrimary{RootID: root.ID, LocationID: location.ID}).Error).To(Succeed())
		}
		nested := filepath.Join(workspace, "missing", "nested")
		Expect(os.Mkdir(nested, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module example.org/nested\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(nested, "nested.go"), []byte("package nested\n\nfunc Run() {}\n"), 0o644)).To(Succeed())
		results, err := reindexAllModules(ctx, database, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(1))
		Expect(results[0].RootKey).To(Equal("example.org/missing"))
		Expect(results[0].SnapshotID).ToNot(BeEmpty())
		roots, err := listModuleRoots(ctx, database)
		Expect(err).ToNot(HaveOccurred())
		Expect(roots).To(HaveLen(2), "nested unregistered modules remain outside reindex --all")
		again, err := reindexAllModules(ctx, database, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(again).To(BeEmpty())
	})

	It("returns the references envelope with positioned rows, declarations, symbols, and partial coverage", func(ctx context.Context) {
		database := openCommandDatabase(ctx)
		workspace := writeReferencesModule()
		indexed, err := addModules(ctx, database, workspace, false)
		Expect(err).ToNot(HaveOccurred())
		result, err := queryModules(ctx, database, moduleQueryOptions{Expression: `store.Store.Save <`, RootKey: referencesRoot})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Operation).To(Equal(query.OperationIncoming))
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
		result, err := queryModules(ctx, database, moduleQueryOptions{Expression: `store.Store.Save`, RootKey: referencesRoot})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Operation).To(Equal(query.OperationResolve))
		Expect(result.Total).To(Equal(1))
		Expect(result.Matches).To(ConsistOf(And(
			HaveField("Kind", "symbol"), HaveField("Path", "store/store.go"), HaveField("Role", "definition"), HaveField("Source", "store/store.go:5:14"),
		)))
		Expect(result.Declarations).ToNot(BeNil())
		Expect(result.Symbols).ToNot(BeNil())
		Expect(result.Coverage).To(HaveLen(1))
	})
	It("runs structured flags with and without a positional expression", func(ctx context.Context) {
		database := openCommandDatabase(ctx)
		_, err := addModules(ctx, database, writeReferencesModule(), false)
		Expect(err).NotTo(HaveOccurred())
		methods, err := queryModules(ctx, database, moduleQueryOptions{Methods: true, RootKey: referencesRoot})
		Expect(err).NotTo(HaveOccurred())
		Expect(methods.Matches).To(ContainElement(HaveField("Symbol", "method:"+referencesRoot+"/store.Store:Save#()")))
		callers, err := queryModules(ctx, database, moduleQueryOptions{Expression: "store.Store.Save", Methods: true, Callers: true, RootKey: referencesRoot})
		Expect(err).NotTo(HaveOccurred())
		Expect(callers.Total).To(Equal(1))
		Expect(callers.Matches[0].Relation).To(Equal("<"))
		Expect(callers.Matches[0].SourceName).To(ContainSubstring("Store.Save"))
		modules, err := queryModules(ctx, database, moduleQueryOptions{Modules: true, Packages: true, RootKey: referencesRoot})
		Expect(err).NotTo(HaveOccurred())
		Expect(modules.Matches).To(ContainElement(HaveField("Kind", "module")))
		Expect(modules.Matches).To(ContainElement(HaveField("Kind", "package")))
		_, err = queryModules(ctx, database, moduleQueryOptions{RootKey: referencesRoot})
		Expect(err).To(MatchError(ContainSubstring("expression or structured flag")))
	})
	It("accepts structured flags from the CLI without a positional expression", func(ctx context.Context) {
		database := openCommandDatabase(ctx)
		_, err := addModules(ctx, database, writeReferencesModule(), false)
		Expect(err).NotTo(HaveOccurred())
		runtime := &commandRuntime{database: database}
		root := newRootCommand(runtime)
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs([]string{"query", "--methods", "--root", referencesRoot, "--format", "json"})
		Expect(root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))).To(Succeed())
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
