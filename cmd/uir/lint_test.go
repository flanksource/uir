package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/flanksource/gavel/linters"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("UIR lint command", func() {
	It("renders findings as JSON, fails error findings and succeeds for warnings", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		root := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.org/cli-lint\n\ngo 1.26\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "sample.go"), []byte("package sample\nfunc Sample(a,b,c,d int) {}\n"), 0o600)).To(Succeed())
		path := filepath.Join(root, ".uir.yaml")
		Expect(os.WriteFile(path, []byte("version: 1\nlint:\n  rules:\n    - {id: small, severity: warning, query: 'SELECT path FROM files()', message: '{{.path}}'}\n    - {id: function-parameters, query: 'SELECT symbol.name AS name FROM func:* WHERE symbol.metrics.param_count > 3', message: '{{.name}}'}\n"), 0o600)).To(Succeed())
		for _, sample := range []struct {
			args  []string
			fails bool
			count int
		}{{nil, true, 2}, {[]string{"--rule", "small", "--no-index"}, false, 1},
			{[]string{"--no-index", "--exclude", "function-parameters"}, false, 1},
			{[]string{"--no-index", "--rule", "function-parameters", "--exclude", "function-parameters"}, false, 0},
			{[]string{"--no-index", "--exclude", "function-parameters,sample.go"}, false, 0},
			{[]string{"--no-index", "--exclude", "function-parameters", "--exclude", "sample.go"}, false, 0},
			{[]string{"--no-index", "--include", "other/**", "--include", "**/*.go", "--exclude", "sample.go"}, false, 0},
			{[]string{"--no-index", "--include", "missing.go,sample.go"}, true, 2}} {
			output, err := captureStdout(func() error {
				command := newRootCommand(&commandRuntime{database: database})
				command.SetArgs(append([]string{"lint", "--config", path, "--format", "json", "--no-progress"}, sample.args...))
				return command.ExecuteContext(ctx)
			})
			Expect(err != nil).To(Equal(sample.fails))
			var results []*linters.LinterResult
			Expect(json.Unmarshal([]byte(output), &results)).To(Succeed(), output)
			Expect(results).To(HaveLen(1))
			Expect(results[0].Success).To(BeTrue())
			Expect(results[0].Violations).To(HaveLen(sample.count))
		}
	})
	It("registers a local command and reports config errors as JSON before opening a database", func(ctx SpecContext) {
		runtime := &commandRuntime{DSN: "invalid://must-not-open"}
		root := newRootCommand(runtime)
		path := filepath.Join(GinkgoT().TempDir(), ".uir.yaml")
		Expect(os.WriteFile(path, []byte("version: 9\n"), 0o600)).To(Succeed())
		output, err := captureStdout(func() error {
			root.SetArgs([]string{"lint", "--config", path, "--format", "json", "--no-progress"})
			return root.ExecuteContext(ctx)
		})
		Expect(err).To(HaveOccurred())
		var results []*linters.LinterResult
		Expect(json.Unmarshal([]byte(output), &results)).To(Succeed(), output)
		Expect(results).To(HaveLen(1))
		Expect(results[0].Error).To(ContainSubstring("unsupported version"))
		Expect(runtime.database).To(BeNil())
	})
	It("rejects invalid filters as JSON before opening a database", func(ctx SpecContext) {
		path := filepath.Join(GinkgoT().TempDir(), ".uir.yaml")
		Expect(os.WriteFile(path, []byte("version: 1\nlint:\n  rules:\n    - {id: files, query: 'SELECT path FROM files()', message: '{{.path}}'}\n"), 0o600)).To(Succeed())
		output, err := captureStdout(func() error {
			command := newRootCommand(&commandRuntime{DSN: "invalid://must-not-open"})
			command.SetArgs([]string{"lint", "--config", path, "--include", "cmd/**.go", "--format", "json", "--no-progress"})
			return command.ExecuteContext(ctx)
		})
		Expect(err).To(MatchError(ContainSubstring("file filter")))
		var results []*linters.LinterResult
		Expect(json.Unmarshal([]byte(output), &results)).To(Succeed(), output)
		Expect(results).To(ConsistOf(HaveField("Error", ContainSubstring("file filter"))))
	})
})
