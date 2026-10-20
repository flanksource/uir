package lint

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Grouped lint results", func() {
	DescribeTable("groups rules by their source symbol independently of projected columns", func(ctx SpecContext, backend string) {
		config := lintCheckout()
		root := filepath.Dir(config.Path)
		Expect(os.WriteFile(filepath.Join(root, "caller.go"), []byte("package linted\nfunc Caller() { Small(1,2,3,4) }\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(config.Path, []byte(validConfig+"    - {id: declaration, query: 'SELECT path FROM func:Caller', message: declaration}\n    - {id: calls, query: 'SELECT symbol.name AS name FROM callees(func:Caller)', message: '{{.name}}'}\n"), 0o600)).To(Succeed())
		config, err := Load(config.Path)
		Expect(err).NotTo(HaveOccurred())
		database := lintDatabase(ctx, backend)
		result := Run(ctx, database, config, Options{GroupBy: "symbol"})
		Expect(result.Success).To(BeTrue(), result.Error)
		Expect(result.Violations).To(HaveLen(4))
		Expect(result.Groups).To(HaveLen(3))
		Expect(result.Groups).To(ContainElement(And(
			HaveField("File", filepath.Join(root, "caller.go")),
			HaveField("Symbol", ContainSubstring("Caller")),
			HaveField("SymbolID", Not(BeEmpty())),
			HaveField("Violations", ConsistOf(HaveField("Rule.Method", "declaration"), HaveField("Rule.Method", "calls"))),
		)))
		files := Run(ctx, database, config, Options{GroupBy: "file", NoIndex: true})
		Expect(files.Success).To(BeTrue(), files.Error)
		Expect(files.Groups).To(ConsistOf(
			And(HaveField("File", filepath.Join(root, "caller.go")), HaveField("Violations", HaveLen(3))),
			And(HaveField("File", filepath.Join(root, "small.go")), HaveField("Violations", HaveLen(1))),
		))
		Expect(os.WriteFile(config.Path, []byte(validConfig+"    - {id: broken, query: 'SELECT path FROM files()', message: '{{.missing}}'}\n"), 0o600)).To(Succeed())
		config, err = Load(config.Path)
		Expect(err).NotTo(HaveOccurred())
		partial := Run(ctx, database, config, Options{GroupBy: "symbol", NoIndex: true})
		Expect(partial.Success).To(BeFalse())
		Expect(partial.Error).To(ContainSubstring("broken"))
		Expect(partial.Groups).To(HaveLen(2))
		Expect(partial.Violations).To(HaveLen(2))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
	It("rejects an unknown grouping before accessing the database", func(ctx SpecContext) {
		result := Run(ctx, nil, lintCheckout(), Options{GroupBy: "rule"})
		Expect(result.Success).To(BeFalse())
		Expect(result.Error).To(ContainSubstring("group-by"))
	})
})
