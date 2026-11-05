package main

import (
	"context"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("module import --plugin", func() {
	buildStub := func() string {
		GinkgoHelper()
		binary := filepath.Join(GinkgoT().TempDir(), "stub")
		output, err := exec.Command("go", "build", "-o", binary, "../../producer/testdata/stubplugin").CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(output))
		return binary
	}

	It("publishes what the plugin returns for the checkout argument", func(ctx context.Context) {
		database := openCommandDatabase(ctx)
		runtime := &commandRuntime{database: database}
		GinkgoT().Setenv("STUB_PUBLICATION", filepath.Join(importExamples, "co-prod.json"))
		root := newRootCommand(runtime)
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs([]string{"import", "--plugin", "stub", "--plugin-path", buildStub(), GinkgoT().TempDir()})
		Expect(root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))).To(Succeed())
		rows, err := listModuleRoots(ctx, database)
		Expect(err).ToNot(HaveOccurred())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].RootKey).To(Equal("Co/Prod"))
	})

	It("rejects plugin-only flags without --plugin", func(ctx context.Context) {
		runtime := &commandRuntime{database: openCommandDatabase(ctx)}
		root := newRootCommand(runtime)
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetIn(strings.NewReader(""))
		root.SetArgs([]string{"import", "--security", "-"})
		Expect(root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))).To(MatchError(ContainSubstring("apply only with --plugin")))
	})

	It("refuses --plugin over HTTP", func(ctx context.Context) {
		_, handler := servedRuntime(ctx)
		refused := postJSON(ctx, handler, "/api/v1/modules/import", `{"plugin":"repomap","args":["."]}`)
		Expect(refused.Code).To(Equal(http.StatusBadRequest), refused.Body.String())
		Expect(refused.Body.String()).To(ContainSubstring("plugin imports run only from the command line"))
	})
})
