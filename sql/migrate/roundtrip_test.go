package migrate_test

import (
	"encoding/json"

	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/sql/migrate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fullSchema sets every field model.Schema holds to a non-zero value somewhere,
// so a field Enrich or Schema drops breaks equality.
func fullSchema() model.Schema {
	customers, orders := model.Name{Schema: "dbo", Name: "customers"}, model.Name{Schema: "dbo", Name: "orders"}
	return model.Schema{
		Tables: []model.Table{
			{Name: customers, Columns: []model.Column{{Name: "id", Type: "int"}},
				Indexes: []model.Index{{Name: "PK_customers", Columns: []model.IndexColumn{{Name: "id"}}, Unique: true, Primary: true, Clustered: true, Constraint: true}}},
			{Name: orders,
				Columns: []model.Column{
					{Name: "id", Type: "int", Identity: &model.Identity{Seed: 100, Increment: 5}},
					{Name: "row_id", Type: "uniqueidentifier", RowGUID: true, Default: &model.Default{Name: "DF__orders__row_id__1A2B3C4D", Expression: "(newsequentialid())", SystemNamed: true}},
					{Name: "customer_id", Type: "int", Nullable: true},
					{Name: "label", Type: "nvarchar(20)", Collation: "Latin1_General_CI_AS", Default: &model.Default{Name: "DF_orders_label", Expression: "(N'new')"}},
				},
				Indexes: []model.Index{
					{Name: "PK_orders", Columns: []model.IndexColumn{{Name: "id"}}, Unique: true, Primary: true, Clustered: true, Constraint: true, FillFactor: 80},
					{Name: "IX_orders_label", Columns: []model.IndexColumn{{Name: "label", Descending: true}}, Included: []string{"customer_id"}, Filter: "([label] IS NOT NULL)", FillFactor: 90},
				},
				ForeignKeys: []model.ForeignKey{{Name: "FK_orders_customers", Columns: []string{"customer_id"}, References: customers, ReferencedColumns: []string{"id"}, OnDelete: "SET NULL", OnUpdate: "CASCADE"}},
				Checks:      []model.Check{{Name: "CK_orders_label", Expression: "([label]<>N'')"}},
			},
		},
		Definitions: []model.Definition{
			{Name: model.Name{Schema: "dbo", Name: "audit_orders"}, Kind: "TRIGGER", SQL: "CREATE TRIGGER dbo.audit_orders ON dbo.orders AFTER INSERT AS SELECT 1",
				Parent: &orders, Disabled: true, QuotedIdentifierOff: true, RuntimeDependencies: []model.RuntimeDependency{{Server: "LINKED", Database: "hr", Schema: "dbo", Name: "people"}}},
			{Name: model.Name{Schema: "dbo", Name: "list_orders"}, Kind: "PROCEDURE", SQL: "CREATE PROCEDURE dbo.list_orders AS SELECT id FROM dbo.orders",
				QuotedIdentifierOff: true, Dependencies: []model.Name{orders}, RuntimeDependencies: []model.RuntimeDependency{{Database: "archive", Schema: "dbo", Name: "orders"}, {Name: "missing_proc"}}},
			{Name: model.Name{Schema: "dbo", Name: "orders_view"}, Kind: "VIEW", SQL: "CREATE VIEW dbo.orders_view AS SELECT id FROM dbo.orders",
				QuotedIdentifierOff: true, Dependencies: []model.Name{orders}, RuntimeDependencies: []model.RuntimeDependency{{Schema: "audit", Name: "orders_log"}}},
		},
		Sequences: []model.Sequence{{Name: model.Name{Schema: "dbo", Name: "OrderIDs"}, Type: "int", Start: "1", Increment: "1", Minimum: "1", Maximum: "2147483647", NoCache: true}},
		TableTypes: []model.TableType{{Name: model.Name{Schema: "dbo", Name: "OrderList"},
			Columns: []model.Column{{Name: "id", Type: "int"}, {Name: "row_id", Type: "uniqueidentifier", Nullable: true}},
			Keys:    []model.Index{{Columns: []model.IndexColumn{{Name: "id"}}, Unique: true, Primary: true, Clustered: true}},
			Checks:  []string{"([id]>(0))"},
		}},
	}
}

var _ = Describe("Catalogue round trip", func() {
	It("projects back every field the snapshot holds through persisted UIR JSON", func() {
		snapshot := fullSchema()
		document, err := migrate.Enrich(uir.UIR{}, snapshot, []string{"dbo"})
		Expect(err).NotTo(HaveOccurred())
		encoded, err := json.Marshal(document)
		Expect(err).NotTo(HaveOccurred())
		var decoded uir.UIR
		Expect(uir.Unmarshal(encoded, &decoded)).To(Succeed())
		got, err := migrate.Schema(decoded)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(snapshot))
	})

	It("keeps the extractor's table statistics and annotations while the snapshot's facts win", func() {
		extracted := uir.NewTable("orders", uir.Identifier{Package: "dbo"}).Build()
		extracted.Description, extracted.RowCount = "Customer orders", new(int64(42))
		extracted.Comments = []uir.Comment{{Text: "orders placed online", Type: uir.CommentTypeDocumentation}}
		label := uir.NewColumn("label", "varchar", uir.RecordFieldTypeString)
		label.Label, label.Nullable, label.MaxLength = "Order label", true, new(5)
		label.Comments = []uir.Comment{{Text: "shown on invoices", Type: uir.CommentTypeDocumentation}}
		label.DefaultValue = &uir.TypedValue{Str: new("new")}
		extracted.Columns = []uir.RecordColumn{label}

		document, err := migrate.Enrich(uir.UIR{Tables: []uir.RecordTable{extracted}}, fullSchema(), []string{"dbo"})
		Expect(err).NotTo(HaveOccurred())
		orders := document.Tables[1]
		Expect([]any{orders.Description, *orders.RowCount, orders.Comments}).To(Equal([]any{extracted.Description, int64(42), extracted.Comments}))
		got := orders.Columns[3]
		Expect([]any{got.Label, got.Comments, got.DefaultValue, got.Ordinal}).To(Equal([]any{label.Label, label.Comments, label.DefaultValue, 4}))
		Expect([]any{got.SQLType, got.Nullable, *got.MaxLength}).To(Equal([]any{"nvarchar", false, 20}))
		_, err = migrate.Schema(document)
		Expect(err).NotTo(HaveOccurred())
	})
})
