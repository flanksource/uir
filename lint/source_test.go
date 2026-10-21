package lint

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func sourceGit(ctx context.Context, root string, args ...string) string {
	GinkgoHelper()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Example", "GIT_AUTHOR_EMAIL=example@example.org", "GIT_COMMITTER_NAME=Example", "GIT_COMMITTER_EMAIL=example@example.org")
	output, err := command.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), string(output))
	return strings.TrimSpace(string(output))
}

func sourceCheckout(ctx context.Context) string {
	GinkgoHelper()
	root, err := filepath.EvalSymlinks(filepath.Dir(lintCheckout().Path))
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".tmp/\nignored.go\n"), 0o600)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(root, "other.go"), []byte("package linted\nfunc Other() {}\n"), 0o600)).To(Succeed())
	sourceGit(ctx, root, "init", "-q")
	sourceGit(ctx, root, "add", "go.mod", "small.go", "other.go", ConfigName, ".gitignore")
	sourceGit(ctx, root, "commit", "-qm", "initial")
	return root
}

var _ = Describe("Git-scoped lint", func() {
	It("intersects literal changed paths with filters and retains matching groups", func(ctx SpecContext) {
		database := lintDatabase(ctx, "sqlite")
		config := lintCheckout()
		root := filepath.Dir(config.Path)
		Expect(os.WriteFile(filepath.Join(root, "other.go"), []byte("package linted\nfunc Other() {}\n"), 0o600)).To(Succeed())
		result := Run(ctx, database, config, Options{Files: []string{filepath.Join(root, "small.go")}, GroupBy: "symbol"})
		Expect(result.Success).To(BeTrue(), result.Error)
		Expect(result.Violations).To(ConsistOf(HaveField("File", filepath.Join(root, "small.go"))))
		Expect(result.Groups).To(ConsistOf(HaveField("File", filepath.Join(root, "small.go"))))
		Expect(result.FileCount).To(Equal(1))
		result = Run(ctx, database, config, Options{Files: []string{}, GroupBy: "symbol"})
		Expect(result.Success).To(BeTrue(), result.Error)
		Expect(result.Violations).To(BeEmpty())
		Expect(result.Groups).To(BeEmpty())
	})

	It("selects working changes and literal untracked names while preserving fileless diagnostics", func(ctx SpecContext) {
		root := sourceCheckout(ctx)
		Expect(os.WriteFile(filepath.Join(root, "other.go"), []byte("package linted\nvar _ = undefined\n"), 0o600)).To(Succeed())
		sourceGit(ctx, root, "add", "other.go")
		sourceGit(ctx, root, "commit", "-qm", "existing coverage gap")
		Expect(os.WriteFile(filepath.Join(root, "small.go"), []byte("package linted\nfunc Changed() {}\n"), 0o600)).To(Succeed())
		name := "literal [name].go"
		Expect(os.WriteFile(filepath.Join(root, name), []byte("package linted\nfunc Literal() {}\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "ignored.go"), []byte("package linted\nfunc Ignored() {}\n"), 0o600)).To(Succeed())
		request, err := Prepare(ctx, root, "", Options{Changed: true, GroupBy: "symbol", Exclude: []string{"small.go"}})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(request.Release)
		result := request.Run(ctx, lintDatabase(ctx, "sqlite"))
		Expect(result.Success).To(BeTrue(), result.Error)
		Expect(result.Violations).To(ContainElement(HaveField("File", filepath.Join(root, name))))
		Expect(result.Violations).To(ContainElement(HaveField("File", "")))
		Expect(result.Violations).NotTo(ContainElement(HaveField("File", filepath.Join(root, "other.go"))))
		Expect(result.FileCount).To(Equal(1))
		Expect(result.Groups).To(HaveLen(2))
	})

	It("loads historical configuration and contents despite malformed current configuration", func(ctx SpecContext) {
		root := sourceCheckout(ctx)
		revision := sourceGit(ctx, root, "rev-parse", "HEAD")
		Expect(os.WriteFile(filepath.Join(root, ConfigName), []byte("invalid: ["), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "small.go"), []byte("package linted\nfunc Current() {}\n"), 0o600)).To(Succeed())
		before := sourceGit(ctx, root, "status", "--porcelain")
		request, err := Prepare(ctx, root, "", Options{Changed: true, Revision: revision})
		Expect(err).NotTo(HaveOccurred())
		result := request.Run(ctx, lintDatabase(ctx, "sqlite"))
		Expect(result.Success).To(BeTrue(), result.Error)
		Expect(result.Violations).To(HaveLen(2))
		Expect(result.WorkDir).To(Equal(root))
		Expect(result.Violations).To(ContainElement(HaveField("File", filepath.Join(root, "small.go"))))
		scratch := request.Options.Source.Info().Dir
		Expect(request.Release()).To(Succeed())
		Expect(scratch).NotTo(BeADirectory())
		Expect(sourceGit(ctx, root, "status", "--porcelain")).To(Equal(before))
	})

	It("uses staged config and nested manifests without borrowing unstaged contents", func(ctx SpecContext) {
		root := sourceCheckout(ctx)
		Expect(os.Mkdir(filepath.Join(root, "nested"), 0o700)).To(Succeed())
		for path, content := range map[string]string{
			ConfigName:         strings.ReplaceAll(validConfig, "is small", "is staged"),
			"small.go":         "package linted\nfunc Staged() {}\n",
			"nested/go.mod":    "module example.org/nested-staged\n\ngo 1.26\n",
			"nested/nested.go": "package nested\nfunc Nested() {}\n",
		} {
			Expect(os.WriteFile(filepath.Join(root, path), []byte(content), 0o600)).To(Succeed())
		}
		sourceGit(ctx, root, "add", ConfigName, "small.go", "nested/go.mod", "nested/nested.go")
		Expect(os.WriteFile(filepath.Join(root, ConfigName), []byte("version: 99\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "nested", "go.mod"), []byte("invalid go.mod"), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "small.go"), []byte("invalid Go"), 0o600)).To(Succeed())
		before, err := os.ReadFile(filepath.Join(root, ".git", "index"))
		Expect(err).NotTo(HaveOccurred())
		request, err := Prepare(ctx, root, filepath.Join(root, ConfigName), Options{Staged: true, GroupBy: "symbol"})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(request.Release)
		result := request.Run(ctx, lintDatabase(ctx, "sqlite"))
		Expect(result.Success).To(BeTrue(), result.Error)
		Expect(result.Violations).To(ConsistOf(
			And(HaveField("File", filepath.Join(root, "small.go")), HaveField("Message", HaveValue(Equal("small.go is staged")))),
			And(HaveField("File", filepath.Join(root, "nested", "nested.go")), HaveField("Message", HaveValue(Equal("nested.go is staged")))),
		))
		Expect(result.FileCount).To(Equal(2))
		Expect(result.Groups).To(HaveLen(2))
		Expect(os.ReadFile(filepath.Join(root, ".git", "index"))).To(Equal(before))
	})

	It("keeps empty selections empty and supports current no-index checks", func(ctx SpecContext) {
		root := sourceCheckout(ctx)
		request, err := Prepare(ctx, root, "", Options{Changed: true, NoIndex: true})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(request.Release)
		Expect(request.Empty()).To(BeTrue())
		result := request.Run(ctx, nil)
		Expect(result.Success).To(BeTrue(), result.Error)
		Expect(result.Violations).To(BeEmpty())
	})

	It("never borrows current configuration when the selected tree has none", func(ctx SpecContext) {
		root := sourceCheckout(ctx)
		Expect(os.Remove(filepath.Join(root, ConfigName))).To(Succeed())
		sourceGit(ctx, root, "add", ConfigName)
		sourceGit(ctx, root, "commit", "-qm", "remove config")
		Expect(os.WriteFile(filepath.Join(root, ConfigName), []byte(validConfig), 0o600)).To(Succeed())
		_, err := Prepare(ctx, root, "", Options{Changed: true, Revision: "HEAD"})
		Expect(err).To(MatchError(ContainSubstring("configuration inside")))
		entries, err := os.ReadDir(filepath.Join(root, ".tmp", "uir-sources"))
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	It("reads historical source headers in modules removed from the current checkout", func(ctx SpecContext) {
		root := sourceCheckout(ctx)
		config := "version: 1\nlint:\n  rules:\n    - {id: headers, query: \"SELECT path FROM files(include_excluded => TRUE) WHERE file.build_constraint NOT REQUIRES 'hack'\", message: '{{.path}}'}\n"
		Expect(os.WriteFile(filepath.Join(root, ConfigName), []byte(config), 0o600)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(root, "removed"), 0o700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "removed", "go.mod"), []byte("module example.org/removed\n\ngo 1.26\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "removed", "removed.go"), []byte("package removed\nfunc Removed() {}\n"), 0o600)).To(Succeed())
		sourceGit(ctx, root, "add", ConfigName, "removed/go.mod", "removed/removed.go")
		sourceGit(ctx, root, "commit", "-qm", "add historical module")
		Expect(os.RemoveAll(filepath.Join(root, "removed"))).To(Succeed())
		request, err := Prepare(ctx, root, "", Options{Changed: true, Revision: "HEAD"})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(request.Release)
		result := request.Run(ctx, lintDatabase(ctx, "sqlite"))
		Expect(result.Success).To(BeTrue(), result.Error)
		Expect(result.Violations).To(ConsistOf(HaveField("File", filepath.Join(root, "removed", "removed.go"))))
	})
})
