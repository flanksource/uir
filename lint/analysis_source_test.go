package lint

import (
	"os"
	"path/filepath"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Git-scoped typed lint analysis", func() {
	It("uses captured variable types and configuration without advancing live analysis heads", func(ctx SpecContext) {
		root := sourceCheckout(ctx)
		config := filepath.Join(root, ConfigName)
		source := filepath.Join(root, "small.go")
		Expect(os.WriteFile(config, []byte("version: 1\nlint:\n  rules:\n    - id: captured\n      query: SELECT symbol.name AS name FROM var:* WHERE symbol.type = 'int'\n      message: '{{.name}}'\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(source, []byte("package linted\nvar Captured int\n"), 0o600)).To(Succeed())
		sourceGit(ctx, root, "add", ConfigName, "small.go")
		database := lintDatabase(ctx, "sqlite")
		for _, options := range []Options{{Staged: true, GroupBy: "symbol"}, {Changed: true, Revision: "HEAD", GroupBy: "symbol"}} {
			request, err := Prepare(ctx, root, config, options)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(source, []byte("package linted\nvar Mutable string\n"), 0o600)).To(Succeed())
			Expect(os.WriteFile(config, []byte("invalid: ["), 0o600)).To(Succeed())
			result := request.Run(ctx, database)
			Expect(result.Success).To(BeTrue(), result.Error)
			Expect(result.Violations).To(ConsistOf(And(HaveField("File", source), HaveField("Message", HaveValue(Equal("Captured"))))))
			Expect(result.Groups).To(ConsistOf(HaveField("SymbolID", Not(BeEmpty()))))
			Expect(request.Release()).To(Succeed())
			if options.Staged {
				sourceGit(ctx, root, "commit", "-qm", "capture typed source")
			}
		}
		var heads int64
		Expect(database.Model(&storage.AnalysisHead{}).Count(&heads).Error).To(Succeed())
		Expect(heads).To(BeZero())
	})
})
