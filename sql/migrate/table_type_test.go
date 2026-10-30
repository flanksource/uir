package migrate_test

import (
	"encoding/json"

	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/sql/migrate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Table types and sequences", func() {
	fieldList := model.TableType{Name: model.Name{Schema: "dbo", Name: "SpawnActivityFieldList"},
		Columns: []model.Column{
			{Name: "ID", Type: "int", Identity: &model.Identity{Seed: 10, Increment: 2}},
			{Name: "FieldName", Type: "nvarchar(50)", Collation: "Latin1_General_CI_AS"},
			{Name: "Kind", Type: "nvarchar(30)", Nullable: true, Default: &model.Default{Expression: "(N'text')"}},
			{Name: "Value", Type: "sql_variant", Nullable: true},
		},
		Keys: []model.Index{
			{Columns: []model.IndexColumn{{Name: "ID", Descending: true}}, Primary: true, Clustered: true},
			{Columns: []model.IndexColumn{{Name: "FieldName"}}, Unique: true},
		},
		Checks: []string{"([Kind]<>N'')"},
	}
	ids := model.Sequence{Name: model.Name{Schema: "dbo", Name: "ActivityIDs"}, Type: "bigint", Start: "1", Increment: "1",
		Minimum: "-9223372036854775808", Maximum: "9223372036854775807", Cycle: true, CacheSize: 50}
	spawn := model.Definition{Name: model.Name{Schema: "dbo", Name: "SpawnActivitiesByBillGroup"}, Kind: "PROCEDURE",
		SQL: "CREATE PROCEDURE dbo.SpawnActivitiesByBillGroup @BillGroupGUID uniqueidentifier, @Fields dbo.SpawnActivityFieldList READONLY AS SELECT FieldName FROM @Fields"}
	snapshot := model.Schema{TableTypes: []model.TableType{fieldList}, Sequences: []model.Sequence{ids}, Definitions: []model.Definition{spawn}}

	It("round trips table types and sequences a procedure depends on through persisted UIR JSON", func() {
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

	It("keeps target table types when a procedure taking one is selected", func() {
		target, err := migrate.Enrich(uir.UIR{}, model.Schema{TableTypes: snapshot.TableTypes, Sequences: snapshot.Sequences}, []string{"dbo"})
		Expect(err).NotTo(HaveOccurred())
		reference, err := migrate.Enrich(uir.UIR{}, snapshot, []string{"dbo"})
		Expect(err).NotTo(HaveOccurred())
		plan, err := migrate.Plan(migrate.Request{Reference: reference, Target: target, Selections: []migrate.Selection{{Kind: "procedure", Object: spawn.Name}}})
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Steps).To(HaveLen(1))
		Expect(plan.Steps[0].Change.Object).To(Equal(spawn.Name))
	})

	It("fails naming the table type a selected procedure takes when only the reference has it", func() {
		target, err := migrate.Enrich(uir.UIR{}, model.Schema{}, []string{"dbo"})
		Expect(err).NotTo(HaveOccurred())
		reference, err := migrate.Enrich(uir.UIR{}, snapshot, []string{"dbo"})
		Expect(err).NotTo(HaveOccurred())
		_, err = migrate.Select(migrate.Request{Reference: reference, Target: target, Selections: []migrate.Selection{{Kind: "procedure", Object: spawn.Name}}})
		Expect(err).To(MatchError(ContainSubstring(`procedure dbo.SpawnActivitiesByBillGroup takes table type dbo.SpawnActivityFieldList, which the desired schema lacks; select table-type:"dbo"."SpawnActivityFieldList"`)))
	})

	It("creates a selected table type and sequence before the procedure taking the type", func() {
		target, err := migrate.Enrich(uir.UIR{}, model.Schema{}, []string{"dbo"})
		Expect(err).NotTo(HaveOccurred())
		reference, err := migrate.Enrich(uir.UIR{}, snapshot, []string{"dbo"})
		Expect(err).NotTo(HaveOccurred())
		var selections []migrate.Selection
		for _, id := range []string{`procedure:"dbo"."SpawnActivitiesByBillGroup"`, `table-type:"dbo"."SpawnActivityFieldList"`, `sequence:"dbo"."ActivityIDs"`} {
			s, err := migrate.ParseSelection(id)
			Expect(err).NotTo(HaveOccurred())
			selections = append(selections, s)
		}
		plan, err := migrate.Plan(migrate.Request{Reference: reference, Target: target, Selections: selections})
		Expect(err).NotTo(HaveOccurred())
		var changes []string
		for _, step := range plan.Steps {
			changes = append(changes, step.Change.Kind+" "+step.Change.Object.Name)
		}
		Expect(changes).To(ConsistOf("sequence ActivityIDs", "table-type SpawnActivityFieldList", "procedure SpawnActivitiesByBillGroup"))
		Expect(changes[len(changes)-1]).To(Equal("procedure SpawnActivitiesByBillGroup"))
	})

	It("rejects a table type outside catalogue coverage", func() {
		_, err := migrate.Enrich(uir.UIR{}, model.Schema{TableTypes: snapshot.TableTypes}, []string{"sales"})
		Expect(err).To(MatchError(ContainSubstring("outside catalogue coverage")))
	})
})
