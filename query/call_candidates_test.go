package query_test

import (
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("snapshot call candidates", func() {
	It("reads typed documents from an explicit snapshot", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		pipeline, scope := insurancePipeline(ctx, database)
		browse, err := pipeline.BrowseModules(ctx, scope)
		Expect(err).ToNot(HaveOccurred())
		documents, err := pipeline.SnapshotDocuments(ctx, browse.Sources[0].SnapshotID)
		Expect(err).ToNot(HaveOccurred())
		Expect(documents).To(ContainElement(And(HaveField("Path", applyPath), HaveField("Content", HaveField("PackagePath", insRules)))))
	})
	It("returns authored call sites from the selected snapshot", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		pipeline, scope := insurancePipeline(ctx, database)
		browse, err := pipeline.BrowseModules(ctx, scope)
		Expect(err).ToNot(HaveOccurred())
		calls, err := pipeline.FindCallCandidates(ctx, query.CallCandidateRequest{
			SnapshotID: browse.Sources[0].SnapshotID, Names: []string{"Calc"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(calls).To(HaveLen(1))
		Expect(calls[0].Path).To(Equal(applyPath))
		Expect(calls[0].Enclosing.Method).To(Equal("Apply"))
		Expect(calls[0].Target.Method).To(Equal("Calc"))
		Expect(calls[0].Text).To(Equal(`<Call rule="Calc"/>`))
	})
})
