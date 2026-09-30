package golang_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/golang"
)

func TestGolangSpecs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Golang")
}

func lower(source string) uir.ExprStmt {
	GinkgoHelper()
	fset := token.NewFileSet()
	expr, err := parser.ParseExprFrom(fset, "expr.go", source, 0)
	Expect(err).ToNot(HaveOccurred(), source)
	return golang.LowerExpr(fset, []byte(source), expr)
}

var _ = Describe("LowerExpr", func() {
	DescribeTable("renders a structured expression as the source reads",
		func(source, text string) {
			lowered := lower(source)
			Expect(lowered.Value()).ToNot(BeNil(), "%s is modelled, not kept as text", source)
			Expect(lowered.Pretty().String()).To(Equal(text))
			Expect(lowered.Content).To(HaveValue(Equal(source)))
		},
		Entry("an identifier", "ready", "ready"),
		Entry("nil, which stays nil", "nil", "nil"),
		Entry("true", "true", "true"),
		Entry("false", "false", "false"),
		Entry("a selector chain", "s.config.Enabled", "s.config.Enabled"),
		Entry("an interpreted string", `"ACTIVE"`, `"ACTIVE"`),
		Entry("a raw string", "`raw`", `"raw"`),
		Entry("a character", `'x'`, `"x"`),
		Entry("an integer", "42", "42"),
		Entry("a float", "1.5", "1.5"),
		Entry("a comparison", "limit > 0", "limit > 0"),
		Entry("a comparison with nil", "err != nil", "err != nil"),
		Entry("a string comparison", `status == "ACTIVE"`, `status == "ACTIVE"`),
		Entry("a conjunction of comparisons", "a == b && c <= d", "a == b && c <= d"),
		Entry("arithmetic", "a+b*c-d/e%f", "a + b * c - d / e % f"),
		Entry("bitwise and, or and a left shift", "a&b | c<<d", "(a & b) | (c << d)"),
		Entry("bitwise xor and a right shift", "a ^ b>>c", "a ^ (b >> c)"),
		Entry("a negation", "!ready", "!ready"),
		Entry("a negated comparison", "!(a == b)", "!(a == b)"),
		Entry("a negative number", "-1", "-1"),
		Entry("a unary plus", "+x", "+x"),
		Entry("parentheses that precedence needs", "a && (b || c)", "a && (b || c)"),
		Entry("parentheses that precedence does not need", "(a && b) || (c)", "a && b || c"),
		Entry("a function call", `validate(order, "strict", 3)`, `validate(order, "strict", 3)`),
		Entry("a call with no arguments", "ready()", "ready()"),
		Entry("a method call on a variable", `s.Save("a")`, `s.Save("a")`),
		Entry("a call through a selector chain", "s.store.Count(ctx)", "s.store.Count(ctx)"),
		Entry("a call on the result of a call", "open(path).Close()", "open(path).Close()"),
		Entry("a call inside a comparison", "len(items) == 0", "len(items) == 0"),
		Entry("a call as an argument", "max(len(a), len(b))", "max(len(a), len(b))"),
	)

	DescribeTable("keeps the exact source text of a kind it does not model",
		func(source string) {
			lowered := lower(source)
			Expect(lowered.Value()).To(BeNil(), "%s has no variant", source)
			Expect(lowered.Content).To(HaveValue(Equal(source)))
			Expect(lowered.Pretty().String()).To(Equal(source))
		},
		Entry("an index", "items[0]"),
		Entry("a slice", "items[1:n]"),
		Entry("a type assertion", "value.(*Store)"),
		Entry("a composite literal", `Options{Depth: 2}`),
		Entry("a function literal", "func() bool { return true }"),
		Entry("a dereference", "*pointer"),
		Entry("an address", "&value"),
		Entry("a channel receive", "<-done"),
		Entry("a bitwise complement", "^mask"),
		Entry("a generic instantiation call", "lookup[string](key)"),
		Entry("a variadic call", "append(items, more...)"),
		Entry("a call through a parenthesised function", "(handler)(request)"),
		Entry("a selector on an index", "rows[0].Name"),
		Entry("an operator with no UIR counterpart", "a &^ b"),
		Entry("an imaginary number", "2i"),
	)

	It("keeps an unmodelled operand as text inside a structured expression", func() {
		lowered := lower(`items[0] == "a" && !seen[key]`)
		Expect(lowered.Binary).ToNot(BeNil())
		Expect(lowered.Pretty().String()).To(Equal(`items[0] == "a" && !seen[key]`))
	})

	DescribeTable("parenthesises a binary operand it kept as text, so that it still reads as one operand",
		func(source, text string) {
			Expect(lower(source).Pretty().String()).To(Equal(text))
		},
		Entry("under a comparison", "a&^b != 0", "(a&^b) != 0"),
		Entry("already parenthesised in the source", "(a &^ b) == c", "(a &^ b) == c"),
		Entry("under a negation", "!(a &^ b)", "!(a &^ b)"),
		Entry("as a call argument, where it needs none", "f(a &^ b)", "f(a &^ b)"),
	)

	It("types each literal and names each variable", func() {
		Expect(lower(`"x"`).Literal.FieldType).To(Equal(uir.RecordFieldTypeString))
		Expect(lower("7").Literal.FieldType).To(Equal(uir.RecordFieldTypeInt))
		Expect(lower("7.5").Literal.FieldType).To(Equal(uir.RecordFieldTypeFloat))
		Expect(lower("true").Literal.FieldType).To(Equal(uir.RecordFieldTypeBoolean))
		Expect(lower("a.b").Variable.Name).To(Equal("a.b"))
		Expect(lower("nil").Variable.Name).To(Equal("nil"))
	})

	It("names a call by its method, with the receiver as written and its arguments in order", func() {
		call := lower(`s.store.Save(name, 1)`).MethodCall
		Expect(call).ToNot(BeNil())
		Expect(call.Method.GetIdentifier()).To(Equal(uir.Identifier{Type: "s.store", Method: "Save", NodeType: uir.NodeTypeMethod}))
		Expect(call.Receiver.Pretty().String()).To(Equal("s.store"))
		Expect(call.Arguments).To(HaveLen(2))
		Expect(call.Arguments[0].Name).To(BeNil())
		Expect(call.Arguments[1].Value.Pretty().String()).To(Equal("1"))
		Expect(lower("run()").MethodCall.Method.GetIdentifier()).To(Equal(uir.Identifier{Method: "run", NodeType: uir.NodeTypeMethod}))
	})

	It("panics on a nil expression, which has no source text to keep", func() {
		Expect(func() { golang.LowerExpr(token.NewFileSet(), nil, nil) }).To(PanicWith(ContainSubstring("nil expression")))
	})
})

var _ = Describe("SourceExpr", func() {
	It("keeps a node's exact source text and no variant", func() {
		const source = "package p\n\nfunc f(ch chan int) {\n\tselect {\n\tcase v := <-ch:\n\t\t_ = v\n\t}\n}\n"
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "p.go", source, 0)
		Expect(err).ToNot(HaveOccurred())
		var comm ast.Stmt
		ast.Inspect(file, func(node ast.Node) bool {
			if clause, ok := node.(*ast.CommClause); ok {
				comm = clause.Comm
			}
			return true
		})
		expr := golang.SourceExpr(fset, []byte(source), comm)
		Expect(expr.Value()).To(BeNil())
		Expect(expr.Pretty().String()).To(Equal("v := <-ch"))
	})
})
