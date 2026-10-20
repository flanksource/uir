package lint

import (
	"os"
	"path/filepath"

	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Lint file filters", func() {
	DescribeTable("matches SQL globs with exclusion precedence", func(include, exclude []string, path string, expected bool) {
		matches, err := query.CompilePathFilter(include, exclude)
		Expect(err).NotTo(HaveOccurred())
		Expect(matches(path)).To(Equal(expected))
	},
		Entry("unrestricted", nil, nil, "small.go", true),
		Entry("root globstar", []string{"**/*.go"}, nil, "small.go", true),
		Entry("nested globstar", []string{"**/*.go"}, nil, "cmd/app/main.go", true),
		Entry("alternatives", []string{"cmd/**", "internal/**"}, nil, "internal/store.go", true),
		Entry("not included", []string{"cmd/**"}, nil, "internal/store.go", false),
		Entry("exclusion wins", []string{"**/*.go"}, []string{"**/*_test.go"}, "cmd/main_test.go", false),
		Entry("segment boundary", []string{"cmd/**"}, nil, "mycmd/main.go", false),
		Entry("recursive sugar", []string{"cmd/..."}, nil, "cmd/app/main.go", true),
		Entry("fileless diagnostics", []string{"cmd/**"}, []string{"**"}, "", true),
	)
	DescribeTable("rejects invalid globs", func(pattern string) {
		_, err := query.CompilePathFilter([]string{pattern}, nil)
		Expect(err).To(HaveOccurred())
		_, err = query.CompilePathFilter(nil, []string{pattern})
		Expect(err).To(HaveOccurred())
	}, Entry("empty", ""), Entry("partial globstar", "cmd/**.go"), Entry("dangling escape", "cmd/\\"))
	It("rejects invalid filters before accessing the database or indexing", func(ctx SpecContext) {
		report := Run(ctx, nil, lintCheckout(), Options{Exclude: []string{"cmd/**.go"}})
		Expect(report.Success).To(BeFalse())
		Expect(report.Error).To(ContainSubstring("file filter"))
	})
	It("filters call-site paths while retaining excluded callees as analysis context", func(ctx SpecContext) {
		config := lintCheckout()
		root := filepath.Dir(config.Path)
		Expect(os.WriteFile(filepath.Join(root, "caller.go"), []byte("package linted\nfunc Caller() { Small(1,2,3,4) }\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(config.Path, []byte("version: 1\nlint:\n  rules:\n    - {id: calls, query: 'SELECT symbol.name AS name FROM callees(func:Caller)', message: '{{.name}}'}\n"), 0o600)).To(Succeed())
		config, err := Load(config.Path)
		Expect(err).NotTo(HaveOccurred())
		database := lintDatabase(ctx, "sqlite")
		first := Run(ctx, database, config, Options{Include: []string{"caller.go"}, Exclude: []string{"small.go"}})
		Expect(first.Success).To(BeTrue(), first.Error)
		Expect(first.Violations).To(ConsistOf(And(HaveField("File", filepath.Join(root, "caller.go")), HaveField("Message", HaveValue(Equal("Small"))))))
		filtered := Run(ctx, database, config, Options{NoIndex: true, Include: []string{"caller.go"}, Exclude: []string{"caller.go"}})
		Expect(filtered.Success).To(BeTrue(), filtered.Error)
		Expect(filtered.Violations).To(BeEmpty())
		Expect(filtered.FileCount).To(BeZero())
	})
})
