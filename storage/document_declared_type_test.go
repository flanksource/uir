package storage_test

import (
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Resolved variable type validation", func() {
	DescribeTable("rejects type facts on non-variables, syntax declarations and old document formats", func(coverage storage.Coverage, version int) {
		document, source, content := typedWorkerDocument()
		document.Coverage, content.Version = coverage, version
		typ := "context.Context"
		content.Symbols[0].DeclaredType = &typ
		_, err := storage.DecodeDocument(encodeDocument(document, content), source)
		Expect(err).To(MatchError(ContainSubstring("declared_type")))
	}, Entry("non-variable", storage.CoverageIndexed, storage.DocumentFormatVersion), Entry("syntax", storage.CoverageSyntax, storage.DocumentFormatVersion), Entry("old format", storage.CoverageIndexed, 7))
})
