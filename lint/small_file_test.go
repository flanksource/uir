package lint

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Small single-declaration file rule", func() {
	It("recommends merging only files below 150 lines with one function or struct", func(ctx SpecContext) {
		root := GinkgoT().TempDir()
		for path, content := range map[string]string{
			"go.mod":       "module example.org/smallfiles\n\ngo 1.26\n",
			"function.go":  "package smallfiles\nfunc Single() {}\n",
			"struct.go":    "package smallfiles\ntype Record struct { Name string; Count int }\n",
			"below.go":     "package smallfiles\nfunc Below() {}\n" + strings.Repeat("// padding\n", 147),
			"at.go":        "package smallfiles\nfunc At() {}\n" + strings.Repeat("// padding\n", 148),
			"above.go":     "package smallfiles\nfunc Above() {}\n" + strings.Repeat("// padding\n", 149),
			"empty.go":     "package smallfiles\n",
			"multiple.go":  "package smallfiles\nfunc First() {}\nfunc Second() {}\n",
			"structs.go":   "package smallfiles\ntype One struct{}\ntype Two struct{}\n",
			"mixed.go":     "package smallfiles\ntype Mixed struct{}\nfunc NewMixed() Mixed { return Mixed{} }\n",
			"methods.go":   "package smallfiles\ntype Methods struct{}\nfunc (Methods) Run() {}\n",
			"interface.go": "package smallfiles\ntype Reader interface { Read() }\n",
			"alias.go":     "package smallfiles\ntype Alias = Record\n",
			"variable.go":  "package smallfiles\nvar Value int\n",
		} {
			Expect(os.WriteFile(filepath.Join(root, path), []byte(content), 0o600)).To(Succeed())
		}
		example, err := os.ReadFile("../.uir.example")
		Expect(err).NotTo(HaveOccurred())
		configPath := filepath.Join(root, ConfigName)
		Expect(os.WriteFile(configPath, example, 0o600)).To(Succeed())
		config, err := Load(configPath)
		Expect(err).NotTo(HaveOccurred())
		report := Run(ctx, lintDatabase(ctx, "sqlite"), config, Options{Rules: []string{"small-single-declaration-file"}})
		Expect(report.Success).To(BeTrue(), report.Error)
		Expect(report.Violations).To(ConsistOf(
			And(HaveField("File", filepath.Join(root, "function.go")), HaveField("Message", HaveValue(ContainSubstring("merging into a related file in the same package")))),
			HaveField("File", filepath.Join(root, "struct.go")),
			HaveField("File", filepath.Join(root, "below.go")),
		))
	})
})
