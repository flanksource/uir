package query_test

import (
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL analysis requirements", func() {
	DescribeTable("selects the minimum required facts from the complete query", func(input string, mode storage.AnalysisMode) {
		plan, err := query.AnalyzeSQLRows(input)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Mode).To(Equal(mode))
	},
		Entry("files", "SELECT path FROM files()", storage.AnalysisSyntax),
		Entry("headers", "SELECT path FROM files(include_excluded => TRUE) WHERE file.build_constraint NOT REQUIRES 'hack'", storage.AnalysisSyntax),
		Entry("metrics and aliases", "SELECT symbol.name AS name FROM func:* WHERE symbol.metrics.param_count > 4 ORDER BY name", storage.AnalysisSyntax),
		Entry("predicate type", "SELECT symbol.name FROM var:* WHERE symbol.type = 'context.Context'", storage.AnalysisTyped),
		Entry("references", "SELECT path FROM references(pkg:example.org/library)", storage.AnalysisTyped),
		Entry("canonical id", "SELECT symbol.id FROM func:*", storage.AnalysisTyped),
		Entry("wildcard", "SELECT * FROM func:*", storage.AnalysisTyped),
	)
})
