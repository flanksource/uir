package lint

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/gavel/models"
	"github.com/flanksource/uir/indexer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

// indexWarnings are a report's warnings of the index itself, uir/index and uir/coverage.
func indexWarnings(report *Result) []models.Violation {
	warnings := []models.Violation{}
	for _, violation := range report.Violations {
		if strings.HasPrefix(violation.Rule.Method, "uir/") {
			warnings = append(warnings, violation)
		}
	}
	return warnings
}

var _ = Describe("lint over a module with a file that cannot be indexed", func() {
	DescribeTable("reports the file's uir/index warning and the same list again from the reused analysis",
		func(ctx SpecContext, backend string) {
			database := lintDatabase(ctx, backend)
			config := lintCheckout()
			root, err := filepath.EvalSymlinks(config.Root)
			Expect(err).NotTo(HaveOccurred())
			duplicateTotal := "package linted\n\ntype Cart struct{}\n\nfunc (c *Cart) Total() int { return 1 }\n\nfunc (c *Cart) Total() int { return 2 }\n"
			Expect(os.WriteFile(filepath.Join(root, "checkout.go"), []byte(duplicateTotal), 0o600)).To(Succeed())

			first := Run(ctx, database, config, Options{})
			Expect(first.Success).To(BeTrue(), first.Error)
			Expect(first.Violations).To(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{"File": Equal(filepath.Join(root, "small.go")), "Rule": gstruct.PointTo(HaveField("Method", "small"))})))
			Expect(indexWarnings(first)).To(HaveExactElements(
				gstruct.MatchAllFields(gstruct.Fields{
					"File": Equal(filepath.Join(root, "checkout.go")), "Line": BeZero(), "Column": BeZero(),
					"Message": gstruct.PointTo(MatchRegexp(`^validate extracted document: .*"checkout.go": symbol 2: key is already declared by symbol 1 .*; indexed as an empty document$`)),
					"Source":  Equal("uir"), "Rule": gstruct.PointTo(Equal(models.Rule{Type: models.RuleTypeDeny, Package: "uir", Method: indexer.RuleIndex})),
					"Severity": Equal(models.SeverityWarning), "Fixable": BeFalse(), "FixApplicability": BeEmpty(), "Code": BeNil(), "CreatedAt": BeZero(),
				}),
			))

			again := Run(ctx, database, config, Options{NoIndex: true})
			Expect(again.Success).To(BeTrue(), again.Error)
			Expect(indexWarnings(again)).To(Equal(indexWarnings(first)))
		},
		Entry("SQLite", "sqlite"),
		Entry("PostgreSQL", "postgres"),
	)
})
