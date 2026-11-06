package lint

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/flanksource/commons-db/models"
	gavel "github.com/flanksource/gavel/models"
	"github.com/flanksource/uir/databasecatalog"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// cacheRules has one rule per closure: typed-params is checked per symbol (it reads symbol.id), file-params
// per file (syntax analysis) and callers over the whole scope (navigation).
const cacheRules = `version: 1
lint:
  rules:
    - id: typed-params
      message: "{{.name}} has parameters"
      query: "SELECT symbol.id AS id, symbol.name AS name FROM func:* WHERE symbol.metrics.param_count > 1"
    - id: file-params
      message: "{{.name}} %s"
      query: "SELECT symbol.name AS name FROM func:* WHERE symbol.metrics.param_count > 1"
    - id: callers
      message: "{{.name}} calls Target"
      query: "SELECT symbol.name AS name FROM callers(func:Target)"
`

// pairCounts is each rule's closure and its evaluated and reused pair counts.
func pairCounts(result *Result) map[string]string {
	counts := map[string]string{}
	for _, rule := range result.Cache.Rules {
		counts[rule.ID] = fmt.Sprintf("%s evaluated=%d reused=%d", rule.Closure, rule.Evaluated, rule.Reused)
	}
	return counts
}

// reported is every finding as rule, file, line, column and message, sorted.
func reported(result *Result) []string {
	findings := []string{}
	for _, violation := range result.Violations {
		findings = append(findings, fmt.Sprintf("%s %s:%d:%d %s", violation.Rule.Method, filepath.Base(violation.File), violation.Line, violation.Column, *violation.Message))
	}
	slices.Sort(findings)
	return findings
}

