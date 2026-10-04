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
		Entry("kind selector", "kind:oipa.rule"),
		Entry("kind selector with a name pattern", "kind:foreign_key:Order* -kind:oipa.rule:Test* | kind:oipa.rule"),
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
		Entry("empty kind", "kind:"),
		Entry("empty kind name pattern", "kind:oipa.rule:"),
		Entry("third kind colon", "kind:oipa.rule:Rule:extra"),
	)
	DescribeTable("parses a kind selector into its kind and name pattern",
		func(expression string, expected query.Selector) {
			parsed, err := query.Parse(expression)
			Expect(err).ToNot(HaveOccurred())
			Expect(parsed.Expr.Selector).To(Equal(&expected))
		},
		Entry("every symbol of a kind", "kind:oipa.rule", query.Selector{Kind: "kind", SymbolKind: "oipa.rule", Pattern: "*"}),
		Entry("named symbols of a kind", "kind:method:Store.Save", query.Selector{Kind: "kind", SymbolKind: "method", Pattern: "Store.Save"}),
	)
	DescribeTable("parses quoted values and Entity:Field references into their typed operands",
		func(expression string, expected query.Expr) {
			parsed, err := query.Parse(expression)
			Expect(err).ToNot(HaveOccurred())
			Expect(parsed.Expr).To(Equal(&expected))
		},
		Entry("quoted symbol with a space", `"Add Rider"`, query.Expr{Kind: query.ExprSymbol, Symbol: "Add Rider"}),
		Entry("quoted symbol keeps its escapes for the glob", `"Say \"Hi\" \\ now"`, query.Expr{Kind: query.ExprSymbol, Symbol: `Say \"Hi\" \\ now`}),
		Entry("quoted symbol keeps globs", `"Premium Calc*"`, query.Expr{Kind: query.ExprSymbol, Symbol: "Premium Calc*"}),
		Entry("quoted nested module", `mod:"Example Regional/Group Life/GL"`,
			query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "mod", Pattern: "Example Regional/Group Life/GL"}}),
		Entry("quoted module descendants", `mod:"Example Regional/Group Life/..."`,
			query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "mod", Pattern: "Example Regional/Group Life/**"}}),
		Entry("quoted function with parentheses", `func:"Premium Calc (Annual)"`,
			query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "func", Pattern: "Premium Calc (Annual)"}}),
		Entry("colon inside quotes is literal", `func:"Rate:Annual"`,
			query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "func", Pattern: "Rate:Annual"}}),
		Entry("quoted kind name pattern", `kind:oipa.rule:"CopyBook-CycleA"`,
			query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "kind", SymbolKind: "oipa.rule", Pattern: "CopyBook-CycleA"}}),
		Entry("quoted module and relative package", `pkg:"Example Regional":"Group Life/**"`,
			query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "pkg", ModulePattern: "Example Regional", Pattern: "Group Life/**"}}),
		Entry("entity field", "Plan:PlanField1",
			query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "field", Owner: "Plan", Pattern: "PlanField1"}}),
		Entry("entity field glob", "Plan:*",
			query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "field", Owner: "Plan", Pattern: "*"}}),
		Entry("quoted entity field", `Plan:"Field With Space"`,
			query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "field", Owner: "Plan", Pattern: "Field With Space"}}),
		Entry("quoted entity named like a selector", `"type":Code`,
			query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "field", Owner: "type", Pattern: "Code"}}),
		Entry("quoted field named like a relation", `Thing:"impl"`,
			query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "field", Owner: "Thing", Pattern: "impl"}}),
		Entry("entity field writes", "Plan:PlanField1 ~w",
			query.Expr{Kind: query.ExprRelation, Relation: "~w", Left: &query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "field", Owner: "Plan", Pattern: "PlanField1"}}}),
		Entry("entity field incoming from quoted modules", `Plan:PlanField1 < (mod:"Co/Prod" | mod:"Co/Prod/GL")`,
			query.Expr{Kind: query.ExprRelation, Relation: "<",
				Left: &query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "field", Owner: "Plan", Pattern: "PlanField1"}},
				Right: &query.Expr{Kind: query.ExprUnion,
					Left:  &query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "mod", Pattern: "Co/Prod"}},
					Right: &query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "mod", Pattern: "Co/Prod/GL"}}}}),
		Entry("entity field as a bare right operand", "func:Touch > Plan:PlanField1",
			query.Expr{Kind: query.ExprRelation, Relation: ">",
				Left:  &query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "func", Pattern: "Touch"}},
				Right: &query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "field", Owner: "Plan", Pattern: "PlanField1"}}}),
		Entry("entity field glob intersected with a module", "Plan:* & mod:Co/Prod/GL",
			query.Expr{Kind: query.ExprIntersection,
				Left:  &query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "field", Owner: "Plan", Pattern: "*"}},
				Right: &query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "mod", Pattern: "Co/Prod/GL"}}}),
	)
	DescribeTable("keeps selector prefixes and relations ahead of Entity:Field",
		func(expression string, expected query.Expr) {
			parsed, err := query.Parse(expression)
			Expect(err).ToNot(HaveOccurred())
			Expect(parsed.Expr).To(Equal(&expected))
		},
		Entry("module scoped package", "pkg:a/b:Sym",
			query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "pkg", ModulePattern: "a/b", Pattern: "Sym"}}),
		Entry("kind with a name", "kind:x:y",
			query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "kind", SymbolKind: "x", Pattern: "y"}}),
		Entry("type selector", "type:Plan",
			query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "type", Pattern: "Plan"}}),
		Entry("implementers without a space", "sub.Iface:impl",
			query.Expr{Kind: query.ExprRelation, Relation: ":impl", Left: &query.Expr{Kind: query.ExprSymbol, Symbol: "sub.Iface"}}),
		Entry("methods without a space", "Thing:methods",
			query.Expr{Kind: query.ExprRelation, Relation: ":methods", Left: &query.Expr{Kind: query.ExprSymbol, Symbol: "Thing"}}),
		Entry("inheritors without a space", "Thing:inherits",
			query.Expr{Kind: query.ExprRelation, Relation: ":inherits", Left: &query.Expr{Kind: query.ExprSymbol, Symbol: "Thing"}}),
		Entry("exclusion modifier starting like a file filter on a right operand", "func:Save < func:Run -func:Test*",
			query.Expr{Kind: query.ExprRelation, Relation: "<",
				Left: &query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "func", Pattern: "Save"}},
				Right: &query.Expr{Kind: query.ExprModifier,
					Left:      &query.Expr{Kind: query.ExprSelector, Selector: &query.Selector{Kind: "func", Pattern: "Run"}},
					Modifiers: []query.TypedModifier{{Include: false, Selector: query.Selector{Kind: "func", Pattern: "Test*"}}}}}),
	)
	DescribeTable("rejects malformed quoted values and Entity:Field references", func(expression string) {
		_, err := query.Parse(expression)
		Expect(err).To(HaveOccurred())
	},
		Entry("unterminated quote", `"Add Rider`),
		Entry("escaped closing quote", `"Add Rider\"`),
		Entry("empty quoted symbol", `""`),
		Entry("empty quoted selector value", `mod:""`),
		Entry("text after a quoted selector value", `func:"Save"x`),
		Entry("entity without a field", "Plan:"),
		Entry("field without an entity", ":PlanField1"),
		Entry("third entity field part", "Plan:Field:Extra"),
		Entry("glob kind name", `kind:"oipa.*"`),
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
