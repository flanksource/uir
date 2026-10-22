package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/tools/go/packages"
)

var _ = Describe("Project analysis", func() {
	DescribeTable("publishes readable and reusable syntax snapshots with external test packages", func(ctx SpecContext, production bool) {
		database := openIndexerSQLite(ctx)
		root := canonicalTempDir()
		const module = "example.org/work"
		writeFile(filepath.Join(root, "go.mod"), "module "+module+"\n\ngo 1.26\n")
		writeFile(filepath.Join(root, "work_test.go"), "package work_test\nfunc Work() {}\n")
		fileCount := 1
		if production {
			writeFile(filepath.Join(root, "work.go"), "package work\nfunc Work() {}\n")
			fileCount++
		}
		engine, err := New(database)
		Expect(err).NotTo(HaveOccurred())
		options := AnalysisOptions{Directory: root, Mode: storage.AnalysisSyntax, IncludeTests: true}
		first, err := engine.Analyze(ctx, options)
		Expect(err).NotTo(HaveOccurred())
		documents, err := storage.ActiveDocuments(ctx, database, uuid.MustParse(first.SnapshotID), storage.ActiveDocumentOptions{Content: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(documents).To(HaveLen(fileCount))
		Expect(documents["work_test.go"].Source.PackagePath).To(Equal(module))
		var content storage.DocumentContent
		Expect(json.Unmarshal(documents["work_test.go"].Document.Content, &content)).To(Succeed())
		Expect(content.PackagePath).To(Equal(module + "_test"))
		Expect(content.Symbols).To(ConsistOf(HaveField("Identifier.Package", module+"_test")))
		var coverage []storage.PackageCoverage
		Expect(database.Where("snapshot_id = ?", first.SnapshotID).Find(&coverage).Error).To(Succeed())
		Expect(coverage).To(ConsistOf(SatisfyAll(HaveField("PackagePath", module), HaveField("FileCount", fileCount))))
		second, err := engine.Analyze(ctx, options)
		Expect(err).NotTo(HaveOccurred())
		Expect(second.SnapshotID).To(Equal(first.SnapshotID))
		Expect(second.Unchanged).To(BeTrue())
	}, Entry("mixed production and external tests", true), Entry("external tests only", false))

	It("creates an independent analysis head when the module path changes in the same directory", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		root := canonicalTempDir()
		writeFile(filepath.Join(root, "main.go"), "package work\nfunc Work() {}\n")
		engine, err := New(database)
		Expect(err).NotTo(HaveOccurred())
		for _, module := range []string{"example.org/original", "example.org/renamed"} {
			writeFile(filepath.Join(root, "go.mod"), "module "+module+"\n\ngo 1.26\n")
			analysis, err := engine.Analyze(ctx, AnalysisOptions{Directory: root, Mode: storage.AnalysisSyntax})
			Expect(err).NotTo(HaveOccurred())
			Expect(analysis.RootKey).To(Equal(module))
		}
		var count int64
		Expect(database.Model(&storage.AnalysisHead{}).Count(&count).Error).To(Succeed())
		Expect(count).To(Equal(int64(2)))
	})

	DescribeTable("preserves normal index heads and retains analysis heads when pruning", func(ctx SpecContext, backend string) {
		database := openIndexerSQLite(ctx)
		if backend == "postgres" {
			database = openIndexerPostgres(ctx)
		}
		root := canonicalTempDir()
		writeFile(filepath.Join(root, "go.mod"), "module example.org/heads\n\ngo 1.26\n")
		writeFile(filepath.Join(root, "main.go"), "package heads\nfunc Work(a, b int) {}\n")
		engine, err := New(database)
		Expect(err).NotTo(HaveOccurred())
		indexed, err := engine.IndexModules(ctx, ModuleOptions{Path: root, Reason: storage.ReasonAdd})
		Expect(err).NotTo(HaveOccurred())
		for _, mode := range []storage.AnalysisMode{storage.AnalysisSyntax, storage.AnalysisTyped} {
			analysis, err := engine.Analyze(ctx, AnalysisOptions{Directory: root, Mode: mode})
			Expect(err).NotTo(HaveOccurred())
			Expect(analysis.SnapshotID).NotTo(Equal(indexed[0].SnapshotID))
		}
		head, err := engine.CheckCurrent(ctx, root, CheckOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(head.SnapshotID).To(Equal(indexed[0].SnapshotID))
		_, err = storage.PruneHistory(ctx, database, "example.org/heads", storage.HistoryPruneOptions{Keep: 1})
		Expect(err).NotTo(HaveOccurred())
		var count int64
		Expect(database.Model(&storage.ModuleSnapshot{}).Count(&count).Error).To(Succeed())
		Expect(count).To(Equal(int64(3)))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))

	It("does not analyze a nested module without selecting it", func(ctx SpecContext) {
		root := canonicalTempDir()
		writeFile(filepath.Join(root, "go.mod"), "module example.org/parent\n\ngo 1.26\n")
		writeFile(filepath.Join(root, "parent.go"), "package parent\nfunc Parent() {}\n")
		Expect(os.Mkdir(filepath.Join(root, "child"), 0o700)).To(Succeed())
		writeFile(filepath.Join(root, "child/go.mod"), "module example.org/child\n\ngo 1.26\n")
		writeFile(filepath.Join(root, "child/child.go"), "invalid Go source")
		engine, err := New(openIndexerSQLite(ctx))
		Expect(err).NotTo(HaveOccurred())
		result, err := engine.Analyze(ctx, AnalysisOptions{Directory: root, Mode: storage.AnalysisSyntax})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Files).To(Equal(1))
	})
	It("publishes and reuses syntax facts without loading packages or indexing dependencies", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		root := canonicalTempDir()
		writeFile(filepath.Join(root, "go.mod"), "module example.org/analysis\n\ngo 1.26\nrequire example.org/unavailable v1.0.0\n")
		writeFile(filepath.Join(root, "main.go"), "package analysis\nimport _ \"example.org/unavailable\"\nfunc Work(a, b int) {}\n")
		engine, err := New(database)
		Expect(err).NotTo(HaveOccurred())
		engine.loadPackages = func(*packages.Config, ...string) ([]*packages.Package, error) {
			return nil, errors.New("syntax analysis must not load Go packages")
		}
		options := AnalysisOptions{Directory: root, Mode: storage.AnalysisSyntax}
		first, err := engine.Analyze(ctx, options)
		Expect(err).NotTo(HaveOccurred())
		second, err := engine.Analyze(ctx, options)
		Expect(err).NotTo(HaveOccurred())
		Expect(second.SnapshotID).To(Equal(first.SnapshotID))
		Expect(second.Unchanged).To(BeTrue())
		var heads, edges, roots int64
		Expect(database.Model(&storage.ModuleLocationHead{}).Count(&heads).Error).To(Succeed())
		Expect(database.Model(&storage.SnapshotDependency{}).Count(&edges).Error).To(Succeed())
		Expect(database.Model(&storage.ModuleRoot{}).Count(&roots).Error).To(Succeed())
		Expect([]int64{heads, edges, roots}).To(Equal([]int64{0, 0, 1}))
		options.NoIndex = true
		_, err = engine.Analyze(ctx, options)
		Expect(err).NotTo(HaveOccurred())
		writeFile(filepath.Join(root, "main.go"), "package analysis\nfunc Changed() {}\n")
		_, err = engine.Analyze(context.Background(), options)
		Expect(err).To(MatchError(ContainSubstring("stale")))
	})
})
