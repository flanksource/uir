package query

import (
	"go/ast"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir/storage"
)

const guardSource = `package sample

func conditions(s *service, a, b bool, n int, kind string, v any, ch chan int, items []int) {
	if a {
		inThen()
	} else {
		inElse()
	}
	if a {
	} else if b {
		inElseIf()
	} else {
		inFinalElse()
	}
	if inCondition() {
	}
	if x := inInit(); x > 0 {
		afterInit()
	}
	switch kind {
	case "a":
		inCase()
	case "b", "c":
		inCaseList()
	default:
		inDefault()
	}
	switch {
	case n > 1:
		inTagless()
	case a, b:
		inTaglessList()
	default:
		inTaglessDefault()
	}
	switch inTag() {
	case inCaseValue():
	}
	switch {
	default:
		inOnlyDefault()
	}
	switch n {
	case 1:
		beforeFallthrough()
		fallthrough
	case 2:
		afterFallthrough()
	default:
		inFallDefault()
	}
	switch t := v.(type) {
	case *int:
		inTypeCase()
	case string, nil:
		inTypeList()
	default:
		inTypeDefault(t)
	}
	select {
	case x := <-ch:
		inReceive(x)
	case ch <- inSendValue():
		inSend()
	default:
		inSelectDefault()
	}
	for i := inForInit(); i < n; i = inForPost(i) {
		inFor()
	}
	for inForCondition() {
	}
	for {
		inBareFor()
	}
	for range items {
		inRange()
	}
	if a {
		for n > 0 {
			if b {
				nested()
			}
		}
	}
	if a {
		go func() {
			if b {
				inClosure()
			}
		}()
	}
	unguarded()
	if a || b && n > 0 {
		inCompound()
	}
	if !a {
		inNegated()
	} else {
		inDoubleNegated()
	}
	if s.ok() {
		s.inner.save("x", 1)
	}
}
`

// calleeSpans finds the call of the named function and returns the byte span of
// its callee identifier, which a typed occurrence records, and of its whole
// callee expression, which a syntax occurrence records.
func calleeSpans(file *guardFile, name string) (ident, callee storage.ByteSpan) {
	GinkgoHelper()
	found := false
	ast.Inspect(file.syntax, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		target, ok := call.Fun.(*ast.Ident)
		if selector, selected := call.Fun.(*ast.SelectorExpr); selected {
			target, ok = selector.Sel, true
		}
		if !ok || target.Name != name {
			return true
		}
		Expect(found).To(BeFalse(), "%s is called once in the fixture", name)
		found = true
		ident = storage.ByteSpan{file.tokens.Offset(target.Pos()), file.tokens.Offset(target.End())}
		callee = storage.ByteSpan{file.tokens.Offset(call.Fun.Pos()), file.tokens.Offset(call.Fun.End())}
		return true
	})
	Expect(found).To(BeTrue(), "%s is called in the fixture", name)
	return ident, callee
}

var _ = Describe("call site guards", func() {
	var file *guardFile

	BeforeEach(func() {
		var err error
		file, err = parseGuardFile("sample.go", []byte(guardSource))
		Expect(err).ToNot(HaveOccurred())
	})

	DescribeTable("lists the conditions that must hold to reach a call, outermost first",
		func(callee string, guards ...string) {
			ident, expression := calleeSpans(file, callee)
			for _, span := range []storage.ByteSpan{ident, expression} {
				conditions, err := file.guardsAt(span)
				Expect(err).ToNot(HaveOccurred())
				Expect(append([]string{}, guardTexts(conditions)...)).To(Equal(append([]string{}, guards...)), "guards of %s at bytes %v", callee, span)
			}
		},
		Entry("in an if body", "inThen", "a"),
		Entry("in an else block", "inElse", "!a"),
		Entry("in an else if body", "inElseIf", "!a", "b"),
		Entry("in the else of an else if", "inFinalElse", "!a", "!b"),
		Entry("in an if condition, which does not guard itself", "inCondition"),
		Entry("in an if init, which its condition does not guard", "inInit"),
		Entry("in the body of an if with an init", "afterInit", "x > 0"),
		Entry("in a tagged switch case", "inCase", `kind == "a"`),
		Entry("in a tagged switch case with several values", "inCaseList", `kind == "b" || kind == "c"`),
		Entry("in a tagged switch default", "inDefault", `!(kind == "a" || kind == "b" || kind == "c")`),
		Entry("in a tagless switch case", "inTagless", "n > 1"),
		Entry("in a tagless switch case with several conditions", "inTaglessList", "a || b"),
		Entry("in a tagless switch default", "inTaglessDefault", "!(n > 1 || a || b)"),
		Entry("in a switch tag", "inTag"),
		Entry("in a case's own value list", "inCaseValue"),
		Entry("in the default of a switch with no other case", "inOnlyDefault"),
		Entry("in a case that falls through", "beforeFallthrough", "n == 1"),
		Entry("in a case another falls through into", "afterFallthrough", "n == 1 || n == 2"),
		Entry("in the default after a fallthrough", "inFallDefault", "!(n == 1 || n == 2)"),
		Entry("in a type switch case", "inTypeCase", "v.(type) == *int"),
		Entry("in a type switch case with several types", "inTypeList", "v.(type) == string || v.(type) == nil"),
		Entry("in a type switch default", "inTypeDefault", "!(v.(type) == *int || v.(type) == string || v.(type) == nil)"),
		Entry("in a select receive case", "inReceive", "x := <-ch"),
		Entry("in a select case's own send", "inSendValue"),
		Entry("in a select send case", "inSend", "ch <- inSendValue()"),
		Entry("in a select default", "inSelectDefault", "default"),
		Entry("in a for init", "inForInit"),
		Entry("in a for post statement, which runs only after an iteration", "inForPost", "i < n"),
		Entry("in a for body", "inFor", "i < n"),
		Entry("in a for condition", "inForCondition"),
		Entry("in a for with no condition", "inBareFor"),
		Entry("in a range loop", "inRange"),
		Entry("under an if, a loop and another if", "nested", "a", "n > 0", "b"),
		Entry("in a function literal, which keeps the guards around it", "inClosure", "a", "b"),
		Entry("at the top of the function", "unguarded"),
		Entry("under a compound condition", "inCompound", "a || b && n > 0"),
		Entry("under a negated condition", "inNegated", "!a"),
		Entry("in the else of a negated condition", "inDoubleNegated", "!!a"),
		Entry("a method call in an if condition", "ok"),
		Entry("a method call through a selector chain", "save", "s.ok()"),
	)

	It("rejects a span that lies outside the source", func() {
		_, err := file.guardsAt(storage.ByteSpan{len(guardSource), len(guardSource) + 4})
		Expect(err).To(MatchError(ContainSubstring("outside")))
	})

	It("does not parse a source with syntax errors", func() {
		_, err := parseGuardFile("broken.go", []byte("package sample\n\nfunc broken( {\n"))
		Expect(err).To(MatchError(ContainSubstring("broken.go")))
	})
})
