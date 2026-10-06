package query

import (
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage/symbolhandle"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("kind categories in queries", func() {
	var index *indexContext
	BeforeEach(func() {
		kinds, err := symbolhandle.NewKinds([]symbolhandle.KindSpec{
			{Code: 63, Name: "acme.rule", Category: symbolhandle.CategoryCallable},
			{Code: 62, Name: "acme.segment", Category: symbolhandle.CategoryMember},
			{Code: 61, Name: "acme.catalog", Category: symbolhandle.CategoryContainer},
		})
		Expect(err).ToNot(HaveOccurred())
		index = &indexContext{kinds: kinds}
	})

	DescribeTable("maps a typed selector to the kinds it selects",
		func(selector Selector, expected []string) {
			kinds, err := selectorKinds(selector, index.kinds)
			Expect(err).ToNot(HaveOccurred())
			Expect(kinds).To(Equal(expected))
		},
		Entry("func: selects every callable", Selector{Kind: "func"}, []string{"func", "method", "constructor", "endpoint", "acme.rule"}),
		Entry("type: selects every type", Selector{Kind: "type"}, []string{"type", "interface", "record", "table"}),
		Entry("struct: selects every type before the type-form check", Selector{Kind: "struct"}, []string{"type", "interface", "record", "table"}),
		Entry("method: stays methods", Selector{Kind: "method"}, []string{"method"}),
		Entry("field: stays fields", Selector{Kind: "field"}, []string{"field"}),
		Entry("var: stays variables", Selector{Kind: "var"}, []string{"var"}),
		Entry("kind: selects exactly its kind", Selector{Kind: "kind", SymbolKind: "acme.segment"}, []string{"acme.segment"}),
		Entry("a scope selector selects every kind", Selector{Kind: "pkg"}, []string(nil)),
	)

	It("fails on an unregistered kind or an unknown selector", func() {
		_, err := selectorKinds(Selector{Kind: "kind", SymbolKind: "acme.unknown"}, index.kinds)
		Expect(err).To(MatchError(ContainSubstring(`symbol kind "acme.unknown" is not registered`)))
		_, err = selectorKinds(Selector{Kind: "label"}, index.kinds)
		Expect(err).To(MatchError(ContainSubstring(`unknown selector kind "label"`)))
	})

	DescribeTable("accepts a relation start by category",
		func(relation, kind string, accepted bool) {
			Expect(index.relationAccepts(relation, kind)).To(Equal(accepted))
		},
		Entry("> from a custom callable", ">", "acme.rule", true),
		Entry("> from a func", ">", "func", true),
		Entry("> from a custom member", ">", "acme.segment", false),
		Entry("< from a custom member", "<", "acme.segment", true),
		Entry("< from a custom container", "<", "acme.catalog", false),
		Entry("< from a package", "<", "package", false),
		Entry(":methods from a Go type only", ":methods", "record", false),
	)

	DescribeTable("names a symbol's node by its category",
		func(symbol ModuleSymbol, expected uir.Identifier) {
			Expect(index.identifier(symbol)).To(Equal(expected))
		},
		Entry("a custom callable without an owner is a function", ModuleSymbol{ModuleKey: "m", PackagePath: "m/p", Kind: "acme.rule", Name: "Rate"},
			uir.Identifier{Module: "m", Package: "m/p", Method: "Rate", NodeType: uir.NodeTypeMethod}),
		Entry("a method is owned by its type", ModuleSymbol{ModuleKey: "m", PackagePath: "m/p", Kind: "method", Owner: "Store", Name: "Save"},
			uir.Identifier{Module: "m", Package: "m/p", Type: "Store", Method: "Save", NodeType: uir.NodeTypeMethod}),
		Entry("a custom member is a field of its owner", ModuleSymbol{ModuleKey: "m", PackagePath: "m/p", Kind: "acme.segment", Owner: "Rate", Name: "Band"},
			uir.Identifier{Module: "m", Package: "m/p", Type: "Rate", Field: "Band", NodeType: uir.NodeTypeField}),
		Entry("a record is a type", ModuleSymbol{ModuleKey: "m", PackagePath: "m/p", Kind: "record", Name: "Policy"},
			uir.Identifier{Module: "m", Package: "m/p", Type: "Policy", NodeType: uir.NodeTypeType}),
		Entry("a builtin is named by its package", ModuleSymbol{Kind: "builtin", Name: "len"},
			uir.Identifier{NodeType: uir.NodeTypePackage}),
		Entry("a module has no package", ModuleSymbol{ModuleKey: "m", PackagePath: "m", Kind: "module", Name: "m"},
			uir.Identifier{Module: "m", NodeType: uir.NodeTypeModule}),
	)
})
