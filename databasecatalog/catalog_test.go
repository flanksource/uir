package databasecatalog_test

import (
	"github.com/flanksource/commons-db/inspect/sql"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/databasecatalog"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL catalogue lowering", func() {
	It("keeps exact SQL types and metadata while retaining empty schemas", func() {
		nullable := false
		catalog := sqlinspect.Catalog{Driver: "postgres", Schemas: []sqlinspect.Schema{
			{Name: "archive"},
			{Name: "public", Relations: []sqlinspect.Relation{{Name: "orders", Type: "table", Columns: []sqlinspect.Column{{Name: "id", DataType: "int8", Ordinal: 1, Nullable: &nullable, PrimaryKey: true}},
				Indexes: []sqlinspect.Index{{Name: "orders_pkey", Primary: true, Unique: true, Columns: []string{"id"}}}}}},
		}}
		documents, err := databasecatalog.Documents(catalog)
		Expect(err).ToNot(HaveOccurred())
		Expect(documents).To(HaveLen(2))
		Expect(documents[0].PackagePath).To(Equal("archive"))
		Expect(documents[0].Nodes).To(BeEmpty())
		Expect(documents[1].Nodes).To(HaveLen(1))
		table, ok := documents[1].Nodes[0].(uir.RecordTable)
		Expect(ok).To(BeTrue())
		Expect(table.Package).To(Equal("public"))
		Expect(table.Columns[0].SQLType).To(Equal("int8"))
		Expect(table.Columns[0].PrimaryKey).To(BeTrue())
		Expect(table.Indexes[0].Columns).To(Equal([]string{"id"}))
	})
})
