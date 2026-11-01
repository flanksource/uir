package lint

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Lint request-state rule", func() {
	It("reports exact global types and inferred aliases at each variable name", func(ctx SpecContext) {
		root := GinkgoT().TempDir()
		Expect(os.CopyFS(root, os.DirFS("testdata/request-state"))).To(Succeed())
		config, err := Load(filepath.Join(root, ConfigName))
		Expect(err).NotTo(HaveOccurred())
		report := Run(ctx, lintDatabase(ctx, "sqlite"), config, Options{})
		Expect(report.Success).To(BeTrue(), report.Error)
		expected := []struct {
			name         string
			line, column int
		}{
			{"globalContext", 12, 5}, {"ConfigValue", 13, 5}, {"ConfigPointer", 14, 5},
			{"RuntimeValue", 15, 5}, {"RuntimePointer", 16, 5}, {"OipaValue", 17, 5},
			{"OipaPointer", 18, 5}, {"DBValue", 19, 5}, {"DBPointer", 20, 5},
			{"Aliased", 24, 5}, {"Inferred", 25, 5}, {"GroupA", 26, 5}, {"GroupB", 26, 13},
		}
		Expect(report.Violations).To(HaveLen(len(expected)))
		for i, item := range expected {
			Expect(report.Violations[i]).To(And(
				HaveField("File", filepath.Join(root, "state.go")),
				HaveField("Line", item.line), HaveField("Column", item.column),
				HaveField("Message", HaveValue(Equal("package-level request state "+item.name+" is shared across users; pass it explicitly or through context"))),
			))
		}
	})
})
