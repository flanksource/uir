package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/lint"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Git-scoped lint command", func() {
	DescribeTable("rejects incompatible selectors before opening the database", func(ctx SpecContext, args []string, message string) {
		runtime := &commandRuntime{DSN: "invalid://must-not-open"}
		output, err := captureStdout(func() error {
			command := newRootCommand(runtime)
			command.SetArgs(append([]string{"lint", "--format", "json", "--no-progress"}, args...))
			return command.ExecuteContext(ctx)
		})
		Expect(err).To(MatchError(ContainSubstring(message)))
		Expect(output).To(ContainSubstring(message))
		Expect(runtime.database).To(BeNil())
	},
		Entry("two selectors", []string{"--changed", "--staged"}, "--changed and --staged"),
		Entry("bare revision", []string{"HEAD"}, "requires --changed"),
		Entry("historical no-index", []string{"--changed", "HEAD", "--no-index"}, "--no-index cannot"),
		Entry("staged no-index", []string{"--staged", "--no-index"}, "--no-index cannot"),
		Entry("snapshot selector", []string{"--changed", "--snapshot", "some-id"}, "--snapshot cannot"),
		Entry("all changed", []string{"--all", "--changed"}, "--all cannot"),
		Entry("all staged", []string{"--all", "--staged"}, "--all cannot"),
		Entry("all snapshot", []string{"--all", "--snapshot", "some-id"}, "--all cannot"),
	)

	It("renders filtered staged findings and their groups as JSON and applies the final verdict", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		root := GinkgoT().TempDir()
		for path, content := range map[string]string{
			"go.mod":       "module example.org/lint-cli\n\ngo 1.26\n",
			".uir.yaml":    "version: 1\nlint:\n  rules:\n    - {id: parameters, query: 'SELECT symbol.name AS name FROM func:* WHERE symbol.metrics.param_count > 3', message: '{{.name}}'}\n",
			"changed.go":   "package cli\nfunc Changed() {}\n",
			"unchanged.go": "package cli\nfunc Existing(a,b,c,d int) {}\n",
		} {
			Expect(os.WriteFile(filepath.Join(root, path), []byte(content), 0o600)).To(Succeed())
		}
		gitIn(root, "init", "-q")
		gitIn(root, "add", "go.mod", ".uir.yaml", "changed.go", "unchanged.go")
		gitIn(root, "commit", "-qm", "initial")
		Expect(os.WriteFile(filepath.Join(root, "changed.go"), []byte("package cli\nfunc Changed(a,b,c,d int) {}\n"), 0o600)).To(Succeed())
		gitIn(root, "add", "changed.go")
		for _, exclude := range []bool{false, true} {
			output, err := captureStdout(func() error {
				command := newRootCommand(&commandRuntime{database: database})
				args := []string{"lint", "--config", filepath.Join(root, ".uir.yaml"), "--staged", "--group-by", "symbol", "--format", "json", "--no-progress"}
				if exclude {
					args = append(args, "--exclude", "changed.go")
				}
				command.SetArgs(args)
				return command.ExecuteContext(ctx)
			})
			Expect(err != nil).To(Equal(!exclude), output)
			var results []*lint.Result
			Expect(json.Unmarshal([]byte(output), &results)).To(Succeed(), output)
			Expect(results).To(HaveLen(1))
			Expect(results[0].Success).To(BeTrue(), results[0].Error)
			if exclude {
				Expect(results[0].Violations).To(BeEmpty())
				Expect(results[0].Groups).To(BeEmpty())
			} else {
				Expect(results[0].Violations).To(ConsistOf(HaveField("File", filepath.Join(root, "changed.go"))))
				Expect(results[0].Groups).To(ConsistOf(HaveField("Symbol", ContainSubstring("Changed"))))
			}
		}
		for _, sample := range []struct {
			path  string
			count int
		}{{"changed.go", 0}, {"unchanged.go", 1}} {
			output, err := captureStdout(func() error {
				command := newRootCommand(&commandRuntime{database: database})
				command.SetArgs([]string{"lint", "--config", filepath.Join(root, ".uir.yaml"), "--changed", "HEAD", "--include", sample.path, "--format", "json", "--no-progress"})
				return command.ExecuteContext(ctx)
			})
			Expect(err != nil).To(Equal(sample.count != 0), output)
			var results []*lint.Result
			Expect(json.Unmarshal([]byte(output), &results)).To(Succeed(), output)
			Expect(results[0].Violations).To(HaveLen(sample.count))
		}
	})

	It("does not open a database when no working paths changed", func(ctx SpecContext) {
		root := GinkgoT().TempDir()
		path := filepath.Join(root, ".uir.yaml")
		Expect(os.WriteFile(path, []byte("version: 1\nlint:\n  rules:\n    - {id: files, query: 'SELECT path FROM files()', message: '{{.path}}'}\n"), 0o600)).To(Succeed())
		gitIn(root, "init", "-q")
		gitIn(root, "add", ".uir.yaml")
		gitIn(root, "commit", "-qm", "initial")
		runtime := &commandRuntime{DSN: "invalid://must-not-open"}
		output, err := captureStdout(func() error {
			command := newRootCommand(runtime)
			command.SetArgs([]string{"lint", "--config", path, "--changed", "--format", "json", "--no-progress"})
			return command.ExecuteContext(ctx)
		})
		Expect(err).NotTo(HaveOccurred(), output)
		Expect(runtime.database).To(BeNil())
		var results []*lint.Result
		Expect(json.Unmarshal([]byte(output), &results)).To(Succeed())
		Expect(results[0].Violations).To(BeEmpty())
	})
})
