package sqlsyntax_test

import (
	"github.com/flanksource/uir/query/sqlsyntax"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL navigation grammar", func() {
	DescribeTable("accepts the documented syntax", func(input string) {
		_, err := sqlsyntax.Parse(input)
		Expect(err).NotTo(HaveOccurred())
	},
		Entry("callers and insensitive matching", "SELECT symbol, path, line FROM callers(method:store.Store.Save) WHERE symbol.name IMATCH '*save*';"),
		Entry("namespaced selector and source alias", "SELECT * FROM http.route:* AS r WHERE r.method = 'POST' AND r.path IMATCH '/orders/**'"),
		Entry("native incoming options", "SELECT * FROM incoming(messaging.topic:OrderCreated, via => 'messaging.publishes_to')"),
		Entry("graph bounds", "SELECT * FROM graph(k8s.service:checkout, via => 'k8s.selects,k8s.uses_config', direction => 'outgoing', depth => 3)"),
		Entry("nested navigation", "SELECT symbol.name AS name FROM callers(callees(method:Store.Save)) ORDER BY name DESC LIMIT 20"),
		Entry("excluded file option", "SELECT path FROM files(include_excluded => TRUE) WHERE file.build_constraint NOT REQUIRES 'hack'"),
		Entry("required tag with alias", "SELECT f.build_constraint FROM files(include_excluded => FALSE) AS f WHERE NOT (f.build_constraint REQUIRES 'hack')"),
		Entry("membership and predicate precedence", "select * from func:* where NOT (symbol IN method:Store.* OR line IS NULL) AND depth >= 2"),
		Entry("quoted selector", `SELECT * FROM definitions(func:"Premium Calc (Annual)")`),
		Entry("quoted contextual selector", `SELECT * FROM pkg:"Regional Plan":"Group Life/**"`),
		Entry("escaped glob and doubled quote", `SELECT * FROM func:* WHERE symbol.name MATCH 'Say ''Hi'' \*'`),
		Entry("Unicode strings and boolean properties", "SELECT symbol.props.enabled FROM http.route:* WHERE symbol.name IMATCH 'Σ*' AND symbol.props.enabled = TRUE"),
	)

	DescribeTable("rejects syntax outside version one", func(input string) {
		_, err := sqlsyntax.Parse(input)
		Expect(err).To(HaveOccurred())
	},
		Entry("empty query", ""),
		Entry("missing source", "SELECT *"),
		Entry("empty selector", "SELECT * FROM method:"),
		Entry("empty contextual selector", "SELECT * FROM pkg::store"),
		Entry("third selector separator", "SELECT * FROM pkg:one:two:three"),
		Entry("join", "SELECT * FROM func:* JOIN method:* ON symbol.id = symbol.id"),
		Entry("aggregation", "SELECT count(*) FROM func:*"),
		Entry("subquery", "SELECT * FROM (SELECT * FROM func:*)"),
		Entry("write", "DELETE FROM func:*"),
		Entry("multiple statements", "SELECT * FROM func:*; SELECT * FROM method:*"),
		Entry("invalid option assignment", "SELECT * FROM graph(func:*, depth = 3)"),
		Entry("unclosed string", "SELECT * FROM func:* WHERE symbol.name = 'unclosed"),
	)

	It("retains nested source selectors", func() {
		parsed, err := sqlsyntax.Parse("SELECT * FROM callers(callees(method:Store.Save))")
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Source.Name).To(Equal("callers"))
		Expect(parsed.Source.Parameters[0].Source.Name).To(Equal("callees"))
		Expect(parsed.Source.Parameters[0].Source.Parameters[0].Source.Selector).To(Equal("method:Store.Save"))
	})
})
