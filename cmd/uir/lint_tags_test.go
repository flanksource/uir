package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/flanksource/gavel/linters"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("UIR lint build tags", func() {
	It("passes repeated tags through CLI, lint and syntax analysis", func(ctx SpecContext) {
		root := GinkgoT().TempDir()
		for name, content := range map[string]string{
			"go.mod":     "module example.org/tagged\n\ngo 1.26\n",
			"plain.go":   "package tagged\nfunc Plain() {}\n",
			"feature.go": "//go:build feature && integration\n\npackage tagged\nfunc Feature(a, b, c, d int) {}\n",
			".uir.yaml":  "version: 1\nlint:\n  rules:\n    - {id: params, query: 'SELECT symbol.name AS name FROM func:* WHERE symbol.metrics.param_count > 3', message: '{{.name}}'}\n",
		} {
			Expect(os.WriteFile(filepath.Join(root, name), []byte(content), 0o600)).To(Succeed())
		}
		database := openCommandDatabase(ctx)
		for _, tagged := range []bool{false, true} {
			output, err := captureStdout(func() error {
				command := newRootCommand(&commandRuntime{database: database})
				args := []string{"lint", "--config", filepath.Join(root, ".uir.yaml"), "--format=json", "--no-progress"}
				if tagged {
					args = append(args, "--tags=feature", "--tags=integration")
				}
				command.SetArgs(args)
				return command.ExecuteContext(ctx)
			})
			Expect(err != nil).To(Equal(tagged), output)
			var reports []*linters.LinterResult
			Expect(json.Unmarshal([]byte(output), &reports)).To(Succeed(), output)
			Expect(reports).To(HaveLen(1))
			Expect(reports[0].Success).To(BeTrue(), reports[0].Error)
			if tagged {
				Expect(reports[0].Violations).To(ConsistOf(HaveField("Message", HaveValue(Equal("Feature")))))
			} else {
				Expect(reports[0].Violations).To(BeEmpty())
			}
		}
	})
})
