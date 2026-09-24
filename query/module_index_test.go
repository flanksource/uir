package query_test

import (
	"context"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const (
	shopModule = "example.org/shop"
	shopStore  = "package store\n" +
		"\n" +
		"// Saver persists named records.\n" +
		"type Saver interface {\n" +
		"\tSave(name string) error\n" +
		"}\n" +
		"\n" +
		"type Store struct{ count int }\n" +
		"\n" +
		"func (s *Store) Save(name string) error {\n" +
		"\ts.count++\n" +
		"\treturn nil\n" +
		"}\n" +
		"\n" +
		"func Persist(saver Saver, name string) error {\n" +
		"\treturn saver.Save(name)\n" +
		"}\n"
	shopRun = "package app\n" +
		"\n" +
		"import \"example.org/shop/store\"\n" +
		"\n" +
		"func Run() error {\n" +
		"\ts := &store.Store{}\n" +
		"\tif err := s.Save(\"a\"); err != nil {\n" +
		"\t\treturn err\n" +
		"\t}\n" +
		"\treturn store.Persist(s, \"b\")\n" +
		"}\n"
	shopDirect = "\nfunc Direct(s *store.Store) error { return s.Save(\"c\") }\n"
	shopAgain  = "\nfunc Again(s *store.Store) error { return s.Save(\"d\") }\n"
)

// occurrenceRow is the part of a match a test pins: where it is, what it is, and what encloses it.
type occurrenceRow struct {
	Kind      string
	Location  string
	Path      string
	Line      int
	Column    int
	EndLine   int
	EndColumn int
	Role      string
	Name      string
	Dispatch  bool
}

func occurrenceRows(matches []query.ModuleMatch) []occurrenceRow {
	GinkgoHelper()
	rows := make([]occurrenceRow, 0, len(matches))
	for _, match := range matches {
		Expect(match.Line).ToNot(BeNil(), "%s at %s has a position", match.Kind, match.Path)
		name := match.Identifier.Method
		if name == "" {
			name = match.Identifier.Type
		}
		rows = append(rows, occurrenceRow{
			Kind: match.Kind, Location: filepath.Base(match.Location), Path: match.Path,
			Line: *match.Line, Column: *match.Column, EndLine: *match.EndLine, EndColumn: *match.EndColumn,
			Role: match.Role, Name: name, Dispatch: match.Dispatch,
		})
	}
	return rows
}

// shopWorkspace writes two checkouts of the shop module; the branch adds one more call to Store.Save.
func shopWorkspace() (primary, branch string) {
	GinkgoHelper()
	workspace := GinkgoT().TempDir()
	primary, branch = filepath.Join(workspace, "primary"), filepath.Join(workspace, "branch")
	for checkout, app := range map[string]string{primary: shopRun + shopDirect, branch: shopRun + shopDirect + shopAgain} {
		for _, directory := range []string{"store", "app"} {
			Expect(os.MkdirAll(filepath.Join(checkout, directory), 0o755)).To(Succeed())
		}
		Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+shopModule+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(checkout, "store", "store.go"), []byte(shopStore), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(checkout, "app", "app.go"), []byte(app), 0o644)).To(Succeed())
	}
	return primary, branch
}

func indexCheckout(ctx context.Context, database *gorm.DB, path string) indexer.ModuleResult {
	GinkgoHelper()
	engine, err := indexer.New(database)
	Expect(err).ToNot(HaveOccurred())
	results, err := engine.IndexModules(ctx, indexer.ModuleOptions{Path: path})
	Expect(err).ToNot(HaveOccurred())
	Expect(results).To(HaveLen(1))
	return results[0]
}

func runQuery(ctx context.Context, pipeline *query.Pipeline, expression string, options query.ModuleScopeOptions) query.ModuleQueryResult {
	GinkgoHelper()
	result, err := pipeline.RunModules(ctx, expression, options)
	Expect(err).ToNot(HaveOccurred(), expression)
	return result
}

