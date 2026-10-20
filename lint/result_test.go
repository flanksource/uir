package lint

import (
	"encoding/json"

	"github.com/flanksource/gavel/linters"
	"github.com/flanksource/gavel/models"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Lint result presentation", func() {
	It("keeps symbol identities and files distinct and retains diagnostics", func() {
		result := &Result{LinterResult: &linters.LinterResult{}, GroupBy: "symbol"}
		first := &query.ModuleSymbol{ID: "symbol-one", QueryName: "func:example.org/pkg.Run"}
		second := &query.ModuleSymbol{ID: "symbol-two", QueryName: first.QueryName}
		finding := models.Violation{File: "b.go", Severity: models.SeverityWarning, Message: models.StringPtr("review")}
		result.addFinding(finding, first)
		result.addFinding(finding, second)
		finding.File = "a.go"
		result.addFinding(finding, first)
		result.addFinding(finding, nil)
		finding.File = ""
		result.addFinding(finding, nil)
		result.sortGroups()
		Expect(result.Violations).To(HaveLen(5))
		Expect(result.Groups).To(HaveLen(5))
		Expect(result.Groups[0].File).To(BeEmpty())
		Expect(result.Groups[0].Pretty().String()).To(Equal("Run diagnostics"))
		Expect(result.Groups[0].GetChildren()).To(HaveLen(1))
		Expect(result.Groups[1].Pretty().String()).To(Equal("File findings"))
		Expect(result.Groups[2].SymbolID).To(Equal(first.ID))
		Expect(result.Groups[3].SymbolID).To(Equal(first.ID))
		Expect(result.Groups[4].SymbolID).To(Equal(second.ID))
		Expect(result.GetChildren()).To(HaveLen(5))
		Expect(result.GetChildren()[2].Pretty().String()).To(ContainSubstring(first.QueryName))
	})
	It("preserves the default report schema and existing file renderer", func() {
		result := &Result{LinterResult: &linters.LinterResult{Linter: "uir", Success: true}}
		result.addFinding(models.Violation{File: "sample.go", Message: models.StringPtr("review")}, nil)
		encoded, err := json.Marshal(result)
		Expect(err).NotTo(HaveOccurred())
		canonical, err := json.Marshal(result.LinterResult)
		Expect(err).NotTo(HaveOccurred())
		Expect(encoded).To(Equal(canonical))
		Expect(result.GetChildren()[0].Pretty()).To(Equal(result.LinterResult.GetChildren()[0].Pretty()))
	})
})
