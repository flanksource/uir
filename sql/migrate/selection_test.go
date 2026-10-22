package migrate_test

import (
	"encoding/json"
	"slices"

	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/sql/migrate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Selection integrity", func() {
	orders := model.Name{Schema: "dbo", Name: "orders"}
	DescribeTable("preserves explicit dialect types in catalogue round trips", func(typ string) {
		input := model.Schema{Tables: []model.Table{{Name: orders, Columns: []model.Column{{Name: "value", Type: typ}}}}}
		document, err := migrate.Enrich(uir.UIR{}, input, []string{"dbo"})
		Expect(err).NotTo(HaveOccurred())
		output, err := migrate.Schema(document)
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(Equal(input))
	}, Entry("float precision", "float(24)"), Entry("real", "real"), Entry("ntext", "ntext"), Entry("temporal scale zero", "datetime2(0)"), Entry("binary width", "varbinary(max)"))
	It("round trips complete snapshots through persisted UIR JSON", func() {
		original, err := migrate.Schema(document(20))
		Expect(err).NotTo(HaveOccurred())
		original.Definitions = []model.Definition{
			{Name: model.Name{Schema: "dbo", Name: "audit_orders"}, Kind: "TRIGGER", SQL: "CREATE TRIGGER dbo.audit_orders ON dbo.orders AFTER INSERT AS SELECT 1", Parent: &orders, Disabled: true},
			{Name: model.Name{Schema: "dbo", Name: "orders_view"}, Kind: "VIEW", SQL: "CREATE VIEW dbo.orders_view AS SELECT id FROM dbo.orders", Dependencies: []model.Name{orders}},
		}
		u, err := migrate.Enrich(uir.UIR{}, original, []string{"dbo"})
		Expect(err).NotTo(HaveOccurred())
		encoded, err := json.Marshal(u)
		Expect(err).NotTo(HaveOccurred())
		var decoded uir.UIR
		Expect(uir.Unmarshal(encoded, &decoded)).To(Succeed())
		got, err := migrate.Schema(decoded)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(original))
	})

	It("removes a selected target-only index and preserves its other indexes", func() {
		reference, target := document(20), document(20)
		reference.Tables[0].Indexes = reference.Tables[0].Indexes[:1]
		input, err := migrate.Select(migrate.Request{Reference: reference, Target: target, Selections: []migrate.Selection{{Kind: "index", Object: orders, Member: "IX_total"}}})
		Expect(err).NotTo(HaveOccurred())
		Expect(input.Desired.Tables[0].Indexes).To(Equal(input.Current.Tables[0].Indexes[:1]))
		Expect(input.Current.Tables[0].Indexes).To(HaveLen(2))
	})

	It("preserves target-only tables during a partial selection", func() {
		reference, target := document(100), document(20)
		target.Tables = append(target.Tables, uir.NewTable("audit", uir.Identifier{Package: "dbo"}).WithColumns(uir.NewColumn("id", "int", uir.RecordFieldTypeInt)).Build())
		input, err := migrate.Select(migrate.Request{Reference: reference, Target: target, Selections: []migrate.Selection{{Kind: "column", Object: orders, Member: "label"}}})
		Expect(err).NotTo(HaveOccurred())
		Expect(input.Desired.Tables).To(ContainElement(model.Table{Name: model.Name{Schema: "dbo", Name: "audit"}, Columns: []model.Column{{Name: "id", Type: "int"}}}))
	})

	It("produces identical plans regardless of selection order", func() {
		reference, target := document(100), document(20)
		reference.Tables[0].Checks[0].Expression = "[total] > 0"
		selections := []migrate.Selection{{Kind: "column", Object: orders, Member: "label"}, {Kind: "check", Object: orders, Member: "CK_total"}}
		a, err := migrate.Plan(migrate.Request{Reference: reference, Target: target, Selections: selections})
		Expect(err).NotTo(HaveOccurred())
		slices.Reverse(selections)
		b, err := migrate.Plan(migrate.Request{Reference: reference, Target: target, Selections: selections})
		Expect(err).NotTo(HaveOccurred())
		Expect(b).To(Equal(a))
	})

	DescribeTable("rejects invalid or conflicting selections",
		func(selections []migrate.Selection, message string) {
			_, err := migrate.Select(migrate.Request{Reference: document(100), Target: document(20), Selections: selections})
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("unknown member", []migrate.Selection{{Kind: "column", Object: orders, Member: "missing"}}, "unknown object or member"),
		Entry("duplicate", []migrate.Selection{{Kind: "column", Object: orders, Member: "label"}, {Kind: "column", Object: orders, Member: "label"}}, "duplicate selection"),
		Entry("whole and member", []migrate.Selection{{Kind: "table", Object: orders}, {Kind: "column", Object: orders, Member: "label"}}, "overlaps"),
		Entry("unknown kind", []migrate.Selection{{Kind: "data", Object: orders}}, "unsupported selection kind"),
	)

	It("rejects deleting a column still used by an unselected index", func() {
		reference, target := document(20), document(20)
		reference.Tables[0].Columns = reference.Tables[0].Columns[:2]
		reference.Tables[0].Indexes = reference.Tables[0].Indexes[:1]
		_, err := migrate.Select(migrate.Request{Reference: reference, Target: target, Selections: []migrate.Selection{{Kind: "column", Object: orders, Member: "label"}}})
		Expect(err).To(MatchError(ContainSubstring("missing included column label")))
	})
})
