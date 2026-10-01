package query

import (
	"go/ast"
	"strings"

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
	wrapper(inArgument(n))
	spread(
		a,
		b,
	)
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

	DescribeTable("reads the whole call and the conditions that must hold to reach it, outermost first",
		func(callee, call string, guards ...string) {
			ident, expression := calleeSpans(file, callee)
			for _, span := range []storage.ByteSpan{ident, expression} {
				site, err := file.siteAt(span)
				Expect(err).ToNot(HaveOccurred())
				Expect(site.Call).To(Equal(call), "call of %s at bytes %v", callee, span)
				Expect(append([]string{}, guardTexts(site.Guards)...)).To(Equal(append([]string{}, guards...)), "guards of %s at bytes %v", callee, span)
			}
		},
		Entry("in an if body", "inThen", "inThen()", "a"),
		Entry("in an else block", "inElse", "inElse()", "!a"),
		Entry("in an else if body", "inElseIf", "inElseIf()", "!a", "b"),
		Entry("in the else of an else if", "inFinalElse", "inFinalElse()", "!a", "!b"),
		Entry("in an if condition, which does not guard itself", "inCondition", "inCondition()"),
		Entry("in an if init, which its condition does not guard", "inInit", "inInit()"),
		Entry("in the body of an if with an init", "afterInit", "afterInit()", "x > 0"),
		Entry("in a tagged switch case", "inCase", "inCase()", `kind == "a"`),
		Entry("in a tagged switch case with several values", "inCaseList", "inCaseList()", `kind == "b" || kind == "c"`),
		Entry("in a tagged switch default", "inDefault", "inDefault()", `!(kind == "a" || kind == "b" || kind == "c")`),
		Entry("in a tagless switch case", "inTagless", "inTagless()", "n > 1"),
		Entry("in a tagless switch case with several conditions", "inTaglessList", "inTaglessList()", "a || b"),
		Entry("in a tagless switch default", "inTaglessDefault", "inTaglessDefault()", "!(n > 1 || a || b)"),
		Entry("in a switch tag", "inTag", "inTag()"),
		Entry("in a case's own value list", "inCaseValue", "inCaseValue()"),
		Entry("in the default of a switch with no other case", "inOnlyDefault", "inOnlyDefault()"),
		Entry("in a case that falls through", "beforeFallthrough", "beforeFallthrough()", "n == 1"),
		Entry("in a case another falls through into", "afterFallthrough", "afterFallthrough()", "n == 1 || n == 2"),
		Entry("in the default after a fallthrough", "inFallDefault", "inFallDefault()", "!(n == 1 || n == 2)"),
		Entry("in a type switch case", "inTypeCase", "inTypeCase()", "v.(type) == *int"),
		Entry("in a type switch case with several types", "inTypeList", "inTypeList()", "v.(type) == string || v.(type) == nil"),
		Entry("in a type switch default", "inTypeDefault", "inTypeDefault(t)", "!(v.(type) == *int || v.(type) == string || v.(type) == nil)"),
		Entry("in a select receive case", "inReceive", "inReceive(x)", "x := <-ch"),
		Entry("in a select case's own send", "inSendValue", "inSendValue()"),
		Entry("in a select send case", "inSend", "inSend()", "ch <- inSendValue()"),
		Entry("in a select default", "inSelectDefault", "inSelectDefault()", "default"),
		Entry("in a for init", "inForInit", "inForInit()"),
		Entry("in a for post statement, which runs only after an iteration", "inForPost", "inForPost(i)", "i < n"),
		Entry("in a for body", "inFor", "inFor()", "i < n"),
		Entry("in a for condition", "inForCondition", "inForCondition()"),
		Entry("in a for with no condition", "inBareFor", "inBareFor()"),
		Entry("in a range loop", "inRange", "inRange()"),
		Entry("under an if, a loop and another if", "nested", "nested()", "a", "n > 0", "b"),
		Entry("in a function literal, which keeps the guards around it", "inClosure", "inClosure()", "a", "b"),
		Entry("at the top of the function", "unguarded", "unguarded()"),
		Entry("under a compound condition", "inCompound", "inCompound()", "a || b && n > 0"),
		Entry("under a negated condition", "inNegated", "inNegated()", "!a"),
		Entry("in the else of a negated condition", "inDoubleNegated", "inDoubleNegated()", "!!a"),
		Entry("a method call in an if condition", "ok", "s.ok()"),
		Entry("a method call through a selector chain", "save", `s.inner.save("x", 1)`, "s.ok()"),
		Entry("a call whose argument is another call", "wrapper", "wrapper(inArgument(n))"),
		Entry("a call in another call's argument, which is its own call", "inArgument", "inArgument(n)"),
		Entry("a call written over several lines, kept as written", "spread", "spread(\n\t\ta,\n\t\tb,\n\t)"),
	)

	It("rejects a span that lies outside the source", func() {
		_, err := file.siteAt(storage.ByteSpan{len(guardSource), len(guardSource) + 4})
		Expect(err).To(MatchError(ContainSubstring("outside")))
	})

	It("rejects a span that is not the callee of a call", func() {
		condition := strings.Index(guardSource, "if a {\n\t\tinThen") + len("if ")
		_, err := file.siteAt(storage.ByteSpan{condition, condition + 1})
		Expect(err).To(MatchError(ContainSubstring("no call")))
	})

	It("does not parse a source with syntax errors", func() {
		_, err := parseGuardFile("broken.go", []byte("package sample\n\nfunc broken( {\n"))
		Expect(err).To(MatchError(ContainSubstring("broken.go")))
	})
})
