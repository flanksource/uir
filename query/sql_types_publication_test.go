package query_test

import (
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Published resolved variable types", func() {
	DescribeTable("preserves missing historical facts and validates producer types", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		const root = "example.org/published-state"
		identity := indexer.Identity{ModuleKey: root, PackagePath: root, Kind: "var", Name: "State"}
		source := publishedText("var State context.Context\n")
		symbol := source.declaration(identity, uir.Identifier{Module: root, Package: root, Field: "State", NodeType: uir.NodeTypePackageVariable}, "var State context.Context", "State")
		publication := indexer.Publication{RootKey: root, Name: "state", Location: indexer.ExternalLocation{URI: "test://state"}, Symbols: publishedSymbols([]indexer.Identity{identity}), Documents: []indexer.PublishedDocument{source.document("state.go", root, []storage.DocumentSymbol{symbol}, nil)}}
		publication.Documents[0].Content.Version = 7
		first, err := indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		for _, expression := range []string{"SELECT symbol.type FROM var:State", "SELECT symbol.type FROM var:Missing"} {
			_, err = pipeline.RunModules(ctx, expression, query.ModuleScopeOptions{SnapshotID: first.SnapshotID})
			Expect(err).To(MatchError(And(ContainSubstring(first.SnapshotID), ContainSubstring("state.go"), ContainSubstring("reindex or republish"))))
		}
		publication.Documents[0].Content.Version = storage.DocumentFormatVersion
		typ := "context.Context"
		publication.Documents[0].Content.Symbols[0].DeclaredType = &typ
		second, err := indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		Expect(runQuery(ctx, pipeline, "SELECT symbol.type FROM var:State", query.ModuleScopeOptions{SnapshotID: second.SnapshotID}).Rows).To(Equal([]map[string]any{{"symbol.type": "context.Context"}}))
		_, err = pipeline.RunModules(ctx, "SELECT symbol.type FROM var:State", query.ModuleScopeOptions{SnapshotID: first.SnapshotID})
		Expect(err).To(MatchError(ContainSubstring("reindex or republish")))
		typ = ""
		_, err = indexer.Publish(ctx, database, publication)
		Expect(err).To(MatchError(ContainSubstring("declared_type")))
		Expect(runQuery(ctx, pipeline, "SELECT symbol.type FROM var:State", query.ModuleScopeOptions{SnapshotID: second.SnapshotID}).Rows).To(Equal([]map[string]any{{"symbol.type": "context.Context"}}))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
})
