package query

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("quoted selector values", func() {
	const (
		group    = "Example Regional/Group Life"
		groupGL  = "Example Regional/Group Life/GL"
		nestedGL = "Example Regional/Group Life/GL/Scheme A"
	)
	DescribeTable("matches a nested module key with spaces",
		func(selector, moduleKey string, matches bool) {
			parsed, err := parseSelector(selector)
			Expect(err).ToNot(HaveOccurred())
			glob, err := compileSelectorGlob(parsed.Pattern)
			Expect(err).ToNot(HaveOccurred())
			Expect(glob.matches(moduleKey)).To(Equal(matches))
		},
		Entry("an exact module matches itself", `mod:"`+groupGL+`"`, groupGL, true),
		Entry("an exact module excludes its descendants", `mod:"`+groupGL+`"`, nestedGL, false),
		Entry("an exact module excludes its parent", `mod:"`+groupGL+`"`, group, false),
		Entry("a /... suffix matches the module itself", `mod:"`+group+`/..."`, group, true),
		Entry("a /... suffix matches a child", `mod:"`+group+`/..."`, groupGL, true),
		Entry("a /... suffix matches a grandchild", `mod:"`+group+`/..."`, nestedGL, true),
		Entry("a /... suffix stops at a segment boundary", `mod:"`+group+`/..."`, group+" Extra", false),
		Entry("a star stays within one segment", `mod:"Example Regional/*"`, group, true),
		Entry("a star does not cross a segment", `mod:"Example Regional/*"`, groupGL, false),
		Entry("a hyphenated name is literal", `func:"CopyBook-CycleA"`, "CopyBook-CycleA", true),
		Entry("parentheses are literal", `func:"Premium Calc (Annual)"`, "Premium Calc (Annual)", true),
		Entry("an escaped star is literal", `func:"Rate\*"`, "Rate*", true),
		Entry("an escaped star matches nothing else", `func:"Rate\*"`, "Rates", false),
		Entry("an escaped quote is a quote", `func:"Say \"Hi\""`, `Say "Hi"`, true),
		Entry("an escaped backslash is a backslash", `func:"A\\B"`, `A\B`, true),
	)
	DescribeTable("rejects malformed quoting",
		func(selector string) {
			_, err := parseSelector(selector)
			Expect(err).To(HaveOccurred())
		},
		Entry("unterminated quote", `mod:"Example Regional`),
		Entry("text after a closing quote", `mod:"Example Regional"/GL`),
		Entry("a quote inside an unquoted value", `mod:Example"Regional"`),
		Entry("dangling escape", `func:Rate\`),
	)
	DescribeTable("selects symbols by a quoted bare pattern",
		func(pattern, qualified string, matches bool) {
			var glob selectorGlob
			if _, exact := globLiteral(pattern); !exact {
				var err error
				glob, err = compileSelectorGlob(pattern)
				Expect(err).ToNot(HaveOccurred())
			}
			Expect(symbolPatternMatches(pattern, qualified, glob)).To(Equal(matches))
		},
		Entry("a name with spaces", "Add Rider", groupGL+".Add Rider", true),
		Entry("an owner-qualified name with spaces", "Plan.Add Rider", groupGL+".Plan.Add Rider", true),
		Entry("a name is not a prefix", "Add Rider", groupGL+".Add Riders", false),
		Entry("an escaped quote", `Say \"Hi\"`, groupGL+`.Say "Hi"`, true),
		Entry("an escaped star is literal", `Rate\*`, groupGL+".Rate*", true),
		Entry("an escaped star does not glob", `Rate\*`, groupGL+".Rates", false),
		Entry("a glob keeps working inside quotes", "Premium Calc*", groupGL+".Premium Calc (Annual)", true),
		Entry("a full path with spaces", groupGL+".Add Rider", groupGL+".Add Rider", true),
	)
})
