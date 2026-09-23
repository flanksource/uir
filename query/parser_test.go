package query_test

import (
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PEG query grammar", func() {
	DescribeTable("parses commands into a typed query",
		func(input string, expected query.Query) {
			parsed, err := query.Parse(input)
			Expect(err).ToNot(HaveOccurred())
			Expect(parsed).To(Equal(expected))
		},
		Entry("all nodes", "nodes", query.Query{Operation: query.OperationNodes}),
		Entry("structured node predicates",
			`nodes where module = "billing" and type = "InvoiceService" and method = "Approve"`,
			query.Query{Operation: query.OperationNodes, Predicates: []query.Predicate{
				{Field: "module", Value: "billing"},
				{Field: "type", Value: "InvoiceService"},
				{Field: "method", Value: "Approve"},
			}},
		),
		Entry("callers with root scope",
			`callers of node where root = "app" and symbol_key = "method:billing.invoices.InvoiceService:Approve"`,
			query.Query{Operation: query.OperationCallers, Predicates: []query.Predicate{
				{Field: "root", Value: "app"},
				{Field: "symbol_key", Value: "method:billing.invoices.InvoiceService:Approve"},
			}},
		),
		Entry("callees", `callees of node where identity_key = "method|Approve"`, query.Query{
			Operation:  query.OperationCallees,
			Predicates: []query.Predicate{{Field: "identity_key", Value: "method|Approve"}},
		}),
		Entry("unresolved calls", "unresolved calls", query.Query{Operation: query.OperationUnresolvedCalls}),
		Entry("root-scoped unresolved calls", `unresolved calls where root = "app"`, query.Query{
			Operation:  query.OperationUnresolvedCalls,
			Predicates: []query.Predicate{{Field: "root", Value: "app"}},
		}),
		Entry("escaped string", `nodes where package = "billing\"core"`, query.Query{
			Operation:  query.OperationNodes,
			Predicates: []query.Predicate{{Field: "package", Value: `billing"core`}},
		}),
	)

	DescribeTable("rejects invalid input",
		func(input string, message string) {
			_, err := query.Parse(input)
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("empty", "", "query"),
		Entry("missing graph selector", "callers of node", "where"),
		Entry("unknown field", `nodes where filename = "main.go"`, "filename"),
		Entry("unquoted value", "nodes where module = billing", "quoted"),
		Entry("trailing input", "unresolved calls now", "now"),
		Entry("unsupported unresolved filter", `unresolved calls where module = "billing"`, "module"),
	)
})
