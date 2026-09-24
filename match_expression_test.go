package uir_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
)

var _ = Describe("MatchExpression.Matches", func() {
	type result struct{ matches, negated bool }

	DescribeTable("comma-separated patterns with * globs and ! exclusions",
		func(expression, value string, want result) {
			matches, negated := uir.MatchExpression(expression).Matches(value)
			Expect(result{matches, negated}).To(Equal(want))
		},
		Entry("exact match ignores case", "GetUser", "getuser", result{true, false}),
		Entry("no match", "GetUser", "SetUser", result{false, false}),
		Entry("star matches everything", "*", "anything", result{true, false}),
		Entry("prefix glob", "Get*", "GetUser", result{true, false}),
		Entry("suffix glob", "*User", "GetUser", result{true, false}),
		Entry("contains glob", "*tUs*", "GetUser", result{true, false}),
		Entry("one of several patterns", "Set*,Get*", "GetUser", result{true, false}),
		Entry("exclusion wins over inclusion", "Get*,!GetUser", "GetUser", result{false, true}),
		Entry("exclusion-only admits the rest", "!Set*", "GetUser", result{true, false}),
		Entry("exclusion-only rejects a match", "!Get*", "GetUser", result{false, true}),
		Entry("!* excludes everything", "!*,GetUser", "GetUser", result{false, true}),
		Entry("blank parts between commas are skipped", "Set*, ,Get*", "GetUser", result{true, false}),
		Entry("patterns are URL-decoded", "Get%2AUser", "Get*User", result{true, false}),
		Entry("empty expression matches only empty", "", "GetUser", result{false, false}),
	)
})

// badEscape is a pattern url.QueryUnescape rejects: "%zz" is not a hex escape.
const badEscape = "Get%zzUser"

var _ = Describe("MatchExpression validation", func() {
	It("ParseMatchExpression rejects a pattern that fails to URL-decode, naming it", func() {
		_, err := uir.ParseMatchExpression("Set*," + badEscape)
		Expect(err).To(MatchError(ContainSubstring(badEscape)))
	})

	It("ParseMatchExpression accepts a decodable expression unchanged", func() {
		expression, err := uir.ParseMatchExpression("Set*,Get%2AUser")
		Expect(err).NotTo(HaveOccurred())
		Expect(expression).To(Equal(uir.MatchExpression("Set*,Get%2AUser")))
	})

	It("decoding JSON rejects an undecodable pattern", func() {
		var expression uir.MatchExpression
		err := json.Unmarshal([]byte(`"`+badEscape+`"`), &expression)
		Expect(err).To(MatchError(ContainSubstring(badEscape)))
	})

	It("Matches panics on an expression that bypassed validation", func() {
		Expect(func() { uir.MatchExpression(badEscape).Matches("GetUser") }).To(PanicWith(ContainSubstring(badEscape)))
	})

	It("Find fails instead of silently dropping an undecodable name pattern", func() {
		_, _, err := uir.Find[uir.MethodNode](servicePackage()).WithName(badEscape).Many()
		Expect(err).To(MatchError(ContainSubstring(badEscape)))
	})
})