var _ = Describe("index-backed module queries", func() {
	DescribeTable("resolves references, definitions, implementations, callers, callees, and search on symbol postings",
		func(ctx SpecContext, backend string) {
			database := openQueryDatabase(ctx, backend)
			primary, branch := shopWorkspace()
			first := indexCheckout(ctx, database, primary)
			indexCheckout(ctx, database, branch)
			pipeline, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())
			allHeads := query.ModuleScopeOptions{RootKey: shopModule}
			primaryHead := query.ModuleScopeOptions{RootKey: shopModule, Location: primary}

			By("returning one row per reference at every head, never multiplied by declaration locations")
			references := runQuery(ctx, pipeline, `references of node where type = "Store" and method = "Save"`, allHeads)
			Expect(occurrenceRows(references.Matches)).To(Equal([]occurrenceRow{
				{Kind: "reference", Location: "branch", Path: "app/app.go", Line: 7, Column: 14, EndLine: 7, EndColumn: 18, Role: "call", Name: "Run"},
				{Kind: "reference", Location: "branch", Path: "app/app.go", Line: 13, Column: 46, EndLine: 13, EndColumn: 50, Role: "call", Name: "Direct"},
				{Kind: "reference", Location: "branch", Path: "app/app.go", Line: 15, Column: 45, EndLine: 15, EndColumn: 49, Role: "call", Name: "Again"},
				{Kind: "reference", Location: "primary", Path: "app/app.go", Line: 7, Column: 14, EndLine: 7, EndColumn: 18, Role: "call", Name: "Run"},
				{Kind: "reference", Location: "primary", Path: "app/app.go", Line: 13, Column: 46, EndLine: 13, EndColumn: 50, Role: "call", Name: "Direct"},
			}))
			Expect(references.Symbols).To(HaveLen(1))
			Expect(references.Symbols[0]).To(And(HaveField("Kind", "method"), HaveField("Owner", "Store"), HaveField("Name", "Save"), HaveField("ModuleKey", shopModule)))
			for _, match := range references.Matches {
				Expect(match.SymbolID).To(Equal(references.Symbols[0].ID))
			}
			Expect(occurrenceRows(references.Declarations)).To(Equal([]occurrenceRow{
				{Kind: "definition", Location: "branch", Path: "store/store.go", Line: 10, Column: 17, EndLine: 10, EndColumn: 21, Role: "definition", Name: "Save"},
				{Kind: "definition", Location: "primary", Path: "store/store.go", Line: 10, Column: 17, EndLine: 10, EndColumn: 21, Role: "definition", Name: "Save"},
			}))
			Expect(references.Coverage).To(BeEmpty())
			Expect(references.Stages).To(ContainElement(query.ResolutionStage{Name: "coverage", Value: "complete: every package in scope is indexed"}))

			By("listing the definition at every head")
			definitions := runQuery(ctx, pipeline, `definitions of node where type = "Store" and method = "Save"`, allHeads)
			Expect(occurrenceRows(definitions.Matches)).To(Equal(occurrenceRows(references.Declarations)))

			By("listing the types that implement an interface")
			implementations := runQuery(ctx, pipeline, `implementations of node where type = "Saver"`, primaryHead)
			Expect(occurrenceRows(implementations.Matches)).To(Equal([]occurrenceRow{
				{Kind: "implementation", Location: "primary", Path: "store/store.go", Line: 8, Column: 6, EndLine: 8, EndColumn: 11, Role: "implements", Name: "Store"},
			}))

			By("finding callers directly, and through the interface when dispatch is included")
			direct := []occurrenceRow{
				{Kind: "caller", Location: "primary", Path: "app/app.go", Line: 7, Column: 14, EndLine: 7, EndColumn: 18, Role: "call", Name: "Run"},
				{Kind: "caller", Location: "primary", Path: "app/app.go", Line: 13, Column: 46, EndLine: 13, EndColumn: 50, Role: "call", Name: "Direct"},
			}
			callers := runQuery(ctx, pipeline, `callers of node where type = "Store" and method = "Save"`, primaryHead)
			Expect(occurrenceRows(callers.Matches)).To(Equal(direct))
			dispatched := runQuery(ctx, pipeline, `callers of node where type = "Store" and method = "Save" including dispatch`, primaryHead)
			Expect(occurrenceRows(dispatched.Matches)).To(Equal(append(direct,
				occurrenceRow{Kind: "caller", Location: "primary", Path: "store/store.go", Line: 16, Column: 15, EndLine: 16, EndColumn: 19, Role: "call", Name: "Persist", Dispatch: true},
			)))
			Expect(dispatched.Stages).To(ContainElement(query.ResolutionStage{Name: "dispatch", Value: "1 interface method"}))

			By("listing the calls a function makes")
			callees := runQuery(ctx, pipeline, `callees of node where package = "example.org/shop/app" and method = "Run"`, primaryHead)
			Expect(occurrenceRows(callees.Matches)).To(Equal([]occurrenceRow{
				{Kind: "callee", Location: "primary", Path: "app/app.go", Line: 7, Column: 14, EndLine: 7, EndColumn: 18, Role: "call", Name: "Save"},
				{Kind: "callee", Location: "primary", Path: "app/app.go", Line: 10, Column: 15, EndLine: 10, EndColumn: 22, Role: "call", Name: "Persist"},
			}))
			Expect(callees.Matches[0].Identifier.Type).To(Equal("Store"))
			Expect(callees.Matches[0].SymbolID).To(Equal(references.Symbols[0].ID))

			By("searching names by prefix and by owner-qualified prefix")
			search := runQuery(ctx, pipeline, `search "sav"`, primaryHead)
			Expect(occurrenceRows(search.Matches)).To(Equal([]occurrenceRow{
				{Kind: "symbol", Location: "primary", Path: "store/store.go", Line: 4, Column: 6, EndLine: 4, EndColumn: 11, Role: "definition", Name: "Saver"},
				{Kind: "symbol", Location: "primary", Path: "store/store.go", Line: 5, Column: 2, EndLine: 5, EndColumn: 6, Role: "definition", Name: "Save"},
				{Kind: "symbol", Location: "primary", Path: "store/store.go", Line: 10, Column: 17, EndLine: 10, EndColumn: 21, Role: "definition", Name: "Save"},
			}))
			qualified := runQuery(ctx, pipeline, `search "Store.Sa"`, primaryHead)
			Expect(occurrenceRows(qualified.Matches)).To(Equal(occurrenceRows(search.Matches[2:])))
			Expect(qualified.Matches[0].SymbolID).To(Equal(references.Symbols[0].ID))

			By("bounding rows by the limit in the same stable order")
			limited := runQuery(ctx, pipeline, `references of node where type = "Store" and method = "Save"`, query.ModuleScopeOptions{RootKey: shopModule, Limit: 2})
			Expect(limited.Matches).To(Equal(references.Matches[:2]))
			Expect(limited.Total).To(Equal(5))
			Expect(runQuery(ctx, pipeline, `references of node where type = "Store" and method = "Save"`, allHeads)).To(Equal(references))

			By("reading a historical snapshot whose references differ from the head")
			Expect(os.WriteFile(filepath.Join(primary, "app", "app.go"), []byte(shopRun), 0o644)).To(Succeed())
			indexCheckout(ctx, database, primary)
			head := runQuery(ctx, pipeline, `references of node where type = "Store" and method = "Save"`, primaryHead)
			Expect(occurrenceRows(head.Matches)).To(Equal(occurrenceRows(references.Matches[3:4])))
			historical := runQuery(ctx, pipeline, `references of node where type = "Store" and method = "Save"`, query.ModuleScopeOptions{SnapshotID: first.SnapshotID})
			Expect(occurrenceRows(historical.Matches)).To(Equal(occurrenceRows(references.Matches[3:])))
			Expect(historical.Matches[0].SnapshotID).To(Equal(first.SnapshotID))
		},
		Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))

	DescribeTable("returns candidates for a selector matching several symbols and reports incomplete coverage",
		func(ctx SpecContext, backend string) {
			database := openQueryDatabase(ctx, backend)
			workspace := GinkgoT().TempDir()
			primary, branch := filepath.Join(workspace, "primary"), filepath.Join(workspace, "branch")
			for checkout, run := range map[string]string{primary: "func Run(n int) int { return n }\n", branch: "func Run(n string) string { return n }\n"} {
				for _, directory := range []string{"run", "broken"} {
					Expect(os.MkdirAll(filepath.Join(checkout, directory), 0o755)).To(Succeed())
				}
				Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module example.org/twin\n\ngo 1.26\n"), 0o644)).To(Succeed())
				Expect(os.WriteFile(filepath.Join(checkout, "run", "run.go"), []byte("package run\n\n"+run), 0o644)).To(Succeed())
				Expect(os.WriteFile(filepath.Join(checkout, "broken", "broken.go"), []byte("package broken\n\nfunc Wrong() int { return \"text\" }\n"), 0o644)).To(Succeed())
			}
			indexCheckout(ctx, database, primary)
			indexCheckout(ctx, database, branch)
			pipeline, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())

			for _, expression := range []string{`definitions of node where method = "Run"`, `references of node where method = "Run"`} {
				result := runQuery(ctx, pipeline, expression, query.ModuleScopeOptions{RootKey: "example.org/twin"})
				Expect(occurrenceRows(result.Matches)).To(Equal([]occurrenceRow{
					{Kind: "candidate", Location: "branch", Path: "run/run.go", Line: 3, Column: 6, EndLine: 3, EndColumn: 9, Role: "definition", Name: "Run"},
					{Kind: "candidate", Location: "primary", Path: "run/run.go", Line: 3, Column: 6, EndLine: 3, EndColumn: 9, Role: "definition", Name: "Run"},
				}), expression)
				Expect(result.Matches[0].SymbolID).ToNot(Equal(result.Matches[1].SymbolID))
				Expect(result.Symbols).To(HaveLen(2))
				Expect(result.Coverage).To(ConsistOf(
					query.ModuleCoverage{RootKey: "example.org/twin", Location: result.Matches[0].Location, SnapshotID: result.Matches[0].SnapshotID, PackagePath: "example.org/twin/broken", Coverage: "partial", Diagnostics: 1},
					query.ModuleCoverage{RootKey: "example.org/twin", Location: result.Matches[1].Location, SnapshotID: result.Matches[1].SnapshotID, PackagePath: "example.org/twin/broken", Coverage: "partial", Diagnostics: 1},
				))
				Expect(result.Stages).To(ContainElement(query.ResolutionStage{Name: "coverage", Value: "incomplete: 2 packages are not fully indexed (2 partial)"}))
			}
			unique := runQuery(ctx, pipeline, `references of node where method = "Run"`, query.ModuleScopeOptions{RootKey: "example.org/twin", Location: primary})
			Expect(unique.Matches).To(BeEmpty())
			Expect(unique.Coverage).To(HaveLen(1), "an empty result still says which packages were not proven")
		},
		Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))

	DescribeTable("rejects selectors and flags the index cannot answer",
		func(ctx SpecContext, expression, message string) {
			database := openQueryDatabase(ctx, "sqlite")
			primary, _ := shopWorkspace()
			indexCheckout(ctx, database, primary)
			pipeline, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())
			_, err = pipeline.RunModules(ctx, expression, query.ModuleScopeOptions{RootKey: shopModule})
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("a predicate symbols do not record", `references of node where signature = "()"`, `predicate "signature" is not supported by references`),
		Entry("a selector without a name", `references of node where package = "example.org/shop/app"`, "requires symbol_id, name, type, method, or field"),
		Entry("conflicting names", `definitions of node where method = "Run" and name = "Direct"`, `name "Direct" conflicts with "Run"`),
		Entry("an unknown symbol", `references of node where method = "Missing"`, "matched no symbols"),
		Entry("dispatch on a function", `callers of node where method = "Persist" including dispatch`, "dispatch applies to methods"),
		Entry("implementations of a method", `implementations of node where type = "Store" and method = "Save"`, "implementations requires a type"),
		Entry("a nested qualified search", `search "a.b.c"`, "at most one dot"),
		Entry("a search without searchable characters", `search "_-"`, "[a-z0-9]"),
		Entry("a node predicate on nodes", `nodes where name = "Run"`, `predicate "name" is not supported by nodes`),
	)
})
