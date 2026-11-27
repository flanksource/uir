package migrate_test

import (
	"slices"

	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/sqlglot-go/migrate/engine"
	"github.com/flanksource/uir/sql/migrate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Selected migration artifacts", func() {
	It("binds a complete selection to a stable artifact while retaining unselected target facts", func() {
		reference, target := document(100), document(20)
		reference.Tables[0].Checks = nil
		selections := []migrate.Selection{
			{Kind: "column", Object: model.Name{Schema: "dbo", Name: "orders"}, Member: "label"},
			{Kind: "check", Object: model.Name{Schema: "dbo", Name: "orders"}, Member: "CK_total"},
		}
		artifact, err := migrate.BuildArtifact(migrate.Request{Reference: reference, Target: target, Selections: selections})
		Expect(err).NotTo(HaveOccurred())
		Expect(engine.Validate(artifact)).To(Succeed())
		Expect(artifact.Scope.Mode).To(Equal(engine.ScopeComplete))
		Expect(artifact.Desired.Tables[0].Checks).To(BeEmpty())
		Expect(artifact.Desired.Tables[0].Indexes).To(ConsistOf(artifact.Baseline.Tables[0].Indexes))
		slices.Reverse(selections)
		again, err := migrate.BuildArtifact(migrate.Request{Reference: reference, Target: target, Selections: selections})
		Expect(err).NotTo(HaveOccurred())
		Expect(again.Hash).To(Equal(artifact.Hash))
		*target.Tables[0].Columns[2].MaxLength = 30
		Expect(engine.Validate(artifact)).To(Succeed())
	})

	It("refuses incomplete snapshots before compiling an artifact", func() {
		target := document(20)
		target.SQLCatalog = nil
		_, err := migrate.BuildArtifact(migrate.Request{Reference: document(100), Target: target})
		Expect(err).To(MatchError(ContainSubstring("document has no SQL catalogue")))
	})
})
