package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/flanksource/gavel/linters"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Hack build-tag command", func() {
	It("returns eight warning findings as native CLI JSON and succeeds", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		root := GinkgoT().TempDir()
		Expect(os.CopyFS(root, os.DirFS("../../lint/testdata/hack-build-tags"))).To(Succeed())
		output, err := captureStdout(func() error {
			command := newRootCommand(&commandRuntime{database: database})
			command.SetArgs([]string{"lint", "--config", filepath.Join(root, ".uir.yaml"), "--rule", "hack-build-tag", "--format", "json", "--no-progress"})
			return command.ExecuteContext(ctx)
		})
		Expect(err).NotTo(HaveOccurred(), output)
		var results []*linters.LinterResult
		Expect(json.Unmarshal([]byte(output), &results)).To(Succeed(), output)
		Expect(results).To(HaveLen(1))
		Expect(results[0].Success).To(BeTrue(), results[0].Error)
		Expect(results[0].Violations).To(HaveLen(8))
		Expect(results[0].Violations).To(ContainElement(And(HaveField("File", filepath.Join(root, "hack/ignored.go")), HaveField("Severity", BeEquivalentTo("warning")))))
	})
})
