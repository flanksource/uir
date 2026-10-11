package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("historical import flags", func() {
	It("publishes a retained commit from CLI flags without creating a head", func(ctx context.Context) {
		database := openCommandDatabase(ctx)
		runtime := &commandRuntime{database: database}
		body, err := os.ReadFile(filepath.Join(importExamples, "co-prod.json"))
		Expect(err).ToNot(HaveOccurred())
		command := newRootCommand(runtime)
		command.SetOut(io.Discard)
		command.SetErr(io.Discard)
		command.SetIn(strings.NewReader(string(body)))
		commit := strings.Repeat("a", 40)
		command.SetArgs([]string{"import", "-", "--historical", "--git-commit", commit, "--producer", "cli-test-v1"})
		Expect(command.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))).To(Succeed())
		var snapshot storage.ModuleSnapshot
		Expect(database.First(&snapshot).Error).To(Succeed())
		Expect([]any{snapshot.Reason, snapshot.GitCommit}).To(Equal([]any{storage.ReasonHistorical, commit}))
		var count int64
		Expect(database.Model(&storage.ModuleLocationHead{}).Count(&count).Error).To(Succeed())
		Expect(count).To(BeZero())
	})
})
