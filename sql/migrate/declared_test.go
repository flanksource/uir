package migrate_test

import (
	"slices"

	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/sqlglot-go/migrate/engine"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/sql/migrate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Declared reference catalogues", func() {
	orders := model.Name{Schema: "dbo", Name: "orders"}
	audit := model.Name{Schema: "dbo", Name: "audit"}

	// target holds orders and an audit table the declared reference never mentions.
	target := func() uir.UIR {
		t := document(20)
		t.Tables = append(t.Tables, uir.NewTable("audit", uir.Identifier{Package: "dbo"}).WithColumns(uir.NewColumn("id", "int", uir.RecordFieldTypeInt)).Build())
		return t
	}
	// reference declares orders with a wider label and without total, its index and its check.
	reference := func() uir.UIR {
		schema, err := migrate.Schema(document(100))
		Expect(err).NotTo(HaveOccurred())
		table := &schema.Tables[0]
		table.Columns = slices.DeleteFunc(table.Columns, func(c model.Column) bool { return c.Name == "total" })
		table.Indexes, table.Checks = table.Indexes[:1], nil
		catalogue, err := migrate.Enrich(uir.UIR{}, model.Schema{Tables: schema.Tables}, declared("dbo"))
		Expect(err).NotTo(HaveOccurred())
		return catalogue
	}
	actions := func(changes []model.Change) []string {
		var out []string
		for _, c := range changes {
			out = append(out, c.Action+" "+c.Kind+" "+c.Member)
		}
		return out
	}

	It("plans in declared scope over the declared objects and leaves undeclared target objects out of the changes", func() {
		artifact, err := migrate.BuildArtifact(migrate.Request{Reference: reference(), Target: target(), Selections: []migrate.Selection{{Kind: "column", Object: orders, Member: "label"}}})
		Expect(err).NotTo(HaveOccurred())
		Expect(artifact.Scope).To(Equal(engine.Scope{Mode: engine.ScopeDeclared, Schemas: []string{"dbo"}, Objects: []model.Name{orders}}))
		Expect(actions(artifact.Changes)).To(Equal([]string{"modify column label"}))
		Expect(artifact.Preserved).To(BeEmpty())
		Expect(artifact.Context).To(Equal([]model.Name{audit}))
		Expect(engine.Validate(artifact)).To(Succeed())
	})

	It("never plans a drop for what the declared table lacks and lists it as preserved", func() {
		artifact, err := migrate.BuildArtifact(migrate.Request{Reference: reference(), Target: target(), Selections: []migrate.Selection{{Kind: "table", Object: orders}}})
		Expect(err).NotTo(HaveOccurred())
		Expect(actions(artifact.Changes)).To(Equal([]string{"modify column label"}))
		Expect(actions(artifact.Preserved)).To(ConsistOf("drop column total", "drop index IX_total", "drop check CK_total"))
		for _, change := range slices.Concat(artifact.Changes, artifact.Preserved) {
			Expect(change.Object).NotTo(Equal(audit))
		}
	})

	It("refuses a selection of something only the target holds, since a declared scope cannot drop it", func() {
		_, err := migrate.Select(migrate.Request{Reference: reference(), Target: target(), Selections: []migrate.Selection{{Kind: "column", Object: orders, Member: "total"}}})
		Expect(err).To(MatchError(ContainSubstring("does not declare; a declared scope never plans a drop")))
	})

	It("plans the same selection of a complete reference in complete scope, where selecting a target-only member drops it", func() {
		complete := document(20)
		complete.Tables[0].Indexes = complete.Tables[0].Indexes[:1]
		artifact, err := migrate.BuildArtifact(migrate.Request{Reference: complete, Target: document(20), Selections: []migrate.Selection{{Kind: "index", Object: orders, Member: "IX_total"}}})
		Expect(err).NotTo(HaveOccurred())
		Expect(artifact.Scope.Mode).To(Equal(engine.ScopeComplete))
		Expect(actions(artifact.Changes)).To(Equal([]string{"drop index IX_total"}))
		Expect(engine.Acknowledgements(artifact)).To(HaveLen(1))
	})

	It("marks a declared catalogue and refuses a stored one that is neither complete nor declared, or both", func() {
		catalogue := reference()
		Expect(catalogue.SQLCatalog).To(HaveField("Declared", true))
		Expect(catalogue.SQLCatalog).To(HaveField("Complete", false))
		partial := reference()
		partial.SQLCatalog.Declared = false
		_, err := migrate.Select(migrate.Request{Reference: partial, Target: target()})
		Expect(err).To(MatchError(ContainSubstring("reference: migration requires a complete SQL Server catalogue")))
		both := reference()
		both.SQLCatalog.Complete = true
		_, err = migrate.Select(migrate.Request{Reference: both, Target: target()})
		Expect(err).To(MatchError(ContainSubstring("both complete and declared")))
	})

	It("refuses a declared target, which is not a baseline", func() {
		_, err := migrate.Select(migrate.Request{Reference: reference(), Target: reference()})
		Expect(err).To(MatchError(ContainSubstring("target must be a complete")))
	})

	It("refuses a declared schema the target does not cover", func() {
		other, err := migrate.Enrich(uir.UIR{}, model.Schema{Tables: []model.Table{{Name: model.Name{Schema: "sales", Name: "orders"}, Columns: []model.Column{{Name: "id", Type: "int"}}}}}, declared("sales"))
		Expect(err).NotTo(HaveOccurred())
		_, err = migrate.Select(migrate.Request{Reference: other, Target: target()})
		Expect(err).To(MatchError(ContainSubstring(`declared reference schema "sales" is outside the target catalogue`)))
	})

	It("rejects a catalogue scope that is neither complete nor declared, or names objects", func() {
		_, err := migrate.Enrich(uir.UIR{}, model.Schema{}, engine.Scope{Schemas: []string{"dbo"}})
		Expect(err).To(HaveOccurred())
		_, err = migrate.Enrich(uir.UIR{}, model.Schema{}, engine.Scope{Mode: engine.ScopeDeclared, Schemas: []string{"dbo"}, Objects: []model.Name{orders}})
		Expect(err).To(HaveOccurred())
	})
})
