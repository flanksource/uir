package lint

import (
	"os"
	"path/filepath"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Module-scoped lint analysis", func() {
	It("checks metrics without resolving an unavailable imported module", func(ctx SpecContext) {
		config := lintCheckout()
		root := config.Root
		Expect(os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.org/linted\n\ngo 1.26\nrequire example.org/unavailable v1.0.0\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "missing.go"), []byte("package linted\nimport _ \"example.org/unavailable\"\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(config.Path, []byte("version: 1\nlint:\n  rules:\n    - {id: params, query: 'SELECT symbol.name AS name FROM func:* WHERE symbol.metrics.param_count > 3', message: '{{.name}}'}\n"), 0o600)).To(Succeed())
		config, err := Load(config.Path)
		Expect(err).NotTo(HaveOccurred())
		report := Run(ctx, lintDatabase(ctx, "sqlite"), config, Options{GroupBy: "symbol"})
		Expect(report.Success).To(BeTrue(), report.Error)
		Expect(report.Violations).To(ConsistOf(And(HaveField("Rule.Method", "params"), HaveField("Message", HaveValue(Equal("Small"))))))
		Expect(report.Groups).To(ConsistOf(And(HaveField("SymbolID", ""), HaveField("DeclarationKey", Not(BeEmpty())))))
	})

	It("keeps syntax findings on a typed failure and restores configured order after both modes succeed", func(ctx SpecContext) {
		config := lintCheckout()
		Expect(os.WriteFile(config.Path, []byte("version: 1\nlint:\n  rules:\n    - {id: identity, query: 'SELECT symbol.id FROM func:Small', message: identity}\n    - {id: params, query: 'SELECT symbol.name AS name FROM func:Small WHERE symbol.metrics.param_count > 3', message: '{{.name}}'}\n"), 0o600)).To(Succeed())
		config, err := Load(config.Path)
		Expect(err).NotTo(HaveOccurred())
		database := lintDatabase(ctx, "sqlite")
		engine, err := indexer.New(database)
		Expect(err).NotTo(HaveOccurred())
		_, err = engine.Analyze(ctx, indexer.AnalysisOptions{Directory: config.Root, Mode: storage.AnalysisSyntax})
		Expect(err).NotTo(HaveOccurred())
		partial := Run(ctx, database, config, Options{NoIndex: true})
		Expect(partial.Success).To(BeFalse())
		Expect(partial.Error).To(ContainSubstring("missing or stale typed lint analysis"))
		Expect(partial.Violations).To(ConsistOf(HaveField("Rule.Method", "params")))
		complete := Run(ctx, database, config, Options{GroupBy: "symbol"})
		Expect(complete.Success).To(BeTrue(), complete.Error)
		Expect(complete.Violations).To(HaveExactElements(HaveField("Rule.Method", "identity"), HaveField("Rule.Method", "params")))
		Expect(complete.Groups).To(ConsistOf(And(HaveField("SymbolID", Not(BeEmpty())), HaveField("Violations", HaveLen(2)))))
	})

	It("keeps typed facts in the current module and traverses registered modules only with all", func(ctx SpecContext) {
		workspace := GinkgoT().TempDir()
		for path, content := range map[string]string{
			"go.work":                "go 1.26\nuse (\n./app\n./library\n./unrelated\n)\n",
			"app/go.mod":             "module example.org/app\n\ngo 1.26\nrequire example.org/library v0.0.0\n",
			"app/app.go":             "package app\nimport \"example.org/library\"\nfunc App() { library.Run() }\n",
			"app/.uir.yaml":          "version: 1\nlint:\n  rules:\n    - {id: indirect, query: 'SELECT symbol.name AS name FROM callees(callees(func:App))', message: '{{.name}}'}\n",
			"library/go.mod":         "module example.org/library\n\ngo 1.26\n",
			"library/library.go":     "package library\nfunc Run() { Inner() }\nfunc Inner() {}\n",
			"unrelated/go.mod":       "module example.org/unrelated\n\ngo 1.26\n",
			"unrelated/unrelated.go": "package unrelated\nfunc Work() {}\n",
		} {
			file := filepath.Join(workspace, path)
			Expect(os.MkdirAll(filepath.Dir(file), 0o700)).To(Succeed())
			Expect(os.WriteFile(file, []byte(content), 0o600)).To(Succeed())
		}
		config, err := Load(filepath.Join(workspace, "app", ConfigName))
		Expect(err).NotTo(HaveOccurred())
		database := lintDatabase(ctx, "sqlite")
		local := Run(ctx, database, config, Options{})
		Expect(local.Success).To(BeTrue(), local.Error)
		Expect(local.Violations).To(BeEmpty())
		var roots, edges int64
		Expect(database.Model(&storage.ModuleRoot{}).Count(&roots).Error).To(Succeed())
		Expect(database.Model(&storage.SnapshotDependency{}).Count(&edges).Error).To(Succeed())
		Expect([]int64{roots, edges}).To(Equal([]int64{1, 0}))
		engine, err := indexer.New(database)
		Expect(err).NotTo(HaveOccurred())
		first, err := engine.Analyze(ctx, indexer.AnalysisOptions{Directory: config.Root, Mode: storage.AnalysisTyped})
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(workspace, "unrelated/unrelated.go"), []byte("package unrelated\nfunc Changed() {}\n"), 0o600)).To(Succeed())
		second, err := engine.Analyze(ctx, indexer.AnalysisOptions{Directory: config.Root, Mode: storage.AnalysisTyped})
		Expect(err).NotTo(HaveOccurred())
		Expect(second.SnapshotID).To(Equal(first.SnapshotID))
		_, err = engine.Analyze(ctx, indexer.AnalysisOptions{Directory: filepath.Join(workspace, "library"), Mode: storage.AnalysisTyped})
		Expect(err).NotTo(HaveOccurred())
		all := Run(ctx, database, config, Options{All: true})
		Expect(all.Success).To(BeTrue(), all.Error)
		Expect(all.Violations).To(ConsistOf(And(HaveField("Rule.Method", "indirect"), HaveField("Message", HaveValue(Equal("Inner"))))))
		filtered := Run(ctx, database, config, Options{All: true, NoIndex: true, Include: []string{"library.go"}})
		Expect(filtered.Success).To(BeTrue(), filtered.Error)
		Expect(filtered.Violations).To(HaveLen(1))
		Expect(os.WriteFile(filepath.Join(workspace, "library", "extra.go"), []byte("package library\nfunc Extra() {}\n"), 0o600)).To(Succeed())
		_, err = engine.Analyze(ctx, indexer.AnalysisOptions{Directory: config.Root, Mode: storage.AnalysisTyped, NoIndex: true})
		Expect(err).To(MatchError(ContainSubstring("stale")))
	})
})
