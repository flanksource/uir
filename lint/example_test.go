package lint

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Lint example command architecture rules", func() {
	It("flags thick command files and aliased Cobra references outside cmd path segments", func(ctx SpecContext) {
		workspace := GinkgoT().TempDir()
		root := filepath.Join(workspace, "app")
		for path, content := range map[string]string{
			"go.work":                   "go 1.26\nuse (\n./app\n./cobra\n)\n",
			"cobra/go.mod":              "module github.com/spf13/cobra\n\ngo 1.26\n",
			"cobra/cobra.go":            "package cobra\ntype Command struct{}\nfunc (*Command) Execute() {}\n",
			"app/go.mod":                "module example.org/architecture\n\ngo 1.26\nrequire github.com/spf13/cobra v0.0.0\n",
			"app/cmd/thick.go":          "package cmd\n" + strings.Repeat("// command wiring\n", 300),
			"app/cmd/thin.go":           "package cmd\n" + strings.Repeat("// command wiring\n", 299),
			"app/nested/cmd/allowed.go": "package cmd\nimport cli \"github.com/spf13/cobra\"\nvar Allowed *cli.Command\n",
			"app/service/service.go":    "package service\nimport cli \"github.com/spf13/cobra\"\nfunc Run(command *cli.Command) { command.Execute() }\n",
			"app/cmdish/forbidden.go":   "package cmdish\nimport cli \"github.com/spf13/cobra\"\nvar Forbidden *cli.Command\n",
			"app/service/import.go":     "package service\nimport _ \"github.com/spf13/cobra\"\n",
			"app/service/local.go":      "package service\ntype Command struct{}\nvar Local *Command\n",
		} {
			file := filepath.Join(workspace, path)
			Expect(os.MkdirAll(filepath.Dir(file), 0o700)).To(Succeed())
			Expect(os.WriteFile(file, []byte(content), 0o600)).To(Succeed())
		}
		example, err := os.ReadFile("../.uir.example")
		Expect(err).NotTo(HaveOccurred())
		configPath := filepath.Join(root, ConfigName)
		Expect(os.WriteFile(configPath, example, 0o600)).To(Succeed())
		config, err := Load(configPath)
		Expect(err).NotTo(HaveOccurred())
		database := lintDatabase(ctx, "sqlite")
		report := Run(ctx, database, config, Options{Rules: []string{"thick-command-files", "cobra-outside-cmd"}})
		Expect(report.Success).To(BeTrue(), report.Error)
		Expect(report.Violations).To(ContainElement(And(HaveField("Rule.Method", "thick-command-files"), HaveField("File", filepath.Join(root, "cmd/thick.go")))))
		Expect(report.Violations).To(ContainElement(And(HaveField("Rule.Method", "cobra-outside-cmd"), HaveField("File", filepath.Join(root, "service/service.go")))))
		Expect(report.Violations).To(ContainElement(And(HaveField("Rule.Method", "cobra-outside-cmd"), HaveField("File", filepath.Join(root, "cmdish/forbidden.go")))))
		Expect(report.Violations).To(ContainElement(And(HaveField("Rule.Method", "cobra-outside-cmd"), HaveField("File", filepath.Join(root, "service/import.go")), HaveField("Line", 2))))
		for _, finding := range report.Violations {
			Expect(finding.File).To(BeElementOf(filepath.Join(root, "cmd/thick.go"), filepath.Join(root, "service/service.go"), filepath.Join(root, "cmdish/forbidden.go"), filepath.Join(root, "service/import.go")))
		}
	})
})
