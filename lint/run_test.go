package lint

import (
	"context"
	"os"
	"path/filepath"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func lintDatabase(ctx context.Context, backend string) *gorm.DB {
	GinkgoHelper()
	dsn := filepath.Join(GinkgoT().TempDir(), "lint.db")
	if backend == "postgres" {
		dsn = dbtest.ForGinkgo(dbtest.Options{Name: "uir_lint"}).DSN()
	}
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: dsn})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		sql, err := database.DB()
		Expect(err).NotTo(HaveOccurred())
		Expect(sql.Close()).To(Succeed())
	})
	return database
}

func lintCheckout() *Config {
	GinkgoHelper()
	path := configFile(validConfig)
	root := filepath.Dir(path)
	Expect(os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.org/linted\n\ngo 1.26\n"), 0o600)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(root, "small.go"), []byte("package linted\nfunc Small(a, b, c, d int) {}\n"), 0o600)).To(Succeed())
	config, err := Load(path)
	Expect(err).NotTo(HaveOccurred())
	return config
}

var _ = Describe("SQL lint execution", func() {
	It("indexes local dependencies as context without linting them as project targets", func(ctx SpecContext) {
		database := lintDatabase(ctx, "sqlite")
		workspace := GinkgoT().TempDir()
		project, app, library := filepath.Join(workspace, "project"), filepath.Join(workspace, "project", "app"), filepath.Join(workspace, "library")
		Expect(os.MkdirAll(app, 0o700)).To(Succeed())
		Expect(os.MkdirAll(library, 0o700)).To(Succeed())
		for path, content := range map[string]string{
			"go.work":             "go 1.26\nuse (\n./project/app\n./library\n)\n",
			"project/app/go.mod":  "module example.org/app\n\ngo 1.26\nrequire example.org/library v0.0.0\n",
			"project/app/main.go": "package app\nimport \"example.org/library\"\nfunc App() { library.Run() }\n",
			"library/go.mod":      "module example.org/library\n\ngo 1.26\n",
			"library/library.go":  "package library\nfunc Run() {}\n",
			"project/.uir.yaml":   validConfig,
		} {
			Expect(os.WriteFile(filepath.Join(workspace, path), []byte(content), 0o600)).To(Succeed())
		}
		config, err := Load(filepath.Join(project, ConfigName))
		Expect(err).NotTo(HaveOccurred())
		report := Run(ctx, database, config, Options{})
		Expect(report.Success).To(BeTrue(), report.Error)
		Expect(report.Violations).To(HaveLen(1))
		Expect(report.Violations[0].File).To(Equal(filepath.Join(app, "main.go")))
		Expect(Run(ctx, database, config, Options{NoIndex: true}).Success).To(BeTrue())
		included := Run(ctx, database, config, Options{NoIndex: true, Include: []string{"app/*.go"}})
		Expect(included.Success).To(BeTrue(), included.Error)
		Expect(included.Violations).To(HaveLen(1))
		excluded := Run(ctx, database, config, Options{NoIndex: true, Exclude: []string{"app/**"}})
		Expect(excluded.Success).To(BeTrue(), excluded.Error)
		Expect(excluded.Violations).To(BeEmpty())
	})
	It("uses call-site witnesses even when projections contain only callee names", func(ctx SpecContext) {
		database := lintDatabase(ctx, "sqlite")
		config := lintCheckout()
		root := filepath.Dir(config.Path)
		Expect(os.WriteFile(filepath.Join(root, "caller.go"), []byte("package linted\nfunc Caller() { Small(1,2,3,4) }\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(config.Path, []byte("version: 1\nlint:\n  rules:\n    - {id: dependencies, query: 'SELECT symbol.name AS name FROM callees(func:Caller)', message: '{{.name}} is forbidden'}\n"), 0o600)).To(Succeed())
		config, err := Load(config.Path)
		Expect(err).NotTo(HaveOccurred())
		report := Run(ctx, database, config, Options{})
		Expect(report.Success).To(BeTrue(), report.Error)
		Expect(report.Violations).To(HaveLen(1))
		Expect(report.Violations[0].File).To(Equal(filepath.Join(root, "caller.go")))
		Expect(report.Violations[0].Line).To(Equal(2))
		Expect(report.Violations[0].Message).To(HaveValue(Equal("Small is forbidden")))
	})
	It("rejects unknown rule selections before indexing", func(ctx SpecContext) {
		database := lintDatabase(ctx, "sqlite")
		config := lintCheckout()
		report := Run(ctx, database, config, Options{Rules: []string{"unknown"}})
		Expect(report.Success).To(BeFalse())
		Expect(report.Error).To(ContainSubstring("unknown"))
		var snapshots int64
		Expect(database.Model(&storage.ModuleSnapshot{}).Count(&snapshots).Error).To(Succeed())
		Expect(snapshots).To(BeZero())
	})
	DescribeTable("refreshes, reuses current facts, rejects stale reuse and supports historical snapshots", func(ctx SpecContext, backend string) {
		database := lintDatabase(ctx, backend)
		config := lintCheckout()
		root := filepath.Dir(config.Path)
		first := Run(ctx, database, config, Options{})
		Expect(first.Success).To(BeTrue(), first.Error)
		Expect(first.Violations).To(HaveLen(1))
		Expect(first.Violations[0].File).To(Equal(filepath.Join(root, "small.go")))
		Expect(first.Violations[0].Line).To(BeZero())
		Expect(first.Violations[0].Message).To(HaveValue(Equal("small.go is small")))
		engine, err := indexer.New(database)
		Expect(err).NotTo(HaveOccurred())
		head, err := engine.CheckCurrent(ctx, root, indexer.CheckOptions{})
		Expect(err).NotTo(HaveOccurred())
		var before int64
		Expect(database.Model(&storage.ModuleSnapshot{}).Count(&before).Error).To(Succeed())
		Expect(Run(ctx, database, config, Options{NoIndex: true}).Success).To(BeTrue())
		Expect(Run(ctx, database, config, Options{}).Success).To(BeTrue())
		var after int64
		Expect(database.Model(&storage.ModuleSnapshot{}).Count(&after).Error).To(Succeed())
		Expect(after).To(Equal(before))
		Expect(os.WriteFile(filepath.Join(root, "small.go"), []byte("package linted\nfunc Changed() {}\n"), 0o600)).To(Succeed())
		stale := Run(ctx, database, config, Options{NoIndex: true})
		Expect(stale.Success).To(BeFalse())
		Expect(stale.Error).To(ContainSubstring("stale"))
		Expect(Run(ctx, database, config, Options{Snapshot: head.SnapshotID}).Success).To(BeTrue())
		Expect(Run(ctx, database, config, Options{}).Success).To(BeTrue())
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
	It("retains completed findings when a later message references a missing column", func(ctx SpecContext) {
		database := lintDatabase(ctx, "sqlite")
		config := lintCheckout()
		Expect(os.WriteFile(config.Path, []byte(validConfig+"    - id: broken\n      query: SELECT path FROM files()\n      message: '{{.missing}}'\n"), 0o600)).To(Succeed())
		config, err := Load(config.Path)
		Expect(err).NotTo(HaveOccurred())
		report := Run(ctx, database, config, Options{})
		Expect(report.Success).To(BeFalse())
		Expect(report.Error).To(ContainSubstring("broken"))
		Expect(report.Violations).To(HaveLen(1))
		excluded := Run(ctx, database, config, Options{NoIndex: true, Exclude: []string{"broken"}})
		Expect(excluded.Success).To(BeTrue(), excluded.Error)
		Expect(excluded.RuleCount).To(Equal(1))
		Expect(excluded.Violations).To(HaveLen(1))
	})
	It("warns on partial coverage and still permits fresh no-index runs", func(ctx SpecContext) {
		database := lintDatabase(ctx, "sqlite")
		config := lintCheckout()
		Expect(os.WriteFile(filepath.Join(filepath.Dir(config.Path), "broken.go"), []byte("package linted\nvar _ = missing\n"), 0o600)).To(Succeed())
		for _, options := range []Options{{}, {NoIndex: true}, {NoIndex: true, Exclude: []string{"**"}}} {
			report := Run(ctx, database, config, options)
			Expect(report.Success).To(BeTrue(), report.Error)
			Expect(report.Violations).To(ContainElement(And(HaveField("File", ""), HaveField("Severity", BeEquivalentTo("warning")), HaveField("Rule.Method", "uir/coverage"))))
		}
	})
})
