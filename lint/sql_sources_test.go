package lint

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing/fstest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL source mappings", func() {
	It("tracks the descriptor and SQL body independently", func() {
		source, err := (SourceMapping{
			Dialect: "tsql",
			Transform: `{"content": read(xmlpath(file.content, "/root/entry[@name='SQLDATA']/@file")),
				"filename": file.path.trimSuffix("data.xml") + "script.sql", "package": "migrations"}`,
		}).resolve(fstest.MapFS{
			"scripts/data.xml":    &fstest.MapFile{Data: []byte(`<root><entry name="SQLDATA" file="SQLData.xml"/></root>`)},
			"scripts/SQLData.xml": &fstest.MapFile{Data: []byte("SELECT N'café';\r\nGO\r\n"), Mode: fs.FileMode(0o600)},
		}, "scripts/data.xml", map[string]string{})
		Expect(err).NotTo(HaveOccurred())
		Expect(source.Origin).To(Equal("scripts/SQLData.xml"))
		Expect(source.Content).To(Equal("SELECT N'café';\r\nGO\r\n"))
		Expect(source.Inputs).To(HaveExactElements("scripts/SQLData.xml", "scripts/data.xml"))
	})

	It("publishes script metadata and reports native SQL rules against the input file", func(ctx SpecContext) {
		root := GinkgoT().TempDir()
		config := filepath.Join(root, ".uir.yaml")
		Expect(os.WriteFile(config, []byte(`version: 1
sources:
  - match_regex: '^scripts/(?P<phase>Pre|Post)Deployment_(?P<name>[^/]+?(?:_(?P<countries>(?:BW|MW|ZW|UG|SZ|KE)(?:_(?:BW|MW|ZW|UG|SZ|KE))*))?)/SQLData\.xml$'
    dialect: tsql
    transform: >-
      {"content": file.content, "filename": file.path.trimSuffix(".xml") + ".sql", "package": "migrations",
      "metadata": {"name": captures.name, "phase": captures.phase.lower(),
      "labels": captures.phase == "Post" && captures.countries != "" ? {"country": captures.countries.split("_")} : {}}}
lint:
  rules:
    - id: go-parameter-count
      query: SELECT symbol.name AS name FROM func:* WHERE symbol.metrics.param_count > 3
      message: '{{.name}} has too many parameters'
    - id: no-ledger-writes
      query: SELECT symbol.name AS name FROM writes(table:ledger) WHERE symbol.package = 'finance'
      message: 'Blocked write to {{.name}}'
    - id: post-script
      query: SELECT symbol.name AS name FROM sql.script:* WHERE symbol.props.phase = 'post'
      message: '{{.name}}'
`), 0o600)).To(Succeed())
		file := filepath.Join(root, "scripts", "PostDeployment_Seed_BW_SZ", "SQLData.xml")
		Expect(os.MkdirAll(filepath.Dir(file), 0o700)).To(Succeed())
		Expect(os.WriteFile(file, []byte("-- migration\r\n  DELETE FROM finance.ledger;\r\nGO\r\n"), 0o600)).To(Succeed())
		request, err := Prepare(ctx, root, config, Options{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(request.Release)
		result := request.Run(ctx, lintDatabase(ctx, "sqlite"))
		Expect(result.Error).To(BeEmpty())
		Expect(result.Violations).To(HaveLen(2))
		Expect(result.Violations[0].File).To(Equal(file))
		Expect(result.Violations[0].Line).To(Equal(2))
		Expect(result.Violations[0].Column).To(Equal(3))
		Expect(result.Violations[1].Message).To(HaveValue(Equal("Seed_BW_SZ")))
	})
})
