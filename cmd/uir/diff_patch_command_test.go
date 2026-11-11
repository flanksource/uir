package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("uir diff --patch", func() {
	It("writes undecorated Git hunks from stdin without a database", func(ctx SpecContext) {
		workspace := GinkgoT().TempDir()
		gitIn(workspace, "init", "--quiet", "--initial-branch=main")
		Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.org/patchcli\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspace, "run.go"), []byte("package p\nfunc Before() {}\n"), 0o644)).To(Succeed())
		gitIn(workspace, "add", "go.mod", "run.go")
		gitIn(workspace, "commit", "--quiet", "--message", "before")
		Expect(os.WriteFile(filepath.Join(workspace, "run.go"), []byte("package p\nfunc After() {}\n"), 0o644)).To(Succeed())
		patch := gitIn(workspace, "diff", "--full-index", "--no-renames") + "\n"
		chdir(workspace)
		runtime := &commandRuntime{}
		root := newRootCommand(runtime)
		root.SetIn(strings.NewReader(patch))
		root.SetArgs([]string{"diff", "--patch", "--visibility", "all", "--change-type", "renamed"})
		output, err := captureStdout(func() error { return root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime)) })
		Expect(err).To(Succeed())
		Expect(output).To(Equal(patch))
	})
	DescribeTable("rejects conflicting output before reading stdin", func(ctx SpecContext, flag string) {
		runtime := &commandRuntime{}
		output, err := runCLI(ctx, runtime, "diff", "--patch", flag)
		Expect(err).To(HaveOccurred())
		Expect(output).To(BeEmpty())
	}, Entry("JSON", "--json"), Entry("statistics", "--stat"))
})
