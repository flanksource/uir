package query

import (
	"context"
	"strings"

	"github.com/flanksource/uir/query/sqlsyntax"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL predicates", func() {
	DescribeTable("matches anchored Unicode glob patterns",
		func(expression, value string, expected sqlTruth) {
			parsed, err := sqlsyntax.Parse("SELECT * FROM func:* WHERE " + expression)
			Expect(err).NotTo(HaveOccurred())
			rows := sqlRows{fields: sqlFields}
			predicate, err := rows.bindPredicate(context.Background(), nil, parsed.Where)
			Expect(err).NotTo(HaveOccurred())
			Expect(predicate(sqlRecord{values: map[string]any{"symbol.name": value}})).To(Equal(expected))
		},
		Entry("simple case fold", "symbol.name IMATCH 'Σave'", "ςAVE", sqlTrue),
		Entry("case sensitive", "symbol.name MATCH 'Σave'", "ςAVE", sqlFalse),
		Entry("no full case expansion", "symbol.name IMATCH 'STRASSE'", "Straße", sqlFalse),
		Entry("escaped wildcard", `symbol.name MATCH 'Save\*'`, "Save*", sqlTrue),
		Entry("anchored", "symbol.name MATCH 'Save'", "Saver", sqlFalse),
		Entry("single segment", "symbol.name MATCH '/orders/*'", "/orders/new/nested", sqlFalse),
		Entry("all segments", "symbol.name MATCH '/orders/**'", "/orders/new/nested", sqlTrue),
	)
	It("keeps NULL comparisons unknown under NOT and Boolean precedence", func() {
		parsed, err := sqlsyntax.Parse("SELECT * FROM func:* WHERE NOT symbol.name = 'Save' OR symbol.name IS NULL AND depth = 2")
		Expect(err).NotTo(HaveOccurred())
		rows := sqlRows{fields: sqlFields}
		predicate, err := rows.bindPredicate(context.Background(), nil, parsed.Where)
		Expect(err).NotTo(HaveOccurred())
		Expect(predicate(sqlRecord{values: map[string]any{"symbol.name": nil, "depth": 1}})).To(Equal(sqlUnknown))
		Expect(predicate(sqlRecord{values: map[string]any{"symbol.name": nil, "depth": 2}})).To(Equal(sqlTrue))
	})
	It("rejects excessive negation before recursive parsing", func() {
		_, err := sqlsyntax.Parse("SELECT * FROM func:* WHERE " + strings.Repeat("NOT ", 65) + "depth = 1")
		Expect(err).To(MatchError(ContainSubstring("negation exceeds")))
	})
})
