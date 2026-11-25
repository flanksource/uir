package lint

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/indexer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HCL sources", func() {
	It("declares every mapped table, column and index under the SQL producer's names and resolves script accesses to them", func(ctx SpecContext) {
		root := copyFixture("hcl-tables")
		config, err := Load(filepath.Join(root, ConfigName))
		Expect(err).NotTo(HaveOccurred())
		sources, err := readMappedSources(root, config.Sources)
		Expect(err).NotTo(HaveOccurred())
		publication, _, err := publishSQL(ctx, lintDatabase(ctx, "sqlite"), root, sources, indexer.PublishOptions{Producer: "uir-migration-sql"})
		Expect(err).NotTo(HaveOccurred())
		names := map[string]string{}
		for _, symbol := range publication.Symbols {
			Expect(symbol.ModuleKey).To(Equal("sql:" + root))
			names[indexer.SymbolID(symbol.Identity)] = symbol.Kind + " " + symbol.PackagePath + "." + symbol.Name
		}
		var declared, accessed []string
		for _, document := range publication.Documents {
			for _, symbol := range document.Content.Symbols {
				declared = append(declared, document.PathKey+": "+names[*symbol.ID])
			}
			for _, occurrence := range document.Content.Occurrences {
				accessed = append(accessed, occurrence.Role+" "+names[*occurrence.Symbol])
			}
		}
		slices.Sort(declared)
		Expect(declared).To(Equal([]string{
			"schema/tenant/orders.hcl: column dbo.CustomerGUID", "schema/tenant/orders.hcl: column dbo.OrderGUID", "schema/tenant/orders.hcl: column dbo.Total",
			"schema/tenant/orders.hcl: index dbo.IX_AcmeOrders_Customer", "schema/tenant/orders.hcl: index dbo.PK_AcmeOrders", "schema/tenant/orders.hcl: table dbo.acmeorders",
			"schema/vendor/customers.hcl: column dbo.CustomerGUID", "schema/vendor/customers.hcl: column dbo.Name", "schema/vendor/customers.hcl: table dbo.acmecustomers",
			"scripts/report.sql: sql.script migrations.report",
		}), "the project's default select leaves vendor tables out of a plan, not out of the analysis")
		Expect(accessed).To(ConsistOf("reference table dbo.acmeorders", "write table dbo.acmecustomers"))
		Expect(names).To(HaveLen(len(declared)), "an accessed table is the declared symbol, not a second one")
	})

	It("reports a script access through a rule that matches only declared tables", func(ctx SpecContext) {
		messages := func(root string) []string {
			request, err := Prepare(ctx, root, filepath.Join(root, ConfigName), Options{})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(request.Release)
			result := request.Run(ctx, lintDatabase(ctx, "sqlite"))
			Expect(result.Error).To(BeEmpty())
			var out []string
			for _, violation := range result.Violations {
				out = append(out, *violation.Message)
			}
			return out
		}
		root := copyFixture("hcl-tables")
		Expect(messages(root)).To(ConsistOf(
			"script reads declared table acmeorders", "script writes declared table acmecustomers",
			"acmeorders.CustomerGUID is named like a GUID but is a character column",
		), "OrderGUID and the vendor CustomerGUID are uniqueidentifier, and Name is not GUID-named")
		Expect(os.RemoveAll(filepath.Join(root, "schema"))).To(Succeed())
		Expect(messages(root)).To(BeEmpty(), "without the declarations the same accesses name undeclared tables")
	})

	DescribeTable("refuses what the HCL producer cannot publish",
		func(source, message string) {
			root := GinkgoT().TempDir()
			writeTree(root, map[string]string{".sqlglot-go.yaml": "version: 1\nmappings:\n  - match: 'schema/*.hcl'\n", "schema/a.hcl": source})
			sources, err := readMappedSources(root, []SourceMapping{{Project: ".sqlglot-go.yaml", Dialect: "tsql"}})
			Expect(err).NotTo(HaveOccurred())
			_, err = hclDocument(sources[0])
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("tables in several schemas",
			"schema \"dbo\" {}\nschema \"audit\" {}\ntable \"a\" {\n  schema = schema.dbo\n  column \"id\" {\n    type = int\n  }\n}\ntable \"b\" {\n  schema = schema.audit\n  column \"id\" {\n    type = int\n  }\n}\n",
			"a source file declares one schema"),
		Entry("a view, which the SQL Server HCL does not manage", "schema \"dbo\" {}\nview \"v\" {\n  schema = schema.dbo\n  as     = \"SELECT 1\"\n}\n",
			"HCL view blocks are not supported"),
		Entry("invalid HCL", "table {", "evaluate sqlserver HCL"),
	)

	It("keeps a table's partial = true on its published declaration, and a complete table is not partial", func() {
		root := GinkgoT().TempDir()
		table := func(name, attributes string) string {
			return "table \"" + name + "\" {\n  schema = schema.dbo\n" + attributes + "  column \"id\" {\n    type = int\n  }\n}\n"
		}
		writeTree(root, map[string]string{
			".sqlglot-go.yaml": "version: 1\nmappings:\n  - match: 'schema/*.hcl'\n",
			"schema/a.hcl":     "schema \"dbo\" {}\n" + table("vendor", "  partial = true\n") + table("tenant", ""),
		})
		sources, err := readMappedSources(root, []SourceMapping{{Project: ".sqlglot-go.yaml", Dialect: "tsql"}})
		Expect(err).NotTo(HaveOccurred())
		document, err := hclDocument(sources[0])
		Expect(err).NotTo(HaveOccurred())
		partial := map[string]bool{}
		for _, node := range document.Nodes {
			partial[node.(uir.RecordTable).Type] = node.(uir.RecordTable).Partial
		}
		Expect(partial).To(Equal(map[string]bool{"vendor": true, "tenant": false}))
	})

	It("accepts a default without a constraint name, which a declaration may leave to the server", func() {
		root := GinkgoT().TempDir()
		writeTree(root, map[string]string{
			".sqlglot-go.yaml": "version: 1\nmappings:\n  - match: 'schema/*.hcl'\n",
			"schema/a.hcl":     "schema \"dbo\" {}\ntable \"a\" {\n  schema = schema.dbo\n  column \"status\" {\n    type    = nvarchar(20)\n    default = \"open\"\n  }\n}\n",
		})
		sources, err := readMappedSources(root, []SourceMapping{{Project: ".sqlglot-go.yaml", Dialect: "tsql"}})
		Expect(err).NotTo(HaveOccurred())
		document, err := hclDocument(sources[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(document.PackagePath).To(Equal("dbo"))
		Expect(document.Nodes).To(HaveLen(1))
	})
})
