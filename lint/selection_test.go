package lint

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Lint rule and path filtering", func() {
	DescribeTable("combines exact rule exclusions with file globs", func(options Options, ids []string, matches bool) {
		config := &Config{Lint: Settings{Rules: []Rule{{ID: "function-parameters"}, {ID: "small"}}}}
		selection, err := config.Filter(options)
		Expect(err).NotTo(HaveOccurred())
		Expect(selection.Rules).To(HaveLen(len(ids)))
		for i, id := range ids {
			Expect(selection.Rules[i].ID).To(Equal(id))
		}
		Expect(selection.MatchesPath("sample.go")).To(Equal(matches))
		Expect(config.Lint.Rules).To(Equal([]Rule{{ID: "function-parameters"}, {ID: "small"}}))
	},
		Entry("defaults", Options{}, []string{"function-parameters", "small"}, true),
		Entry("rule ID", Options{Exclude: []string{"function-parameters"}}, []string{"small"}, true),
		Entry("exclusion wins over selection", Options{Rules: []string{"function-parameters"}, Exclude: []string{"function-parameters"}}, []string{}, true),
		Entry("mixed rules and files", Options{Exclude: []string{"function-parameters", "*.go"}}, []string{"small"}, false),
		Entry("unselected rule ID is not a path", Options{Rules: []string{"small"}, Exclude: []string{"function-parameters"}}, []string{"small"}, true),
		Entry("rule globs are file globs", Options{Exclude: []string{"function-*"}}, []string{"function-parameters", "small"}, true),
		Entry("file inclusion remains active", Options{Include: []string{"cmd/**"}, Exclude: []string{"function-parameters"}}, []string{"small"}, false),
	)
	It("validates file globs even when every rule is excluded", func() {
		config := &Config{Lint: Settings{Rules: []Rule{{ID: "small"}}}}
		_, err := config.Filter(Options{Exclude: []string{"small", "cmd/**.go"}})
		Expect(err).To(MatchError(ContainSubstring("file filter")))
	})
	It("rejects unknown selected rules even when they are also excluded", func() {
		config := &Config{Lint: Settings{Rules: []Rule{{ID: "small"}}}}
		_, err := config.Filter(Options{Rules: []string{"unknown"}, Exclude: []string{"unknown"}})
		Expect(err).To(MatchError(ContainSubstring("unknown lint rule")))
	})
	It("skips indexing and queries when all rules are excluded", func(ctx SpecContext) {
		config, err := Load(configFile(validConfig))
		Expect(err).NotTo(HaveOccurred())
		report := Run(ctx, nil, config, Options{Exclude: []string{"small"}})
		Expect(report.Success).To(BeTrue(), report.Error)
		Expect(report.RuleCount).To(BeZero())
		Expect(report.Violations).To(BeEmpty())
	})
})
