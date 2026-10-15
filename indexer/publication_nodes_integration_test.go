package indexer

import (
	"context"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// layersChanged is, per fingerprint layer, whether a symbol's delta differs from its earlier one.
func layersChanged(before, after storage.SymbolDelta) [3]bool {
	return [3]bool{*before.PublicFP != *after.PublicFP, *before.PrivateFP != *after.PrivateFP, *before.ContentFP != *after.ContentFP}
}

var _ = Describe("PublicationFromNodes published", func() {
	DescribeTable("records the layer each schema change touches as symbol deltas",
		func(ctx SpecContext, open func(context.Context) *gorm.DB) {
			database := open(ctx)
			first := publish(ctx, database, schemaPublication("schema-v1"))
			Expect(first).To(Equal(ModuleResult{RootKey: schemaRoot, Location: schemaURI, SnapshotID: first.SnapshotID, HeadVersion: 1, Files: 2, ParsedFiles: 2}))
			initial := symbolDeltas(ctx, database, first.SnapshotID)
			Expect(initial).To(HaveLen(13), "every published symbol is set initially")

			second := publish(ctx, database, schemaPublication("schema-v2"))
			Expect(second).To(Equal(ModuleResult{RootKey: schemaRoot, Location: schemaURI, SnapshotID: second.SnapshotID, HeadVersion: 2, Files: 2, ParsedFiles: 2}))
			customers := Identity{ModuleKey: schemaRoot, PackagePath: schemaPackage, Kind: "table", Name: "customers"}
			email := Identity{ModuleKey: schemaRoot, PackagePath: schemaPackage, Kind: "column", OwnerID: SymbolID(customers), Name: "email"}
			index := Identity{ModuleKey: schemaRoot, PackagePath: schemaPackage, Kind: "index", OwnerID: SymbolID(customers), Name: "ix_customers_name"}
			view := Identity{ModuleKey: schemaRoot, PackagePath: schemaPackage, Kind: "table", Name: "order_totals"}
			changed := symbolDeltas(ctx, database, second.SnapshotID)
			Expect(operations(changed)).To(Equal(map[string]storage.SourceOperation{
				SymbolID(customers): storage.SourceSet, SymbolID(email): storage.SourceSet,
				SymbolID(index): storage.SourceSet, SymbolID(view): storage.SourceSet,
			}), "unchanged tables, columns, and the function write no delta")
			Expect(layersChanged(initial[SymbolID(customers)], changed[SymbolID(customers)])).To(Equal([3]bool{true, true, false}),
				"a new nullable column changes the table's public layer, and so its private one")
			Expect(layersChanged(initial[SymbolID(view)], changed[SymbolID(view)])).To(Equal([3]bool{false, false, true}),
				"a rewritten definition with the same columns changes only the view's content")
		},
		Entry("SQLite", openIndexerSQLite),
		Entry("PostgreSQL", openIndexerPostgres),
	)

	DescribeTable("stores a class, whose type_form and nested extents its UIR document validation accepts",
		func(ctx SpecContext, open func(context.Context) *gorm.DB) {
			database := open(ctx)
			publication, err := PublicationFromNodes(NodePublication{
				RootKey: schemaRoot, Name: "warehouse", Location: ExternalLocation{URI: schemaURI}, Revision: "r1",
				Documents: []NodeDocument{{PathKey: ledgerPath, PackagePath: ledgerPackage, Nodes: []uir.Node{ledgerClass("append_entry")}}},
			})
			Expect(err).ToNot(HaveOccurred())
			result := publish(ctx, database, publication)
			Expect(result).To(Equal(ModuleResult{RootKey: schemaRoot, Location: schemaURI, SnapshotID: result.SnapshotID, HeadVersion: 1, Files: 1, ParsedFiles: 1}))
			Expect(symbolDeltas(ctx, database, result.SnapshotID)).To(HaveLen(5), "the class, its two fields, its constructor, and its method")
		},
		Entry("SQLite", openIndexerSQLite),
		Entry("PostgreSQL", openIndexerPostgres),
	)
})