var _ = Describe("Lint result reuse", func() {
	DescribeTable("evaluates only the pairs whose closure changed", func(ctx SpecContext, backend string) {
		database := lintDatabase(ctx, backend)
		root := GinkgoT().TempDir()
		write := func(name, content string) {
			GinkgoHelper()
			Expect(os.WriteFile(filepath.Join(root, name), []byte(content), 0o600)).To(Succeed())
		}
		load := func(message string) *Config {
			GinkgoHelper()
			write(ConfigName, fmt.Sprintf(cacheRules, message))
			config, err := Load(filepath.Join(root, ConfigName))
			Expect(err).NotTo(HaveOccurred())
			return config
		}
		write("go.mod", "module example.org/cache\n\ngo 1.26\n")
		write("a.go", "package cache\n\nfunc Target() int { return 1 }\n\nfunc Caller() int { return Target() }\n\nfunc Other(a, b int) int { return a + b }\n")
		write("b.go", "package cache\n\nfunc Lonely(a, b int) int { return a * b }\n\nfunc helper() {}\n")
		config := load("has parameters")
		run := func(noCache bool) *Result {
			GinkgoHelper()
			result := Run(ctx, database, config, Options{NoCache: noCache})
			Expect(result.Error).To(BeEmpty())
			Expect(result.Cache.Source).To(Equal("uir"))
			return result
		}
		// reuse runs with stored findings and checks them against a run that evaluates everything.
		reuse := func() *Result {
			GinkgoHelper()
			cached := run(false)
			Expect(reported(cached)).To(Equal(reported(run(true))))
			return cached
		}
		rows := func(rule string) map[int64][]finding {
			GinkgoHelper()
			var stored []storage.LintFinding
			Expect(database.WithContext(ctx).Where("rule_id = ?", rule).Find(&stored).Error).To(Succeed())
			decoded := map[int64][]finding{}
			for _, row := range stored {
				var findings []finding
				Expect(json.Unmarshal(row.Findings, &findings)).To(Succeed())
				decoded[row.SubjectHandle] = findings
			}
			return decoded
		}

		first := run(false)
		Expect(first.Cache.Hit).To(BeFalse())
		Expect(pairCounts(first)).To(Equal(map[string]string{
			"typed-params": "subject evaluated=5 reused=0", "file-params": "file evaluated=2 reused=0", "callers": "snapshot evaluated=1 reused=0",
		}))
		Expect(reported(first)).To(Equal([]string{
			"callers a.go:5:28 Caller calls Target",
			"file-params a.go:7:6 Other has parameters", "file-params b.go:3:6 Lonely has parameters",
			"typed-params a.go:7:6 Other has parameters", "typed-params b.go:3:6 Lonely has parameters",
		}))

		By("an unchanged checkout evaluates no rule, and a clean subject's empty result is stored and reused")
		unchanged := reuse()
		Expect(unchanged.Cache.Hit).To(BeTrue())
		Expect(unchanged.Cache.Evaluated).To(BeZero())
		Expect(unchanged.Cache.Fingerprint).To(Equal(first.Cache.Fingerprint))
		Expect(unchanged.Cache.StoredAt).NotTo(BeNil())
		Expect(reported(unchanged)).To(Equal(reported(first)))
		clean := 0
		for _, findings := range rows("typed-params") {
			if len(findings) == 0 {
				clean++
			}
		}
		Expect(rows("typed-params")).To(HaveLen(5))
		Expect(clean).To(Equal(3), "Target, Caller and helper store an empty result")

		By("a body edit re-evaluates only that function's pair and file, and stops reporting it")
		write("a.go", "package cache\n\nfunc Target() int { return 1 }\n\nfunc Caller() int { return Target() }\n\nfunc Other(a int) int { return a + 1 }\n")
		edited := reuse()
		Expect(pairCounts(edited)).To(Equal(map[string]string{
			"typed-params": "subject evaluated=1 reused=4", "file-params": "file evaluated=1 reused=1", "callers": "snapshot evaluated=1 reused=0",
		}))
		Expect(reported(edited)).To(Equal([]string{
			"callers a.go:5:28 Caller calls Target", "file-params b.go:3:6 Lonely has parameters", "typed-params b.go:3:6 Lonely has parameters",
		}))

		By("a navigation rule re-evaluates on a change elsewhere in the scope")
		write("b.go", "package cache\n\nfunc Lonely(a, b int) int { return a * b }\n\nfunc helper() {}\n\nfunc Second() int { return Target() }\n")
		navigated := reuse()
		Expect(pairCounts(navigated)).To(Equal(map[string]string{
			"typed-params": "subject evaluated=1 reused=5", "file-params": "file evaluated=1 reused=1", "callers": "snapshot evaluated=1 reused=0",
		}))
		Expect(reported(navigated)).To(ContainElement("callers b.go:7:28 Second calls Target"))

		By("editing one rule's message re-evaluates only that rule")
		config = load("takes too many")
		renamed := reuse()
		Expect(pairCounts(renamed)).To(Equal(map[string]string{
			"typed-params": "subject evaluated=0 reused=6", "file-params": "file evaluated=2 reused=0", "callers": "snapshot evaluated=0 reused=1",
		}))
		Expect(reported(renamed)).To(ContainElement("file-params b.go:3:6 Lonely takes too many"))
		Expect(reported(renamed)).NotTo(ContainElement("file-params b.go:3:6 Lonely has parameters"))

		By("--no-lint-cache ignores stored findings and overwrites them")
		Expect(database.WithContext(ctx).Model(&storage.LintFinding{}).Where("rule_id = ?", "typed-params").
			Update("findings", storage.JSON("[]")).Error).To(Succeed())
		Expect(reported(run(false))).NotTo(ContainElement(ContainSubstring("typed-params")), "a cached run serves the stored rows")
		forced := run(true)
		Expect(forced.Cache.Hit).To(BeFalse())
		Expect(forced.Cache.Reused).To(BeZero())
		Expect(forced.Cache.Evaluated).To(Equal(9))
		Expect(reported(forced)).To(ContainElement("typed-params b.go:3:6 Lonely has parameters"))
		after := run(false)
		Expect(after.Cache.Hit).To(BeTrue())
		Expect(reported(after)).To(Equal(reported(forced)))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))

	DescribeTable("re-evaluates when a document ordinal is recycled or selection changes", func(ctx SpecContext, backend string) {
		database := lintDatabase(ctx, backend)
		root := GinkgoT().TempDir()
		write := func(name, content string) {
			GinkgoHelper()
			Expect(os.WriteFile(filepath.Join(root, name), []byte(content), 0o600)).To(Succeed())
		}
		params := `    - id: params
      message: "{{.name}}"
      query: "SELECT symbol.name AS name FROM func:* WHERE symbol.metrics.param_count > 1"
`
		structs := `    - id: structs
      message: "{{.name}}"
      query: "SELECT symbol.name AS name FROM struct:*"
`
		var config *Config
		load := func(rules string) {
			GinkgoHelper()
			write(ConfigName, "version: 1\nlint:\n  rules:\n"+rules)
			var err error
			config, err = Load(filepath.Join(root, ConfigName))
			Expect(err).NotTo(HaveOccurred())
		}
		reuse := func() []string {
			GinkgoHelper()
			cached := Run(ctx, database, config, Options{})
			Expect(cached.Error).To(BeEmpty())
			forced := Run(ctx, database, config, Options{NoCache: true})
			Expect(forced.Error).To(BeEmpty())
			Expect(reported(cached)).To(Equal(reported(forced)))
			return reported(cached)
		}
		write("go.mod", "module example.org/recycle\n\ngo 1.26\n")
		write("a.go", "package recycle\n\ntype Shape int\n\nfunc A() {}\n")
		write("z.go", "package recycle\n\nfunc Z(a, b int) int { return a + b }\n")
		load(params)
		Expect(reuse()).To(Equal([]string{"params z.go:3:6 Z"}))

		By("a pruned document's ordinal is allocated to a new document of the same path")
		Expect(os.Remove(filepath.Join(root, "z.go"))).To(Succeed())
		Expect(reuse()).To(BeEmpty())
		var roots []storage.ModuleRoot
		Expect(database.WithContext(ctx).Find(&roots).Error).To(Succeed())
		Expect(roots).To(HaveLen(1))
		pruned, err := storage.PruneHistory(ctx, database, roots[0].RootKey, storage.HistoryPruneOptions{Keep: 1})
		Expect(err).NotTo(HaveOccurred())
		Expect(pruned.Documents).To(BeNumerically(">", 0))
		write("z.go", "package recycle\n\nfunc Z(a int) int { return a }\n")
		Expect(reuse()).To(BeEmpty())

		By("a symbol the struct selector skipped becomes a struct")
		load(params + structs)
		Expect(reuse()).To(BeEmpty())
		write("a.go", "package recycle\n\ntype Shape struct{}\n\nfunc A() {}\n")
		Expect(reuse()).To(Equal([]string{"structs a.go:3:6 Shape"}))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))

	DescribeTable("re-evaluates table rules only for the layer they read", func(ctx SpecContext, backend string) {
		database := lintDatabase(ctx, backend)
		root := GinkgoT().TempDir()
		sourcePath := filepath.Join(root, "source.db")
		source, err := sql.Open("sqlite", sourcePath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(source.Close)
		exec := func(statement string) {
			GinkgoHelper()
			_, err := source.ExecContext(ctx, statement)
			Expect(err).NotTo(HaveOccurred())
		}
		exec("CREATE TABLE audit_log (id INTEGER PRIMARY KEY, msg TEXT)")
		exec("CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT)")
		exec("CREATE TABLE orders (id INTEGER PRIMARY KEY, customer_id INTEGER, total INTEGER)")
		connection := models.Connection{Name: "Local", Type: models.ConnectionTypeSQLite, URL: sourcePath}
		Expect(database.WithContext(ctx).Create(&connection).Error).To(Succeed())
		module := storage.DatabaseModule{ConnectionID: connection.ID, DatabaseName: "snapshot", Name: "Shop"}
		Expect(storage.CreateDatabaseModule(ctx, database, &module)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, ConfigName), []byte(`version: 1
lint:
  rules:
    - id: tables-private
      reads: private
      message: "{{.name}}"
      query: "SELECT symbol.name AS name FROM table:*"
    - id: tables-public
      message: "{{.name}}"
      query: "SELECT symbol.name AS name FROM table:*"
    - id: orders-only
      message: "{{.name}}"
      query: "SELECT symbol.name AS name FROM table:orders"
`), 0o600)).To(Succeed())
		config, err := Load(filepath.Join(root, ConfigName))
		Expect(err).NotTo(HaveOccurred())
		capture := func() string {
			GinkgoHelper()
			result, err := databasecatalog.Capture(ctx, database, module)
			Expect(err).NotTo(HaveOccurred())
			return result.SnapshotID
		}
		// A relative work dir: catalog paths are database:/ URIs, which an absolute one cannot be relative to.
		run := func(snapshot string, noCache bool) *Result {
			GinkgoHelper()
			result := Run(ctx, database, config, Options{Snapshot: snapshot, NoCache: noCache, WorkDir: "."})
			Expect(result.Error).To(BeEmpty())
			return result
		}
		reuse := func(snapshot string) *Result {
			GinkgoHelper()
			cached := run(snapshot, false)
			Expect(reported(cached)).To(Equal(reported(run(snapshot, true))))
			return cached
		}
		orders := func(result *Result) int {
			GinkgoHelper()
			index := slices.IndexFunc(result.Violations, func(violation gavel.Violation) bool { return violation.Rule.Method == "orders-only" })
			Expect(index).NotTo(Equal(-1))
			return result.Violations[index].Line
		}
		first := capture()
		run(first, false)
		Expect(reuse(first).Cache.Hit).To(BeTrue())

		By("adding an index re-evaluates only the reads: private rule, for that table")
		exec("CREATE INDEX orders_customer_idx ON orders (customer_id)")
		indexed := reuse(capture())
		Expect(pairCounts(indexed)).To(Equal(map[string]string{
			"tables-private": "subject evaluated=1 reused=2", "tables-public": "subject evaluated=0 reused=3", "orders-only": "subject evaluated=0 reused=3",
		}))

		By("a DDL change to a table the rule does not read re-evaluates nothing, and moves its reused finding")
		exec("ALTER TABLE audit_log ADD COLUMN level TEXT")
		altered := reuse(capture())
		Expect(pairCounts(altered)["orders-only"]).To(Equal("subject evaluated=0 reused=3"))
		Expect(pairCounts(altered)["tables-public"]).To(Equal("subject evaluated=1 reused=2"))
		Expect(orders(altered)).To(Equal(orders(indexed)+1), "orders is declared one line lower after audit_log grew a column")
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
})
