package main

import (
	"context"
	"encoding/json"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir/symboldiff"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("uir history show", func() {
	It("prints the selected commit's logical changes against its first parent", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		from, to, _ := indexedCommits(ctx, database)
		runtime := &commandRuntime{database: database}
		root := newRootCommand(runtime)
		root.SetArgs([]string{"history", "show", to, "--root", diffRoot, "--visibility", "all", "--format", "json"})
		output, err := captureStdout(func() error {
			return root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))
		})
		Expect(err).ToNot(HaveOccurred(), output)
		var result symboldiff.Result
		Expect(json.Unmarshal([]byte(output), &result)).To(Succeed(), output)
		Expect([]string{result.From.Commit, result.To.Commit}).To(Equal([]string{from, to}))
		Expect(result.Packages).ToNot(BeEmpty())
		Expect(clicky.IsLocalOnly(findCommand(findCommand(root, "history"), "show"))).To(BeTrue())
	})

	It("lists and shows the history of the working directory's checkout without --root", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		from, to, workspace := indexedCommits(ctx, database)
		runtime := &commandRuntime{database: database}
		chdir(workspace)

		output, err := runCLI(ctx, runtime, "history", "--format", "json")
		Expect(err).ToNot(HaveOccurred(), output)
		var history symboldiff.History
		Expect(json.Unmarshal([]byte(output), &history)).To(Succeed(), output)
		output, err = runCLI(ctx, runtime, "history", "show", to, "--format", "json")
		Expect(err).ToNot(HaveOccurred(), output)
		var shown symboldiff.Result
		Expect(json.Unmarshal([]byte(output), &shown)).To(Succeed(), output)

		Expect([]string{history.RootKey, history.Location, history.Commits[0].Commit}).To(Equal([]string{diffRoot, workspace, to}))
		Expect([]string{shown.RootKey, shown.From.Commit, shown.To.Commit}).To(Equal([]string{diffRoot, from, to}))
	})
})
