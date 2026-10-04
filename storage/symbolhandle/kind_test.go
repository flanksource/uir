package symbolhandle_test

import (
	"github.com/flanksource/uir/storage/symbolhandle"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("symbol kinds", func() {
	It("numbers the eighteen builtin kinds upward from 1, each with its category", func() {
		container, typed, callable, member := symbolhandle.CategoryContainer, symbolhandle.CategoryType, symbolhandle.CategoryCallable, symbolhandle.CategoryMember
		Expect(symbolhandle.BuiltinKinds()).To(Equal([]symbolhandle.KindSpec{
			{Code: 1, Name: "package", Category: container}, {Code: 2, Name: "type", Category: typed},
			{Code: 3, Name: "func", Category: callable}, {Code: 4, Name: "method", Category: callable},
			{Code: 5, Name: "field", Category: member}, {Code: 6, Name: "var", Category: member},
			{Code: 7, Name: "const", Category: member}, {Code: 8, Name: "builtin", Category: member},
			{Code: 9, Name: "module", Category: container}, {Code: 10, Name: "interface", Category: typed},
			{Code: 11, Name: "constructor", Category: callable}, {Code: 12, Name: "record", Category: typed},
			{Code: 13, Name: "endpoint", Category: callable}, {Code: 14, Name: "table", Category: typed},
			{Code: 15, Name: "column", Category: member}, {Code: 16, Name: "index", Category: member},
			{Code: 17, Name: "foreign_key", Category: member}, {Code: 18, Name: "annotation", Category: member},
		}))
		Expect(symbolhandle.MaxBuiltinKind).To(Equal(symbolhandle.Kind(18)))
		Expect(symbolhandle.KindMethod.String()).To(Equal("method"))
		Expect(symbolhandle.Kind(40).String()).To(Equal("Kind(40)"))
	})

	Describe("a registry", func() {
		var custom = []symbolhandle.KindSpec{
			{Code: 63, Name: "oipa.rule", Category: symbolhandle.CategoryCallable},
			{Code: 62, Name: "oipa.segment", Category: symbolhandle.CategoryMember},
		}

		It("resolves builtins and custom kinds by name and by code", func() {
			kinds, err := symbolhandle.NewKinds(custom)
			Expect(err).ToNot(HaveOccurred())
			for _, expected := range append(symbolhandle.BuiltinKinds(), custom...) {
				byName, err := kinds.Lookup(expected.Name)
				Expect(err).ToNot(HaveOccurred())
				Expect(byName).To(Equal(expected))
				byCode, err := kinds.Spec(expected.Code)
				Expect(err).ToNot(HaveOccurred())
				Expect(byCode).To(Equal(expected))
			}
			Expect(kinds.All()).To(HaveLen(20))
			Expect(kinds.All()[19]).To(Equal(custom[0]), "All orders by code")
		})

		It("lists the kinds of a category by code, builtins before custom kinds", func() {
			kinds, err := symbolhandle.NewKinds(custom)
			Expect(err).ToNot(HaveOccurred())
			Expect(kinds.InCategory(symbolhandle.CategoryCallable)).To(Equal([]string{"func", "method", "constructor", "endpoint", "oipa.rule"}))
			Expect(kinds.InCategory(symbolhandle.CategoryType)).To(Equal([]string{"type", "interface", "record", "table"}))
			Expect(kinds.InCategory(symbolhandle.CategoryContainer)).To(Equal([]string{"package", "module"}))
		})

		It("fails on an unknown name or code instead of guessing", func() {
			kinds := symbolhandle.Builtins()
			_, err := kinds.Lookup("oipa.rule")
			Expect(err).To(MatchError(ContainSubstring(`symbol kind "oipa.rule" is not registered`)))
			_, err = kinds.Spec(40)
			Expect(err).To(MatchError(ContainSubstring("symbol kind code 40 is not registered")))
			_, err = kinds.Spec(0)
			Expect(err).To(MatchError(ContainSubstring("symbol kind code 0 is not registered")))
		})

		DescribeTable("refuses an invalid custom kind",
			func(spec symbolhandle.KindSpec, message string) {
				_, err := symbolhandle.NewKinds([]symbolhandle.KindSpec{spec})
				Expect(err).To(MatchError(ContainSubstring(message)))
			},
			Entry("a builtin code", symbolhandle.KindSpec{Code: 18, Name: "oipa.rule", Category: symbolhandle.CategoryCallable}, "code 18 is not a custom code"),
			Entry("a code past the field", symbolhandle.KindSpec{Code: 64, Name: "oipa.rule", Category: symbolhandle.CategoryCallable}, "code 64 is not a custom code"),
			Entry("a name without a namespace", symbolhandle.KindSpec{Code: 63, Name: "rule", Category: symbolhandle.CategoryCallable}, `kind "rule" must be namespaced`),
			Entry("an empty namespace", symbolhandle.KindSpec{Code: 63, Name: ".rule", Category: symbolhandle.CategoryCallable}, `kind ".rule" must be namespaced`),
			Entry("an uppercase name", symbolhandle.KindSpec{Code: 63, Name: "oipa.Rule", Category: symbolhandle.CategoryCallable}, `kind "oipa.Rule" must be namespaced`),
			Entry("an unknown category", symbolhandle.KindSpec{Code: 63, Name: "oipa.rule", Category: "screen"}, `category "screen"`),
		)

		It("refuses two custom kinds sharing a code or a name", func() {
			_, err := symbolhandle.NewKinds([]symbolhandle.KindSpec{custom[0], {Code: 63, Name: "oipa.other", Category: symbolhandle.CategoryType}})
			Expect(err).To(MatchError(ContainSubstring("code 63")))
			_, err = symbolhandle.NewKinds([]symbolhandle.KindSpec{custom[0], {Code: 61, Name: "oipa.rule", Category: symbolhandle.CategoryCallable}})
			Expect(err).To(MatchError(ContainSubstring(`"oipa.rule"`)))
		})
	})

	It("names the namespace of a custom kind and none for a builtin", func() {
		Expect(symbolhandle.KindNamespace("oipa.rule")).To(Equal("oipa"))
		Expect(symbolhandle.KindNamespace("oipa.rule.math")).To(Equal("oipa"))
		Expect(symbolhandle.KindNamespace("func")).To(Equal(""))
	})

	It("parses the four categories by name", func() {
		for _, name := range []string{"container", "type", "callable", "member"} {
			category, err := symbolhandle.ParseCategory(name)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(category)).To(Equal(name))
		}
		_, err := symbolhandle.ParseCategory("screen")
		Expect(err).To(MatchError(ContainSubstring(`category "screen"`)))
	})
})
