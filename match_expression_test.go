package uir_test

import (
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
