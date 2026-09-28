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
		from, to := indexedCommits(ctx, database)
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
})
