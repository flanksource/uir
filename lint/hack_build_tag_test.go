package lint

import (
	"os"
	"path/filepath"

	"github.com/flanksource/gavel/models"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var hackViolationPaths = []string{
	"hack/alternative.go", "hack/check_test.go", "hack/ignored.go", "hack/legacy.go",
	"hack/missing.go", "hack/negated.go", "hack/platform_windows.go", "tools/hack/nested.go",
}

func hackConfig() *Config {
	GinkgoHelper()
	checkout := GinkgoT().TempDir()
	Expect(os.CopyFS(checkout, os.DirFS("testdata/hack-build-tags"))).To(Succeed())
	config, err := Load(filepath.Join(checkout, ConfigName))
	Expect(err).NotTo(HaveOccurred())
	return config
}

var _ = Describe("Hack build-tag lint", func() {
	It("keeps the runnable fixture rule identical to the example rule", func() {
		example, err := Load("../.uir.example")
		Expect(err).NotTo(HaveOccurred())
		rules, err := example.Select([]string{"hack-build-tag"})
		Expect(err).NotTo(HaveOccurred())
		fixture := hackConfig().Lint.Rules[0]
		Expect([]any{rules[0].Query, rules[0].Message, rules[0].Severity}).To(Equal([]any{fixture.Query, fixture.Message, fixture.Severity}))
	})

	DescribeTable("reports exact witnesses including excluded files and preserves file filters", func(ctx SpecContext, backend string) {
		config := hackConfig()
		root := filepath.Dir(config.Path)
		database := lintDatabase(ctx, backend)
		result := Run(ctx, database, config, Options{Rules: []string{"hack-build-tag"}})
		Expect(result.Success).To(BeTrue(), result.Error)
		Expect(result.Violations).To(HaveLen(len(hackViolationPaths)))
		for i, path := range hackViolationPaths {
			Expect(result.Violations[i]).To(And(
				HaveField("File", filepath.Join(root, path)), HaveField("Line", 0),
				HaveField("Severity", models.SeverityWarning), HaveField("Rule.Method", "hack-build-tag"),
				HaveField("Message", HaveValue(Equal(path+" must require the hack build tag"))),
			))
		}
		filtered := Run(ctx, database, config, Options{NoIndex: true, Include: []string{"hack/**"}, Exclude: []string{"**/*_test.go", "hack/ignored.go"}})
		Expect(filtered.Success).To(BeTrue(), filtered.Error)
		Expect(filtered.Violations).To(HaveLen(5))
		config.Lint.IncludeTests = false
		withoutTests := Run(ctx, database, config, Options{})
		Expect(withoutTests.Success).To(BeTrue(), withoutTests.Error)
		Expect(withoutTests.Violations).To(HaveLen(7))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
})
