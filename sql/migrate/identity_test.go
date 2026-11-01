package migrate_test

import (
	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/uir/sql/migrate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Selection identities", func() {
	DescribeTable("round trips quoted database identities", func(selection migrate.Selection) {
		decoded, err := migrate.ParseSelection(selection.ID())
		Expect(err).NotTo(HaveOccurred())
		Expect(decoded).To(Equal(selection))
	},
		Entry("whole table", migrate.Selection{Kind: "table", Object: model.Name{Schema: "dbo", Name: "orders"}}),
		Entry("member with punctuation", migrate.Selection{Kind: "column", Object: model.Name{Schema: `tenant."a`, Name: "orders/items"}, Member: `label.with."quotes`}),
	)
	DescribeTable("rejects malformed and trailing selection text", func(id string) {
		_, err := migrate.ParseSelection(id)
		Expect(err).To(HaveOccurred())
	}, Entry("unquoted", `table:dbo.orders`), Entry("trailing", `table:"dbo"."orders" trailing`), Entry("missing member", `column:"dbo"."orders"`), Entry("empty schema", `table:""."orders"`))
})
