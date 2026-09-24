package query_test

import (
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("compact query grammar", func() {
	DescribeTable("accepts compact expressions", func(expression string) {
		_, err := query.Parse(expression)
		Expect(err).ToNot(HaveOccurred())
	},
		Entry("symbol", "sub.Thing.Do"),
		Entry("incoming", "sub.Thing.Do <"),
		Entry("outgoing", "sub.Thing.Do >"),
		Entry("definition", "sub.Thing.Do ="),
		Entry("bounded callers", "sub.Thing.Do <<3"),
		Entry("write references", "sub.Thing.Field ~w"),
		Entry("implementers", "sub.Iface :impl"),
		Entry("members", "sub.Thing :methods"),
		Entry("file filter", "sub.Thing.Do < -f _test.go"),
		Entry("package filter", "sub.Thing.Do < +pkg api"),
		Entry("exclude package filter", "sub.Thing.Do < -pkg sub"),
		Entry("intersection", "os.Exec.Command < & io.ReadAll >"),
		Entry("path", "main.* >> sub.Thing.Do"),
		Entry("package selector", "pkg:example.org/shop/store"),
		Entry("module scoped package selector", "pkg:example.org/shop:store"),
		Entry("module and relative package glob", "pkg:example.org/*:internal/**"),
		Entry("literal punctuation", "pkg:example.org/!team$#@/store"),
		Entry("other typed selectors", "mod:example.org/shop & struct:Store | field:Store.count"),
		Entry("function modifier", "func:Run +pkg:example.org/shop:app -func:Test*"),
		Entry("binary incoming", "func:Save < pkg:example.org/shop:app"),
		Entry("binary outgoing", "func:Run > func:Store.Save"),
		Entry("modified unary relation", "(func:Save <) +pkg:example.org/shop:app"),
	)
	DescribeTable("rejects malformed typed selectors", func(expression string) {
		_, err := query.Parse(expression)
		Expect(err).To(HaveOccurred())
	},
		Entry("empty package", "pkg:"),
		Entry("empty module side", "pkg::store"),
		Entry("empty relative package", "pkg:example.org/shop:"),
		Entry("third package colon", "pkg:example.org/shop:store:extra"),
		Entry("leading exclusion", "-pkg:example.org/shop:store"),
		Entry("malformed glob", "pkg:example.org/***/store"),
		Entry("root token inside relative path", "pkg:example.org/shop:app/."),
		Entry("definition with right operand", "func:Save = func:Run"),
	)
	It("keeps the module and relative package patterns separate on a binary right operand", func() {
		parsed, err := query.Parse("func:Save < pkg:example.org/*:internal/** & func:Run")
		Expect(err).ToNot(HaveOccurred())
		Expect(parsed.Expr.Kind).To(Equal(query.ExprIntersection))
		Expect(parsed.Expr.Left.Kind).To(Equal(query.ExprRelation))
		Expect(parsed.Expr.Left.Right.Selector).To(Equal(&query.Selector{
			Kind: "pkg", ModulePattern: "example.org/*", Pattern: "internal/**",
		}))
	})
})
