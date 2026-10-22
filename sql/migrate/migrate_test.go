package migrate_test

import (
	"testing"

	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/sql/migrate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMigrations(t *testing.T) { RegisterFailHandler(Fail); RunSpecs(t, "Selected SQL migrations") }

func document(width int) uir.UIR {
	id := uir.NewColumn("id", "int", uir.RecordFieldTypeInt)
	id.PrimaryKey = true
	id.Identity, id.AutoIncrement = &uir.SQLIdentity{Seed: 1000, Increment: 5}, true
	total := uir.NewColumn("total", "decimal", uir.RecordFieldTypeFloat)
	total.Precision, total.Scale = new(18), new(2)
	label := uir.NewColumn("label", "nvarchar", uir.RecordFieldTypeString)
	label.MaxLength = new(width)
	label.SQLDefault = &uir.SQLDefault{Name: "DF_label", Expression: "(N'pending')"}
	label.Collation = "Latin1_General_CI_AS"
	key := uir.NewIndex("PK_orders", "id")
	key.Primary, key.Unique, key.Clustered = true, true, true
	index := uir.NewIndex("IX_total", "total")
	index.Descending, index.Included, index.Condition = []string{"total"}, []string{"label"}, "[total] > 0"
	table := uir.NewTable("orders", uir.Identifier{Package: "dbo"}).WithColumns(id, total, label).WithIndexes(key, index).Build()
	table.Checks = []uir.SQLCheck{{Name: "CK_total", Expression: "[total] >= 0"}}
	return uir.UIR{SQLCatalog: &uir.SQLCatalog{Version: migrate.CatalogVersion, Dialect: "tsql", Schemas: []string{"dbo"}, Complete: true}, Tables: []uir.RecordTable{table}}
}

var _ = Describe("Selected migrations", func() {
	orders := model.Name{Schema: "dbo", Name: "orders"}
	It("changes one selected column and preserves unselected target members", func() {
		reference, target := document(100), document(20)
		reference.Tables[0].Checks = nil
		reference.Tables[0].Indexes = reference.Tables[0].Indexes[:1]
		input, err := migrate.Select(migrate.Request{Reference: reference, Target: target, Selections: []migrate.Selection{{Kind: "column", Object: orders, Member: "label"}}})
		Expect(err).NotTo(HaveOccurred())
		Expect(input.Desired.Tables[0].Columns[2].Type).To(Equal("nvarchar(100)"))
		Expect(input.Desired.Tables[0].Indexes).To(Equal(input.Current.Tables[0].Indexes))
		Expect(input.Desired.Tables[0].Checks).To(Equal(input.Current.Tables[0].Checks))
		Expect(*target.Tables[0].Columns[2].MaxLength).To(Equal(20))
		plan, err := migrate.Plan(migrate.Request{Reference: reference, Target: target, Selections: []migrate.Selection{{Kind: "column", Object: orders, Member: "label"}}})
		Expect(err).NotTo(HaveOccurred())
		var statements []string
		for _, step := range plan.Steps {
			statements = append(statements, step.SQL)
		}
		Expect(statements).To(Equal([]string{
			"ALTER TABLE [dbo].[orders] DROP CONSTRAINT [DF_label]",
			"ALTER TABLE [dbo].[orders] ALTER COLUMN [label] nvarchar(100) COLLATE Latin1_General_CI_AS NOT NULL",
			"ALTER TABLE [dbo].[orders] ADD CONSTRAINT [DF_label] DEFAULT (N'pending') FOR [label]",
		}))
	})

	It("projects the complete column, index and check definitions", func() {
		schema, err := migrate.Schema(document(20))
		Expect(err).NotTo(HaveOccurred())
		Expect(schema.Tables).To(Equal([]model.Table{{Name: orders,
			Columns: []model.Column{
				{Name: "id", Type: "int", Identity: &model.Identity{Seed: 1000, Increment: 5}},
				{Name: "total", Type: "decimal(18,2)"},
				{Name: "label", Type: "nvarchar(20)", Default: &model.Default{Name: "DF_label", Expression: "(N'pending')"}, Collation: "Latin1_General_CI_AS"},
			}, Indexes: []model.Index{
				{Name: "PK_orders", Columns: []model.IndexColumn{{Name: "id"}}, Primary: true, Unique: true, Clustered: true},
				{Name: "IX_total", Columns: []model.IndexColumn{{Name: "total", Descending: true}}, Included: []string{"label"}, Filter: "[total] > 0"},
			}, Checks: []model.Check{{Name: "CK_total", Expression: "[total] >= 0"}},
		}}))
	})

	It("rejects snapshots whose catalogue coverage is unknown", func() {
		u := document(20)
		u.SQLCatalog = nil
		_, err := migrate.Schema(u)
		Expect(err).To(MatchError(ContainSubstring("complete SQL Server catalogue")))
	})
})
