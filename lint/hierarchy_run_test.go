package lint

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Hierarchical lint execution", func() {
	It("runs home rules against the checkout and keeps project-relative filtering", func(ctx SpecContext) {
		root := sourceCheckout(ctx)
		home := GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)
		Expect(os.WriteFile(filepath.Join(home, ConfigName), []byte(validConfig), 0o600)).To(Succeed())
		Expect(os.Remove(filepath.Join(root, ConfigName))).To(Succeed())
		request, err := Prepare(ctx, root, "", Options{Include: []string{"small.go"}})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(request.Release)
		Expect(request.Options.WorkDir).To(Equal(root))
		result := request.Run(ctx, lintDatabase(ctx, "sqlite"))
		Expect(result.Success).To(BeTrue(), result.Error)
		Expect(result.RuleCount).To(Equal(1))
		Expect(result.Violations).To(ConsistOf(And(
			HaveField("File", filepath.Join(root, "small.go")),
			HaveField("Message", HaveValue(Equal("small.go is small"))),
		)))
	})

	DescribeTable("merges only captured ancestors for immutable sources", func(ctx SpecContext, options Options) {
		root := sourceCheckout(ctx)
		home := GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)
		Expect(os.WriteFile(filepath.Join(home, ConfigName), []byte("invalid: ["), 0o600)).To(Succeed())
		nested := filepath.Join(root, "nested")
		Expect(os.Mkdir(nested, 0o700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(nested, ConfigName), []byte(strings.ReplaceAll(validConfig, "id: small", "id: nested")), 0o600)).To(Succeed())
		sourceGit(ctx, root, "add", "nested/.uir.yaml")
		sourceGit(ctx, root, "commit", "-qm", "nested configuration")
		Expect(os.WriteFile(filepath.Join(root, ConfigName), []byte("version: 99\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(nested, ConfigName), []byte("invalid: ["), 0o600)).To(Succeed())
		request, err := Prepare(ctx, nested, "", options)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(request.Release)
		Expect(request.Config.Lint.Rules).To(HaveExactElements(HaveField("ID", "small"), HaveField("ID", "nested")))
		Expect(request.Options.WorkDir).To(Equal(nested))
		Expect(request.Config.Root).To(Equal(filepath.Join(request.Options.Source.Info().Dir, "nested")))
	}, Entry("staged", Options{Staged: true}), Entry("commit", Options{Changed: true, Revision: "HEAD"}))
})
