package indexer

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("readGoEnvironment", func() {
	const (
		member  = "example.org/workspace/app"
		fixture = "example.org/workspace/app/fixture"
	)

	It("loads a module its enclosing go.work does not use with GOWORK=off", func(ctx context.Context) {
		workspace, err := filepath.EvalSymlinks(GinkgoT().TempDir())
		Expect(err).ToNot(HaveOccurred())
		app := filepath.Join(workspace, "app")
		writeGoMod(app, member)
		writeGoMod(filepath.Join(app, "fixture"), fixture)
		writeFile(filepath.Join(workspace, "go.work"), "go 1.26\n\nuse ./app\n")

		used, err := readGoEnvironment(ctx, app)
		Expect(err).ToNot(HaveOccurred())
		unused, err := readGoEnvironment(ctx, filepath.Join(app, "fixture"))
		Expect(err).ToNot(HaveOccurred())

		Expect([]any{used.WorkFile, used.Variant.GoWorkOff, unused.WorkFile, unused.Variant.GoWorkOff}).
			To(Equal([]any{filepath.Join(workspace, "go.work"), false, "", true}))
	})
})
