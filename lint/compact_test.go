package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Compact lint rules", func() {
	DescribeTable("reports schema-agnostic writes with the same physical witnesses as SQL rules", func(ctx SpecContext, translation bool) {
		const count = 101
		const path = "scripts/PostDeployment_Seed/SQLData.xml"
		const template = "{{.symbol}} {{.symbol.module}} {{.symbol.package}} {{.symbol.kind}} {{.symbol.name}} {{.symbol.filename}} {{.symbol.filepath}} {{.symbol.location}} {{.symbol.line}} {{len .symbol.params}}"
		configPath := configFile(fmt.Sprintf(`version: 1
sources:
  - match: 'scripts/**/SQLData.xml'
    dialect: tsql
    transform: '{"content": file.content, "filename": file.path.trimSuffix(".xml") + ".sql", "package": "migrations", "metadata": {"phase": "post"}}'
lint:
  rules:
    - id: compact-writes
      query: '*.ascode ~w | *.astranslation ~w'
      message: '%s'
    - id: sql-writes
      query: |-
        -- The first significant token selects the query language.
        /* migration policy */ SELECT symbol FROM writes(table:*)
        WHERE symbol.name = 'ascode' OR symbol.name = 'astranslation'
      message: '%s'
    - id: script-phase
      query: 'kind:sql.script:*'
      message: '{{.symbol.name}} {{.kind.phase}}'
    - id: sql-script-phase
      query: SELECT path FROM sql.script:*
      message: '{{.symbol.name}} {{.kind.phase}}'
`, template, template))
		root := filepath.Dir(configPath)
		body := strings.Repeat("UPDATE finance.AsCode SET CodeValue = '01';\n", count)
		if translation {
			body += "INSERT INTO local.AsTranslation VALUES ('example');\n"
		}
		file := filepath.Join(root, path)
		Expect(os.MkdirAll(filepath.Dir(file), 0o700)).To(Succeed())
		Expect(os.WriteFile(file, []byte(body+"SELECT * FROM dbo.AsCode;\n"), 0o600)).To(Succeed())
		config, err := Load(configPath)
		Expect(err).NotTo(HaveOccurred())
		report := Run(ctx, lintDatabase(ctx, "sqlite"), config, Options{})
		Expect(report.Error).To(BeEmpty())
		want := make([]any, 0, 2*(count+1)+2)
		for _, id := range []string{"compact-writes", "sql-writes"} {
			for line := 1; line <= count; line++ {
				message := fmt.Sprintf("finance.ascode sql:%s finance table ascode SQLData.xml %s %s:%d %d 0", root, path, path, line, line)
				want = append(want, And(HaveField("Rule.Method", id), HaveField("Line", line), HaveField("File", file), HaveField("Message", HaveValue(Equal(message)))))
			}
			if translation {
				message := fmt.Sprintf("local.astranslation sql:%s local table astranslation SQLData.xml %s %s:102 102 0", root, path, path)
				want = append(want, And(HaveField("Rule.Method", id), HaveField("Line", 102), HaveField("File", file), HaveField("Message", HaveValue(Equal(message)))))
			}
		}
		for _, id := range []string{"script-phase", "sql-script-phase"} {
			want = append(want, And(HaveField("Rule.Method", id), HaveField("Message", HaveValue(Equal("SQLData post")))))
		}
		Expect(report.Violations).To(ConsistOf(want...))
	}, Entry("both tables", true), Entry("one absent table", false))

	It("reports ambiguity as a rule error instead of treating candidates as findings", func(ctx SpecContext) {
		config := lintCheckout()
		root := filepath.Dir(config.Path)
		Expect(os.WriteFile(filepath.Join(root, "method.go"), []byte("package linted\ntype Service struct{}\nfunc (Service) Small() {}\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(config.Path, []byte("version: 1\nlint:\n  rules:\n    - {id: ambiguous, query: 'Small <', message: '{{.symbol}}'}\n"), 0o600)).To(Succeed())
		config, err := Load(config.Path)
		Expect(err).NotTo(HaveOccurred())
		report := Run(ctx, lintDatabase(ctx, "sqlite"), config, Options{})
		Expect(report.Error).To(ContainSubstring("rule ambiguous: symbol spelling is ambiguous"))
		Expect(report.Violations).To(BeEmpty())
	})

	It("rejects compact call paths when loading the rule", func() {
		_, err := Load(configFile("version: 1\nlint:\n  rules:\n    - {id: path, query: 'Small >> Caller', message: '{{.symbol}}'}\n"))
		Expect(err).To(MatchError(ContainSubstring("rule path: compact lint must produce rows")))
	})

	It("binds a Go callee with the call site and preserves explicit SQL aliases", func(ctx SpecContext) {
		config := lintCheckout()
		root := filepath.Dir(config.Path)
		Expect(os.WriteFile(filepath.Join(root, "caller.go"), []byte("package linted\ntype Service struct{}\nfunc (Service) Run() { Small(1,2,3,4) }\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(config.Path, []byte(`version: 1
lint:
  rules:
    - id: compact-call
      query: 'method:Service.Run >'
      message: '{{.symbol}} at {{.symbol.location}} takes {{len .symbol.params}} {{index .symbol.params 0}} arguments'
    - id: sql-call
      query: SELECT path FROM callees(method:Service.Run)
      message: '{{.symbol}} at {{.symbol.location}} takes {{len .symbol.params}} {{index .symbol.params 0}} arguments'
    - id: sql-aliases
      query: SELECT symbol.name AS symbol, role AS kind FROM callees(method:Service.Run)
      message: '{{.symbol}} {{.kind}}'
`), 0o600)).To(Succeed())
		config, err := Load(config.Path)
		Expect(err).NotTo(HaveOccurred())
		report := Run(ctx, lintDatabase(ctx, "sqlite"), config, Options{GroupBy: "symbol"})
		Expect(report.Error).To(BeEmpty())
		const message = "example.org/linted.Small at caller.go:3 takes 4 int arguments"
		Expect(report.Violations).To(HaveExactElements(
			HaveField("Message", HaveValue(Equal(message))),
			HaveField("Message", HaveValue(Equal(message))),
			HaveField("Message", HaveValue(Equal("Small call"))),
		))
		Expect(report.Groups).To(HaveExactElements(And(HaveField("File", filepath.Join(root, "caller.go")), HaveField("Symbol", ContainSubstring("Run")))))
	})
})
