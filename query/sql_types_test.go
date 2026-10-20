package query_test

import (
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
)

var _ = Describe("SQL resolved variable types", func() {
	DescribeTable("resolves aliases and inferred types while preserving history and witnesses", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		root := metricCheckout()
		path := filepath.Join(root, "state.go")
		Expect(os.WriteFile(path, []byte("package metrics\nimport ctx \"context\"\ntype Alias = ctx.Context\ntype Named ctx.Context\nvar Global Alias\nvar Pointer *Alias\nvar Inferred = ctx.Background()\nvar Wrapped []*Alias\nvar Distinct Named\nfunc Use() ctx.Context { return Global }\n"), 0o600)).To(Succeed())
		first := indexCheckout(ctx, database, root)
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{SnapshotID: first.SnapshotID}
		original := runQuery(ctx, pipeline, "SELECT symbol.type FROM var:Global", scope)
		Expect(indexCheckout(ctx, database, root).SnapshotID).To(Equal(first.SnapshotID))
		statement := "SELECT symbol.name AS name, symbol.type AS type FROM var:* ORDER BY name"
		Expect(runQuery(ctx, pipeline, statement, scope).Rows).To(Equal([]map[string]any{
			{"name": "Distinct", "type": metricModule + ".Named"},
			{"name": "Global", "type": "context.Context"},
			{"name": "Inferred", "type": "context.Context"},
			{"name": "Pointer", "type": "*context.Context"},
			{"name": "Wrapped", "type": "[]*context.Context"},
		}))
		refs := runQuery(ctx, pipeline, "SELECT path, line, symbol.type FROM references(var:Global)", scope)
		Expect(refs.Rows).To(Equal([]map[string]any{{"path": "state.go", "line": 10, "symbol.type": "context.Context"}}))
		Expect(runQuery(ctx, pipeline, "SELECT symbol.type, symbol.metrics.param_count FROM func:Use", scope).Rows).To(Equal([]map[string]any{{"symbol.type": nil, "symbol.metrics.param_count": 0}}))
		catalog, err := pipeline.QueryCatalog(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(catalog.Fields).To(HaveKeyWithValue("symbol.type", "string"))
		Expect(os.WriteFile(path, []byte("package metrics\nvar Global int\n"), 0o600)).To(Succeed())
		second := indexCheckout(ctx, database, root)
		changed := runQuery(ctx, pipeline, "SELECT symbol.type FROM var:Global", query.ModuleScopeOptions{SnapshotID: second.SnapshotID})
		Expect(changed.Rows).To(Equal([]map[string]any{{"symbol.type": "int"}}))
		Expect(changed.Matches[0].SymbolID).To(Equal(original.Matches[0].SymbolID))
		Expect(runQuery(ctx, pipeline, "SELECT symbol.type FROM var:Global", scope).Rows).To(Equal([]map[string]any{{"symbol.type": "context.Context"}}))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
	It("fails type queries on incomplete coverage before a selector can hide unknown declarations", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		root := metricCheckout()
		Expect(os.WriteFile(filepath.Join(root, "broken.go"), []byte("package metrics\nvar Global = missing\n"), 0o600)).To(Succeed())
		indexed := indexCheckout(ctx, database, root)
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		_, err = pipeline.RunModules(ctx, "SELECT symbol.type FROM var:DoesNotExist", query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID})
		Expect(err).To(MatchError(And(ContainSubstring("complete typed coverage"), ContainSubstring(indexed.SnapshotID))))
	})
})
