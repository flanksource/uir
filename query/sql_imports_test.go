package query_test

import (
	"os"
	"path/filepath"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const importsRoot = "example.org/imports/a"

// importsWorkspace copies the three-module testdata workspace, whose modules a, b and c are joined by
// local replace directives, so indexing needs no network.
func importsWorkspace() string {
	GinkgoHelper()
	workspace := GinkgoT().TempDir()
	Expect(os.CopyFS(workspace, os.DirFS("testdata/imports"))).To(Succeed())
	return workspace
}

func indexImports(ctx SpecContext, database *gorm.DB, workspace string) string {
	GinkgoHelper()
	engine, err := indexer.New(database)
	Expect(err).ToNot(HaveOccurred())
	results, err := engine.IndexModules(ctx, indexer.ModuleOptions{Path: filepath.Join(workspace, "a"), Reason: storage.ReasonAdd})
	Expect(err).ToNot(HaveOccurred())
	for _, result := range results {
		if result.RootKey == importsRoot {
			return result.SnapshotID
		}
	}
	Fail("no snapshot of " + importsRoot)
	return ""
}

var _ = Describe("package_imports", func() {
	const columns = "SELECT path, file, package, count, direct, largest FROM package_imports() ORDER BY path"

	It("charges each package for the closure of its own direct third-party imports only", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		snapshot := indexImports(ctx, database, importsWorkspace())

		result := runQuery(ctx, pipeline, columns, query.ModuleScopeOptions{SnapshotID: snapshot})

		Expect(result.Rows).To(Equal([]map[string]any{
			{"path": ".", "file": "a.go", "package": importsRoot, "count": 0, "direct": 0, "largest": ""},
			{"path": "p1", "file": "p1/p1.go", "package": importsRoot + "/p1", "count": 4, "direct": 1, "largest": "example.org/imports/b/bpkg (4)"},
			{"path": "p2", "file": "p2/p2.go", "package": importsRoot + "/p2", "count": 1, "direct": 1, "largest": "example.org/imports/c/cother (1)"},
			{"path": "p3", "file": "p3/p3.go", "package": importsRoot + "/p3", "count": 0, "direct": 0, "largest": ""},
			{"path": "p4", "file": "p4/doc.go", "package": importsRoot + "/p4", "count": 4, "direct": 2, "largest": "example.org/imports/b/bpkg (4), example.org/imports/c/cpkg (2)"},
		}))
	})

	It("filters on the budget and attributes the row to the package file", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		snapshot := indexImports(ctx, database, importsWorkspace())

		result := runQuery(ctx, pipeline, "SELECT path FROM package_imports() WHERE count >= 4 ORDER BY path", query.ModuleScopeOptions{SnapshotID: snapshot})

		Expect(result.Rows).To(Equal([]map[string]any{{"path": "p1"}, {"path": "p4"}}))
		Expect(result.Matches).To(HaveLen(2))
		Expect(result.Matches[0].Path).To(Equal("p1/p1.go"))
		Expect(result.Matches[0].PackagePath).To(Equal(importsRoot + "/p1"))
	})

	It("recounts after a replaced module gains an import that changes no export shape", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		workspace := importsWorkspace()
		indexImports(ctx, database, workspace)
		source := "package bpkg\n\nimport (\n\t_ \"example.org/imports/b/bleaf\"\n\t_ \"example.org/imports/c/cother\"\n\t_ \"example.org/imports/c/cpkg\"\n)\n"
		Expect(os.WriteFile(filepath.Join(workspace, "b", "bpkg", "bpkg.go"), []byte(source), 0o600)).To(Succeed())

		snapshot := indexImports(ctx, database, workspace)
		result := runQuery(ctx, pipeline, "SELECT count FROM package_imports() WHERE path = 'p1'", query.ModuleScopeOptions{SnapshotID: snapshot})

		Expect(result.Rows).To(Equal([]map[string]any{{"count": 5}}))
	})

	It("refuses a snapshot that recorded no import graph", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		const root = "example.org/published-imports"
		published := publishedText("package sample\n")
		indexed, err := indexer.Publish(ctx, database, indexer.Publication{RootKey: root, Name: "imports", Location: indexer.ExternalLocation{URI: "test://imports"},
			Documents: []indexer.PublishedDocument{published.document("a.go", root, []storage.DocumentSymbol{}, nil)}})
		Expect(err).ToNot(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())

		_, err = pipeline.RunModules(ctx, columns, query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID})

		Expect(err).To(MatchError(And(ContainSubstring(indexed.SnapshotID), ContainSubstring("import graph"), ContainSubstring("reindex"))))
	})
})
