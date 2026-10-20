package syntax_test

import (
	"github.com/flanksource/uir/indexer/syntax"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Callable metrics", func() {
	DescribeTable("counts value parameters and the physical body span", func(declaration string, params int, bodyLines *int) {
		indexed, err := syntax.Extract("metrics.go", "example.org/metrics", []byte("package metrics\n"+declaration))
		Expect(err).NotTo(HaveOccurred())
		Expect(indexed.Document().Symbols).To(HaveLen(1))
		Expect(indexed.Document().Symbols[0].Metrics).To(Equal(&storage.CallableMetrics{
			Version: storage.MetricsVersion, ParamCount: params, BodyLines: bodyLines,
		}))
	},
		Entry("empty body", "func Empty() {}\n", 0, metricLines(1)),
		Entry("grouped and variadic", "func Grouped(a, b int, rest ...string) {}\n", 3, metricLines(1)),
		Entry("unnamed", "func Unnamed(int, string) {}\n", 2, metricLines(1)),
		Entry("receiver excluded", "func (r Receiver) Method(a int) {}\n", 1, metricLines(1)),
		Entry("generic parameter excluded", "func Generic[T any](a T) {}\n", 1, metricLines(1)),
		Entry("bodyless declaration", "func Assembly(a int)\n", 1, (*int)(nil)),
		Entry("multiline signature and comments", "// Documentation\nfunc Multiline(\n a, b int,\n) {\n // body comment\n\n}\n", 2, metricLines(4)),
		Entry("CRLF body", "func Windows() {\r\n\r\n}\r\n", 0, metricLines(3)),
	)
})

func metricLines(lines int) *int { return &lines }
