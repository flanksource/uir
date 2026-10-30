package query_test

import (
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func buildConstraintCheckout() string {
	GinkgoHelper()
	checkout := GinkgoT().TempDir()
	otherOS := "windows"
	if runtime.GOOS == otherOS {
		otherOS = "linux"
	}
	for path, source := range map[string]string{
		"go.mod":                           "module example.org/build-constraints\n\ngo 1.26\n",
		"main.go":                          "package sample\n",
		"hack/missing.go":                  "package hack\n",
		"hack/ignored.go":                  "//go:build ignore\n\npackage hack\n",
		"hack/valid.go":                    "//go:build hack\n\npackage hack\n",
		"hack/compound.go":                 "//go:build (hack || linux) && !linux\n\npackage hack\n",
		"hack/alternative.go":              "//go:build hack || " + otherOS + "\n\npackage hack\n",
		"hack/platform_" + otherOS + ".go": "package hack\n",
	} {
		Expect(os.MkdirAll(filepath.Dir(filepath.Join(checkout, path)), 0o700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(checkout, path), []byte(source), 0o600)).To(Succeed())
	}
	return checkout
}

var _ = Describe("SQL build constraints", func() {
	DescribeTable("reads external snapshot headers and preserves non-Go NULL semantics", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		const root = "example.org/published-build-constraints"
		goSource := publishedText("//go:build hack\n\npackage sample\n")
		nonGo := publishedText("//go:build hack\n")
		publication := indexer.Publication{RootKey: root, Name: "build-constraints", Location: indexer.ExternalLocation{URI: "test://build-constraints"},
			Documents: []indexer.PublishedDocument{goSource.document("hack/tool.go", root, []storage.DocumentSymbol{}, nil), nonGo.document("notes.txt", root, []storage.DocumentSymbol{}, nil)}}
		indexed, err := indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID}
		Expect(runQuery(ctx, pipeline, "SELECT path, file.build_constraint FROM files() ORDER BY path", scope).Rows).To(Equal([]map[string]any{{"path": "hack/tool.go", "file.build_constraint": "hack"}, {"path": "notes.txt", "file.build_constraint": nil}}))
		Expect(runQuery(ctx, pipeline, "SELECT path FROM files() WHERE file.build_constraint NOT REQUIRES 'hack'", scope).Total).To(BeZero())
		Expect(runQuery(ctx, pipeline, "SELECT path FROM files() WHERE file.build_constraint IS NULL", scope).Rows).To(Equal([]map[string]any{{"path": "notes.txt"}}))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))

	It("returns contextual errors for malformed published headers without affecting path-only queries", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		const root = "example.org/malformed-build-constraints"
		source := publishedText("//go:build hack &&\n\npackage sample\n")
		indexed, err := indexer.Publish(ctx, database, indexer.Publication{RootKey: root, Name: "malformed", Location: indexer.ExternalLocation{URI: "test://malformed-build"}, Documents: []indexer.PublishedDocument{source.document("hack/broken.go", root, []storage.DocumentSymbol{}, nil)}})
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID}
		Expect(runQuery(ctx, pipeline, "SELECT path FROM files()", scope).Rows).To(Equal([]map[string]any{{"path": "hack/broken.go"}}))
		_, err = pipeline.RunModules(ctx, "SELECT path FROM files() WHERE file.build_constraint NOT REQUIRES 'hack'", scope)
		Expect(err).To(MatchError(And(ContainSubstring("hack/broken.go"), ContainSubstring(indexed.SnapshotID), ContainSubstring("header line 1"))))
	})

	It("recovers clean historical headers from the pinned Git revision", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		checkout := sourcesCheckout(true)
		Expect(os.WriteFile(filepath.Join(checkout, "a.go"), []byte("//go:build hack\n\npackage sources\n"), 0o600)).To(Succeed())
		runGitIn(checkout, "add", "a.go")
		runGitIn(checkout, "-c", "user.name=test", "-c", "user.email=test@example.org", "commit", "--quiet", "-m", "build constraint")
		indexed := indexCheckout(ctx, database, checkout)
		Expect(os.WriteFile(filepath.Join(checkout, "a.go"), []byte("package sources\n"), 0o600)).To(Succeed())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		result := runQuery(ctx, pipeline, "SELECT path FROM files(include_excluded => TRUE) WHERE file.build_constraint REQUIRES 'hack'", query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID})
		Expect(result.Rows).To(Equal([]map[string]any{{"path": "a.go"}}))
	})

	DescribeTable("deduplicates cold header reads by hash and needs no source bytes on warm queries", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		const root = "example.org/build-io"
		source := publishedText("//go:build hack\n\npackage sample\n")
		publication := indexer.Publication{RootKey: root, Name: "build-io", Location: indexer.ExternalLocation{URI: "test://build-io"}}
		for _, path := range []string{"hack/a.go", "hack/b.go", "hack/c.go", "notes.txt"} {
			publication.Documents = append(publication.Documents, source.document(path, root, []storage.DocumentSymbol{}, nil))
		}
		indexed, err := indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		observed := &metricIO{}
		pipeline, err := query.NewPipeline(database.Session(&gorm.Session{Logger: observed}))
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID}
		const input = "SELECT path FROM files() WHERE file.build_constraint REQUIRES 'hack' ORDER BY path"
		started := time.Now()
		first := runQuery(ctx, pipeline, input, scope)
		cold := time.Since(started)
		Expect(first.Rows).To(Equal([]map[string]any{{"path": "hack/a.go"}, {"path": "hack/b.go"}, {"path": "hack/c.go"}}))
		Expect([]int{observed.sourceReads, observed.contentReads}).To(Equal([]int{1, 0}))
		Expect(database.Exec("DELETE FROM source_blobs").Error).To(Succeed())
		started = time.Now()
		Expect(runQuery(ctx, pipeline, input, scope).Rows).To(Equal(first.Rows))
		warm := time.Since(started)
		Expect([]int{observed.sourceReads, observed.contentReads}).To(Equal([]int{1, 0}))
		AddReportEntry("build constraint IO (three Go witnesses, one source hash)", map[string]any{"backend": backend, "cold": cold.String(), "warm": warm.String(), "cold_source_reads": 1, "warm_source_reads": 0}, ReportEntryVisibilityAlways)
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
	DescribeTable("includes excluded sources only on request and evaluates predicate-only constraints", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		indexed := indexCheckout(ctx, database, buildConstraintCheckout())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID}
		ordinary := runQuery(ctx, pipeline, "SELECT path FROM files() ORDER BY path", scope)
		Expect(ordinary.Rows).To(Equal([]map[string]any{{"path": "hack/missing.go"}, {"path": "main.go"}}))
		Expect(runQuery(ctx, pipeline, "SELECT path FROM files(include_excluded => FALSE) ORDER BY path", scope).Rows).To(Equal(ordinary.Rows))
		all := runQuery(ctx, pipeline, "SELECT path FROM files(include_excluded => TRUE)", scope)
		Expect(all.Total).To(Equal(7))
		result := runQuery(ctx, pipeline, "SELECT path FROM files(include_excluded => TRUE) WHERE path MATCH '**/hack/**/*.go' AND file.build_constraint NOT REQUIRES 'hack' ORDER BY path", scope)
		otherOS := "windows"
		if runtime.GOOS == otherOS {
			otherOS = "linux"
		}
		Expect(result.Rows).To(Equal([]map[string]any{{"path": "hack/alternative.go"}, {"path": "hack/ignored.go"}, {"path": "hack/missing.go"}, {"path": "hack/platform_" + otherOS + ".go"}}))
		Expect(result.Matches).To(HaveLen(4))
		for _, match := range result.Matches {
			Expect(match.SnapshotID).To(Equal(indexed.SnapshotID))
			Expect(match.SourceID).NotTo(BeEmpty())
			Expect(match.Line).To(BeNil())
		}
		aliased := runQuery(ctx, pipeline, "SELECT f.path, f.build_constraint FROM files(include_excluded => TRUE) AS f WHERE NOT (f.build_constraint NOT REQUIRES 'hack') ORDER BY f.path", scope)
		Expect(aliased.Rows).To(Equal([]map[string]any{{"f.path": "hack/compound.go", "f.build_constraint": "(hack || linux) && !linux"}, {"f.path": "hack/valid.go", "f.build_constraint": "hack"}}))
		Expect(runQuery(ctx, pipeline, "SELECT * FROM files()", scope).Columns).To(HaveLen(8))
		catalog, err := pipeline.QueryCatalog(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(catalog.Fields).To(HaveKeyWithValue("file.build_constraint", "string"))
		Expect(catalog.Functions).To(ContainElement(And(HaveField("Name", "files"), HaveField("Options", ContainElement("include_excluded")))))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))

	It("recovers historical headers and fails loudly when indexed bytes are unavailable", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		checkout := buildConstraintCheckout()
		indexed := indexCheckout(ctx, database, checkout)
		Expect(os.WriteFile(filepath.Join(checkout, "hack/valid.go"), []byte("//go:build ignore\n\npackage hack\n"), 0o600)).To(Succeed())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID}
		input := "SELECT path FROM files(include_excluded => TRUE) WHERE file.build_constraint REQUIRES 'hack' ORDER BY path"
		Expect(runQuery(ctx, pipeline, input, scope).Rows).To(Equal([]map[string]any{{"path": "hack/compound.go"}, {"path": "hack/valid.go"}}))
		Expect(os.RemoveAll(checkout)).To(Succeed())
		Expect(database.Exec("DELETE FROM source_blobs").Error).To(Succeed())
		Expect(runQuery(ctx, pipeline, input, scope).Total).To(Equal(2))
		cold, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		_, err = cold.RunModules(ctx, input, scope)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(indexed.SnapshotID))
	})

	It("rejects invalid build-constraint queries before reading sources", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		for _, input := range []string{
			"SELECT path FROM files(include_excluded => 'true')",
			"SELECT path FROM files(include_excluded => 1)",
			"SELECT path FROM files(include_excluded => NULL)",
			"SELECT path FROM files(include_excluded => TRUE, include_excluded => FALSE)",
			"SELECT path FROM files(unknown => TRUE)",
			"SELECT path FROM callers(func:*, include_excluded => TRUE)",
			"SELECT path FROM callers(depth => 1, func:*)",
			"SELECT path FROM files() WHERE path REQUIRES 'hack'",
			"SELECT path FROM files() WHERE file.build_constraint REQUIRES ''",
			"SELECT path FROM files() WHERE file.build_constraint REQUIRES '!hack'",
			"SELECT path FROM files() WHERE file.build_constraint REQUIRES 'hack || linux'",
		} {
			_, err := pipeline.RunModules(ctx, input, query.ModuleScopeOptions{})
			Expect(err).To(BeAssignableToTypeOf(&query.InvalidQueryError{}), input)
		}
	})

	It("preserves unavailable source errors instead of passing a negative predicate", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		checkout := buildConstraintCheckout()
		indexed := indexCheckout(ctx, database, checkout)
		var revision storage.SourceRevision
		Expect(database.Where("path_key = ?", "hack/valid.go").First(&revision).Error).To(Succeed())
		Expect(os.Remove(filepath.Join(checkout, "hack/valid.go"))).To(Succeed())
		Expect(database.Exec("UPDATE source_blobs SET content = ? WHERE content_hash = ?", []byte("corrupt"), revision.ContentHash).Error).To(Succeed())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		_, err = pipeline.RunModules(ctx, "SELECT path FROM files(include_excluded => TRUE) WHERE file.build_constraint NOT REQUIRES 'hack'", query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID})
		Expect(err).To(MatchError(And(ContainSubstring("hack/valid.go"), ContainSubstring(indexed.SnapshotID), ContainSubstring("hash"))))
	})
})
