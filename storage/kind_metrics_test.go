package storage_test

import (
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const ruleKind = "oipa.rule"

var ruleMetrics = map[string]storage.MetricSpec{"variables": {Description: "declared variables"}, "fan_in": {Description: "distinct callers"}}

var _ = Describe("Producer metrics", func() {
	DescribeTable("refuses a metric name that is not a new lower snake_case identifier", func(name string) {
		Expect(storage.ValidateMetricName(name)).To(MatchError(ContainSubstring("metric %q", name)))
	},
		Entry("upper case", "Fan_in"),
		Entry("leading digit", "1_fan"),
		Entry("repeated underscore", "fan__in"),
		Entry("trailing underscore", "fan_"),
		Entry("the callable param_count", "param_count"),
		Entry("the callable body_lines", "body_lines"),
	)

	DescribeTable("evolves a registered kind's metrics only by adding names", func(declared map[string]storage.MetricSpec, refused string) {
		err := storage.EvolveKindMetrics(ruleKind, map[string]storage.MetricSpec{"variables": ruleMetrics["variables"]}, declared)
		if refused == "" {
			Expect(err).NotTo(HaveOccurred())
			return
		}
		Expect(err).To(MatchError(And(ContainSubstring(ruleKind), ContainSubstring(refused))))
	},
		Entry("adding fan_in", ruleMetrics, ""),
		Entry("removing variables", map[string]storage.MetricSpec{"fan_in": ruleMetrics["fan_in"]}, `metric "variables" is no longer declared`),
		Entry("redescribing variables", map[string]storage.MetricSpec{"variables": {Description: "locals"}}, `metric "variables" changes its description`),
	)

	DescribeTable("requires exactly the kind's declared metrics on each of its symbols", func(kind string, measures map[string]int64, refused string) {
		catalog := storage.DomainCatalog{Metrics: map[string]map[string]storage.MetricSpec{ruleKind: ruleMetrics}}
		id := digest("rule")
		err := catalog.Validate(storage.DocumentContent{Symbols: []storage.DocumentSymbol{{ID: &id, Key: "Rule", Kind: kind, Measures: measures}}})
		if refused == "" {
			Expect(err).NotTo(HaveOccurred())
			return
		}
		Expect(err).To(MatchError(And(ContainSubstring(kind), ContainSubstring("Rule"), ContainSubstring(refused))))
	},
		Entry("every declared metric", ruleKind, map[string]int64{"variables": 12, "fan_in": 0}, ""),
		Entry("an undeclared metric", ruleKind, map[string]int64{"variables": 12, "fan_in": 2, "fanin": 1}, `metric "fanin"`),
		Entry("a missing declared metric", ruleKind, map[string]int64{"variables": 12}, `metric "fan_in"`),
		Entry("measures on a kind without metrics", "oipa.transaction", map[string]int64{"variables": 1}, `metric "variables"`),
	)

	DescribeTable("rejects measures the document format or declaration cannot carry", func(mutate func(*storage.DocumentContent), refused string) {
		document, source, content := typedWorkerDocument()
		content.Symbols[2].Measures = map[string]int64{"variables": 3}
		mutate(&content)
		_, err := storage.DecodeDocument(encodeDocument(document, content), source)
		Expect(err).To(MatchError(ContainSubstring(refused)))
	},
		Entry("format 9", func(content *storage.DocumentContent) { content.Version = 9 }, "measures require document format 10"),
		Entry("a negative value", func(content *storage.DocumentContent) { content.Symbols[2].Measures["variables"] = -1 }, `metric "variables" has negative value -1`),
		Entry("no canonical id", func(content *storage.DocumentContent) { content.Symbols[2].ID = nil }, "measures require a declaration with a canonical id"),
	)
})
