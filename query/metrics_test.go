package query_test

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const metricModule = "example.org/metrics"

var metricSources = map[string]string{
	"empty.go":     "package metrics\n",
	"small.go":     "package metrics\nfunc Small(a, b int) {}\n",
	"large.go":     "package metrics\nfunc Large(a, b, c, d int) {\n // comment\n\n}\n",
	"many.go":      "package metrics\nconst First = 1\nconst Second = 2\nconst Third = 3\n",
	"interface.go": "package metrics\ntype Reader interface { Read(a, b int) }\n",
	"excluded.go":  "//go:build metric_excluded\n\npackage metrics\nfunc Excluded() {}\n",
}

func metricCheckout() string {
	GinkgoHelper()
	checkout := GinkgoT().TempDir()
	Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+metricModule+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
	for path, source := range metricSources {
		Expect(os.WriteFile(filepath.Join(checkout, path), []byte(source), 0o644)).To(Succeed())
	}
	return checkout
}

var _ = Describe("SQL metrics", func() {
	It("projects caller and callee metrics while retaining the call-site witness", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		checkout := metricCheckout()
		Expect(os.WriteFile(filepath.Join(checkout, "caller.go"), []byte("package metrics\nfunc Caller() { Small(1, 2) }\n"), 0o644)).To(Succeed())
		indexed := indexCheckout(ctx, database, checkout)
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID}
		caller := runQuery(ctx, pipeline, "SELECT symbol.name, path, line, symbol.metrics.param_count FROM callers(func:Small) WHERE symbol.metrics.param_count = 0", scope)
		Expect(caller.Rows).To(Equal([]map[string]any{{"symbol.name": "Caller", "path": "caller.go", "line": 2, "symbol.metrics.param_count": 0}}))
		callee := runQuery(ctx, pipeline, "SELECT symbol.name, path, line, symbol.metrics.param_count FROM callees(func:Caller)", scope)
		Expect(callee.Rows).To(Equal([]map[string]any{{"symbol.name": "Small", "path": "caller.go", "line": 2, "symbol.metrics.param_count": 2}}))
	})
	It("keeps strict file-size and definition thresholds, aliases and wildcard columns", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		checkout := metricCheckout()
		for _, sample := range []struct {
			path  string
			lines int
		}{{"below.go", 99}, {"at.go", 100}} {
			Expect(os.WriteFile(filepath.Join(checkout, sample.path), []byte("package metrics\n"+strings.Repeat("// counted\n", sample.lines-1)), 0o644)).To(Succeed())
		}
		indexed := indexCheckout(ctx, database, checkout)
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID}
		result := runQuery(ctx, pipeline, "SELECT f.path, f.lines FROM files() AS f WHERE f.lines >= 99 AND f.lines < 100 AND f.definitions < 3", scope)
		Expect(result.Rows).To(Equal([]map[string]any{{"f.path": "below.go", "f.lines": 99}}))
		result = runQuery(ctx, pipeline, "SELECT * FROM files() WHERE path = 'many.go'", scope)
		Expect(result.Columns).To(HaveLen(6))
		Expect(result.Rows[0]).To(HaveKeyWithValue("file.definitions", 3))
	})

	It("fails before filtering when requested stored metrics are unavailable", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		indexed := indexCheckout(ctx, database, metricCheckout())
		Expect(database.Exec("DELETE FROM source_metrics").Error).To(Succeed())
		Expect(database.Exec("DELETE FROM declaration_metrics").Error).To(Succeed())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID}
		for _, expression := range []string{"SELECT path FROM files() WHERE file.lines < 0", "SELECT symbol.name FROM func:* WHERE symbol.metrics.param_count < 0"} {
			_, err := pipeline.RunModules(ctx, expression, scope)
			Expect(err).To(MatchError(And(ContainSubstring(indexed.SnapshotID), ContainSubstring("reindex or republish"))))
		}
		Expect(runQuery(ctx, pipeline, "SELECT path FROM files()", scope).Total).To(Equal(5))
		Expect(runQuery(ctx, pipeline, "SELECT symbol.name FROM func:Small", scope).Total).To(Equal(1))
	})

	It("retains unchanged metric projections and prunes only discarded document metrics", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		checkout := metricCheckout()
		first := indexCheckout(ctx, database, checkout)
		var original int64
		Expect(database.Model(&storage.DeclarationMetrics{}).Count(&original).Error).To(Succeed())
		Expect(indexCheckout(ctx, database, checkout).SnapshotID).To(Equal(first.SnapshotID))
		Expect(os.WriteFile(filepath.Join(checkout, "small.go"), []byte("package metrics\nfunc Small(a, b int) {\n\n}\n"), 0o644)).To(Succeed())
		second := indexCheckout(ctx, database, checkout)
		_, err := storage.PruneHistory(ctx, database, metricModule, storage.HistoryPruneOptions{Keep: 1})
		Expect(err).NotTo(HaveOccurred())
		var remaining int64
		Expect(database.Model(&storage.DeclarationMetrics{}).Count(&remaining).Error).To(Succeed())
		Expect(remaining).To(Equal(original))
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		Expect(runQuery(ctx, pipeline, "SELECT symbol.metrics.body_lines FROM func:Small", query.ModuleScopeOptions{SnapshotID: second.SnapshotID}).Rows).To(Equal([]map[string]any{{"symbol.metrics.body_lines": 3}}))
	})
	DescribeTable("selects stored files and callable metrics with source witnesses", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		indexed := indexCheckout(ctx, database, metricCheckout())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID}
		files := runQuery(ctx, pipeline, "SELECT path, file.lines, file.definitions FROM files() WHERE file.lines < 100 AND file.definitions < 3 ORDER BY path LIMIT 2", scope)
		Expect(files.Total).To(Equal(4))
		Expect(files.Rows).To(Equal([]map[string]any{
			{"path": "empty.go", "file.lines": 1, "file.definitions": 0},
			{"path": "interface.go", "file.lines": 2, "file.definitions": 2},
		}))
		Expect(files.Matches).To(HaveLen(2))
		Expect(files.Matches[0].SourceID).NotTo(BeEmpty())
		Expect(files.Matches[0].SnapshotID).To(Equal(indexed.SnapshotID))
		callables := runQuery(ctx, pipeline, "SELECT symbol.name, path, line, symbol.metrics.param_count, symbol.metrics.body_lines FROM func:* WHERE symbol.metrics.param_count > 3 OR symbol.metrics.body_lines IS NULL ORDER BY symbol.name", scope)
		Expect(callables.Rows).To(Equal([]map[string]any{
			{"symbol.name": "Large", "path": "large.go", "line": 2, "symbol.metrics.param_count": 4, "symbol.metrics.body_lines": 4},
			{"symbol.name": "Read", "path": "interface.go", "line": 2, "symbol.metrics.param_count": 2, "symbol.metrics.body_lines": nil},
		}))
		Expect(callables.Matches).To(HaveLen(2))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))

	It("retains metrics for historical documents after layout-only edits", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		checkout := metricCheckout()
		first := indexCheckout(ctx, database, checkout)
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(checkout, "small.go"), []byte("package metrics\nfunc Small(a, b int) {\n\n}\n"), 0o644)).To(Succeed())
		second := indexCheckout(ctx, database, checkout)
		for _, sample := range []struct {
			snapshot string
			lines    int
		}{{first.SnapshotID, 1}, {second.SnapshotID, 3}} {
			result := runQuery(ctx, pipeline, "SELECT symbol.metrics.body_lines FROM func:Small", query.ModuleScopeOptions{SnapshotID: sample.snapshot})
			Expect(result.Rows).To(Equal([]map[string]any{{"symbol.metrics.body_lines": sample.lines}}))
		}
	})

	It("rejects symbol navigation and symbol fields on file sources", func(ctx SpecContext) {
		pipeline, err := query.NewPipeline(openQueryDatabase(ctx, "sqlite"))
		Expect(err).NotTo(HaveOccurred())
		for _, expression := range []string{"SELECT * FROM callers(files())", "SELECT * FROM files(func:*)", "SELECT symbol.name FROM files()"} {
			_, err := pipeline.RunModules(ctx, expression, query.ModuleScopeOptions{})
			Expect(err).To(HaveOccurred(), expression)
		}
	})
})
