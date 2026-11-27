package migrate_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

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
	declaration := func(partial bool) uir.UIR {
		schema, err := migrate.Schema(document(100))
		Expect(err).NotTo(HaveOccurred())
		table := &schema.Tables[0]
		table.Columns = slices.DeleteFunc(table.Columns, func(c model.Column) bool { return c.Name == "total" })
		table.Indexes, table.Checks, table.Partial = table.Indexes[:1], nil, partial
		catalogue, err := migrate.Enrich(uir.UIR{}, model.Schema{Tables: schema.Tables}, declared("dbo"))
		Expect(err).NotTo(HaveOccurred())
		return catalogue
	}
	reference := func() uir.UIR { return declaration(false) }
	// omitted are the members of orders the target holds and the declaration does not name.
	omitted := []migrate.Selection{
		{Kind: "check", Object: orders, Member: "CK_total"},
		{Kind: "column", Object: orders, Member: "total"},
		{Kind: "index", Object: orders, Member: "IX_total"},
	}
	ordersOf := func(schema model.Schema) model.Table {
		at := slices.IndexFunc(schema.Tables, func(t model.Table) bool { return t.Name == orders })
		Expect(at).To(BeNumerically(">=", 0))
		return schema.Tables[at]
	}
	// acknowledged says which drops an artifact requires acknowledging.
	acknowledged := func(artifact engine.Artifact) []string {
		var out []string
		for _, change := range artifact.Changes {
			if change.Action == "drop" && slices.Contains(engine.Acknowledgements(artifact), change.ID) {
				out = append(out, change.Action+" "+change.Kind+" "+change.Member)
			}
		}
		return out
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

	It("plans a selectable, destructive drop for each member a complete declared table omits", func() {
		artifact, err := migrate.BuildArtifact(migrate.Request{Reference: reference(), Target: target(), Selections: omitted})
		Expect(err).NotTo(HaveOccurred())
		Expect(actions(artifact.Changes)).To(ConsistOf("drop column total", "drop index IX_total", "drop check CK_total"))
		Expect(artifact.Preserved).To(BeEmpty())
		Expect(ordersOf(artifact.Desired).Partial).To(BeFalse())
		for _, change := range artifact.Changes {
			Expect(change.Destructive).To(BeTrue(), change.ID)
		}
		Expect(acknowledged(artifact)).To(ConsistOf("drop column total", "drop index IX_total", "drop check CK_total"))
		Expect(engine.Validate(artifact)).To(Succeed())
	})

	It("plans the same drops and the declared change when the complete table is selected whole", func() {
		artifact, err := migrate.BuildArtifact(migrate.Request{Reference: reference(), Target: target(), Selections: []migrate.Selection{{Kind: "table", Object: orders}}})
		Expect(err).NotTo(HaveOccurred())
		Expect(actions(artifact.Changes)).To(ConsistOf("modify column label", "drop column total", "drop index IX_total", "drop check CK_total"))
		Expect(artifact.Preserved).To(BeEmpty())
		Expect(acknowledged(artifact)).To(ConsistOf("drop column total", "drop index IX_total", "drop check CK_total"))
		for _, change := range artifact.Changes {
			Expect(change.Object).NotTo(Equal(audit))
		}
	})

	It("keeps a partial declared table's omitted members and lists them as preserved", func() {
		artifact, err := migrate.BuildArtifact(migrate.Request{Reference: declaration(true), Target: target(), Selections: []migrate.Selection{{Kind: "table", Object: orders}}})
		Expect(err).NotTo(HaveOccurred())
		Expect(actions(artifact.Changes)).To(Equal([]string{"modify column label"}))
		Expect(actions(artifact.Preserved)).To(ConsistOf("drop column total", "drop index IX_total", "drop check CK_total"))
		Expect(acknowledged(artifact)).To(BeEmpty())
		Expect(ordersOf(artifact.Desired).Partial).To(BeTrue())
		for _, change := range slices.Concat(artifact.Changes, artifact.Preserved) {
			Expect(change.Object).NotTo(Equal(audit))
		}
	})

	It("carries the partial flag into the desired schema of a member selection", func() {
		input, err := migrate.Select(migrate.Request{Reference: declaration(true), Target: target(), Selections: []migrate.Selection{{Kind: "column", Object: orders, Member: "label"}}})
		Expect(err).NotTo(HaveOccurred())
		Expect(ordersOf(input.Desired).Partial).To(BeTrue())
		Expect(ordersOf(input.Current).Partial).To(BeFalse())
	})

	DescribeTable("refuses selecting a drop the reference cannot make, and says why",
		func(reference func() uir.UIR, selection migrate.Selection, message string) {
			_, err := migrate.Select(migrate.Request{Reference: reference(), Target: target(), Selections: []migrate.Selection{selection}})
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("a column of a partial table, which keeps what it does not name", func() uir.UIR { return declaration(true) },
			migrate.Selection{Kind: "column", Object: orders, Member: "total"}, "table dbo.orders is declared partial"),
		Entry("an index of a partial table", func() uir.UIR { return declaration(true) },
			migrate.Selection{Kind: "index", Object: orders, Member: "IX_total"}, "table dbo.orders is declared partial"),
		Entry("a member of a table the reference does not declare", reference,
			migrate.Selection{Kind: "column", Object: audit, Member: "id"}, "table dbo.audit is not declared"),
		Entry("an undeclared table, which a declared scope never drops", reference,
			migrate.Selection{Kind: "table", Object: audit}, "table dbo.audit is not declared"),
	)

	It("fails the plan of a partial table the target does not have, with the planner's message", func() {
		invoices := model.Name{Schema: "dbo", Name: "invoices"}
		missing, err := migrate.Enrich(uir.UIR{}, model.Schema{Tables: []model.Table{{Name: invoices, Partial: true, Columns: []model.Column{{Name: "id", Type: "int"}}}}}, declared("dbo"))
		Expect(err).NotTo(HaveOccurred())
		_, err = migrate.BuildArtifact(migrate.Request{Reference: missing, Target: target(), Selections: []migrate.Selection{{Kind: "table", Object: invoices}}})
		Expect(err).To(MatchError(ContainSubstring("declared partial and does not exist")))
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

	It("round trips the partial flag through persisted UIR JSON and omits it when false", func() {
		for _, partial := range []bool{true, false} {
			encoded, err := json.Marshal(declaration(partial))
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Contains(string(encoded), `"partial"`)).To(Equal(partial))
			var decoded uir.UIR
			Expect(uir.Unmarshal(encoded, &decoded)).To(Succeed())
			Expect(decoded.Tables[0].Partial).To(Equal(partial))
			schema, err := migrate.Schema(decoded)
			Expect(err).NotTo(HaveOccurred())
			Expect(schema.Tables[0].Partial).To(Equal(partial))
		}
	})

	It("refuses a partial table in a complete catalogue, which an inspection never produces", func() {
		partial := model.Schema{Tables: []model.Table{{Name: orders, Partial: true, Columns: []model.Column{{Name: "id", Type: "int"}}}}}
		_, err := migrate.Enrich(uir.UIR{}, partial, complete("dbo"))
		Expect(err).To(MatchError(ContainSubstring("complete catalogue cannot hold partial table dbo.orders")))
		stored := declaration(true)
		stored.SQLCatalog.Declared, stored.SQLCatalog.Complete = false, true
		_, err = migrate.Schema(stored)
		Expect(err).To(MatchError(ContainSubstring("complete catalogue cannot hold partial table dbo.orders")))
	})

	It("marks a declared catalogue and refuses a stored one that is neither complete nor declared, or both", func() {
		catalogue := reference()
		Expect(catalogue.SQLCatalog).To(HaveField("Declared", true))
		Expect(catalogue.SQLCatalog).To(HaveField("Complete", false))
		partial := reference()
		partial.SQLCatalog.Declared = false
		_, err := migrate.Select(migrate.Request{Reference: partial, Target: target()})
		Expect(err).To(MatchError(ContainSubstring("reference: SQL catalogue is neither complete nor declared")))
		both := reference()
		both.SQLCatalog.Complete = true
		_, err = migrate.Select(migrate.Request{Reference: both, Target: target()})
		Expect(err).To(MatchError(ContainSubstring("both complete and declared")))
	})

	DescribeTable("names why it refuses an invalid declared catalogue",
		func(mutate func(*uir.SQLCatalog), message string) {
			catalogue := reference()
			mutate(catalogue.SQLCatalog)
			_, err := migrate.Schema(catalogue)
			Expect(err).To(MatchError(message))
		},
		Entry("another version", func(catalog *uir.SQLCatalog) { catalog.Version = migrate.CatalogVersion + 1 },
			fmt.Sprintf("SQL catalogue is at version %d; migration reads version %d", migrate.CatalogVersion+1, migrate.CatalogVersion)),
		Entry("another dialect", func(catalog *uir.SQLCatalog) { catalog.Dialect = "postgres" }, `SQL catalogue dialect is "postgres"; migration reads only SQL Server (tsql)`),
		Entry("no schema", func(catalog *uir.SQLCatalog) { catalog.Schemas = nil }, "SQL catalogue covers no schema"),
	)

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
