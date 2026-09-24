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

var _ = Describe("compact symbol index queries", func() {
	DescribeTable("keeps results in the selected checkout and historical snapshot", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		primary, branch := shopWorkspace()
		first := indexCheckout(ctx, database, primary)
		indexCheckout(ctx, database, branch)
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())

		primaryScope := query.ModuleScopeOptions{RootKey: shopModule, Location: primary}
		incoming := runQuery(ctx, pipeline, "store.Store.Save <", primaryScope)
		Expect(occurrenceRows(incoming.Matches)).To(Equal([]occurrenceRow{
			{Kind: "caller", Location: "primary", Path: "app/app.go", Line: 7, Column: 14, EndLine: 7, EndColumn: 18, Role: "call", Name: "Run"},
			{Kind: "caller", Location: "primary", Path: "app/app.go", Line: 13, Column: 46, EndLine: 13, EndColumn: 50, Role: "call", Name: "Direct"},
			{Kind: "caller", Location: "primary", Path: "store/store.go", Line: 16, Column: 15, EndLine: 16, EndColumn: 19, Role: "call", Name: "Persist", Dispatch: true},
		}))
		Expect(incoming.Declarations).To(HaveLen(1))
		Expect(incoming.Coverage).To(BeEmpty())
		Expect(runQuery(ctx, pipeline, "store.Store.Save =", primaryScope).Matches).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "store.Saver :impl", primaryScope).Matches).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "store.Store :methods", primaryScope).Matches).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "store.Store.count ~w", primaryScope).Matches).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "app.Run >", primaryScope).Matches).To(HaveLen(2))
		Expect(runQuery(ctx, pipeline, "store.Store.Save <", query.ModuleScopeOptions{RootKey: shopModule, Location: branch}).Total).To(Equal(4))

		limited := runQuery(ctx, pipeline, "store.Store.Save <", query.ModuleScopeOptions{RootKey: shopModule, Location: primary, Limit: 2})
		Expect(limited.Total).To(Equal(3))
		Expect(limited.Matches).To(Equal(incoming.Matches[:2]))
		Expect(os.WriteFile(filepath.Join(primary, "app", "app.go"), []byte(shopRun), 0o644)).To(Succeed())
		indexCheckout(ctx, database, primary)
		Expect(runQuery(ctx, pipeline, "store.Store.Save < +pkg app", primaryScope).Total).To(Equal(1))
		historical := runQuery(ctx, pipeline, "store.Store.Save < +pkg app", query.ModuleScopeOptions{SnapshotID: first.SnapshotID})
		Expect(historical.Total).To(Equal(2))
		Expect(historical.Matches[0].SnapshotID).To(Equal(first.SnapshotID))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))

	It("returns candidates for a suffix shared by two indexed roots", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		workspace := GinkgoT().TempDir()
		for _, module := range []string{"example.org/one", "example.org/two"} {
			path := filepath.Join(workspace, filepath.Base(module))
			Expect(os.MkdirAll(filepath.Join(path, "run"), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(path, "go.mod"), []byte("module "+module+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(path, "run", "run.go"), []byte("package run\n\nfunc Run() {}\n"), 0o644)).To(Succeed())
			indexCheckout(ctx, database, path)
		}
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		candidates := runQuery(ctx, pipeline, "run.Run <", query.ModuleScopeOptions{})
		Expect(candidates.Matches).To(HaveLen(2))
		Expect(candidates.Matches[0].Kind).To(Equal("candidate"))
		Expect(candidates.Symbols[0].QueryName).To(Equal("example.org/one/run.Run"))
		Expect(runQuery(ctx, pipeline, "example.org/one/run.Run =", query.ModuleScopeOptions{}).Matches).To(HaveLen(1))
	})
})
