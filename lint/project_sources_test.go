package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func copyFixture(name string) string {
	GinkgoHelper()
	root := GinkgoT().TempDir()
	Expect(os.CopyFS(root, os.DirFS(filepath.Join("testdata", name)))).To(Succeed())
	return root
}

func writeTree(root string, files map[string]string) {
	GinkgoHelper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		Expect(os.MkdirAll(filepath.Dir(path), 0o700)).To(Succeed())
		Expect(os.WriteFile(path, []byte(content), 0o600)).To(Succeed())
	}
}

func loadProjectConfig(content string) error {
	GinkgoHelper()
	_, err := Load(configFile(content))
	return err
}

var _ = Describe("Sources taken from a sqlglot project", func() {
	It("maps the same sources as the equivalent hand-written mapping", func() {
		hand, project := copyFixture("sql-migrations"), copyFixture("sql-migrations-project")
		handConfig, err := Load(filepath.Join(hand, ConfigName))
		Expect(err).NotTo(HaveOccurred())
		projectConfig, err := Load(filepath.Join(project, ConfigName))
		Expect(err).NotTo(HaveOccurred())
		expected, err := readMappedSources(hand, handConfig.Sources)
		Expect(err).NotTo(HaveOccurred())
		Expect(expected).To(HaveLen(1))
		got, err := readMappedSources(project, projectConfig.Sources)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(expected))
		Expect(got[0].Metadata).To(Equal(map[string]any{"name": "Seed_BW_SZ", "phase": "post", "labels": map[string]any{"country": []any{"BW", "SZ"}}}))
	})

	It("publishes the same script, table and function symbols and findings as the hand-written mapping", func(ctx SpecContext) {
		run := func(fixture string) []string {
			root := copyFixture(fixture)
			request, err := Prepare(ctx, root, filepath.Join(root, ConfigName), Options{})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(request.Release)
			result := request.Run(ctx, lintDatabase(ctx, "sqlite"))
			Expect(result.Error).To(BeEmpty())
			var findings []string
			for _, v := range result.Violations {
				findings = append(findings, fmt.Sprintf("%s %s:%d:%d %s", v.Rule.Method, strings.TrimPrefix(v.File, root), v.Line, v.Column, *v.Message))
			}
			return findings
		}
		expected := run("sql-migrations")
		Expect(expected).To(HaveLen(3), "the ledger write, the mask_pan call and GENERATE_SERIES at compatibility level 130")
		Expect(run("sql-migrations-project")).To(Equal(expected))
	})

	It("loads every mapped source and ignores the project's default select", func() {
		root := GinkgoT().TempDir()
		writeTree(root, map[string]string{
			".sqlglot-go.yaml": "version: 1\nmappings:\n  - match: 'sql/*.sql'\n    labels: {scope: [vendor]}\nselect: ['!scope=vendor']\n",
			"sql/a.sql":        "SELECT 1;\n",
		})
		sources, err := readMappedSources(root, []SourceMapping{{Project: ".sqlglot-go.yaml", Dialect: "tsql"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(sources).To(HaveLen(1))
		Expect(sources[0].Metadata).To(Equal(map[string]any{"labels": map[string]any{"scope": []any{"vendor"}}}))
	})

	It("takes the project from a subdirectory and reports paths from the project root", func() {
		root := GinkgoT().TempDir()
		writeTree(root, map[string]string{
			"db/.sqlglot-go.yaml": "version: 1\nmappings:\n  - match: 'sql/*.sql'\n",
			"db/sql/a.sql":        "SELECT 1;\n",
		})
		sources, err := readMappedSources(root, []SourceMapping{{Project: "db/.sqlglot-go.yaml", Dialect: "tsql"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(sources).To(HaveLen(1))
		Expect([]string{sources[0].Filename, sources[0].Origin}).To(Equal([]string{"db/sql/a.sql", "db/sql/a.sql"}))
		Expect(sources[0].Inputs).To(Equal([]string{"db/sql/a.sql"}))
	})

	It("takes the companion files a transform reads as inputs, so changing one selects the source and findings land on it", func() {
		root := GinkgoT().TempDir()
		writeTree(root, map[string]string{
			".sqlglot-go.yaml":      "version: 1\nmappings:\n  - match: 'scripts/*/data.xml'\n    transform: '{\"sql\": read(xmlpath(file.content, \"/root/entry[@name=\\\"SQLDATA\\\"]/@file\")), \"name\": \"Seed\"}'\n",
			"scripts/a/data.xml":    "<root><entry name=\"SQLDATA\" file=\"SQLData.xml\"/></root>",
			"scripts/a/SQLData.xml": "SELECT 1;\n",
		})
		sources, err := readMappedSources(root, []SourceMapping{{Project: ".sqlglot-go.yaml", Dialect: "tsql"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(sources).To(HaveLen(1))
		Expect(sources[0].Inputs).To(Equal([]string{"scripts/a/data.xml", "scripts/a/SQLData.xml"}))
		Expect(sources[0].Origin).To(Equal("scripts/a/SQLData.xml"))
		Expect(sourceMatches(sources[0], root, []string{filepath.Join(root, "scripts", "a", "SQLData.xml")})).To(BeTrue())
		Expect(sourceMatches(sources[0], root, []string{filepath.Join(root, "scripts", "b")})).To(BeFalse())
	})

	It("names the missing project file", func() {
		_, err := readMappedSources(GinkgoT().TempDir(), []SourceMapping{{Project: "db/.sqlglot-go.yaml", Dialect: "tsql"}})
		Expect(err).To(MatchError(And(ContainSubstring("project db/.sqlglot-go.yaml"), ContainSubstring("no such file"))))
	})

	It("refuses a project that also maps files itself, naming each conflicting key", func() {
		err := loadProjectConfig("version: 1\nsources:\n  - project: .sqlglot-go.yaml\n    dialect: tsql\n    match: 'sql/*.sql'\n    transform: '{}'\n")
		Expect(err).To(MatchError(ContainSubstring(`project ".sqlglot-go.yaml" maps its own files, so it cannot also set match, transform`)))
	})

	It("requires a dialect beside the project", func() {
		Expect(loadProjectConfig("version: 1\nsources:\n  - project: .sqlglot-go.yaml\n")).To(MatchError(ContainSubstring("requires a dialect")))
	})

	It("refuses a file that both a project and a hand-written mapping claim", func() {
		root := GinkgoT().TempDir()
		writeTree(root, map[string]string{".sqlglot-go.yaml": "version: 1\nmappings:\n  - match: 'sql/*.sql'\n", "sql/a.sql": "SELECT 1;\n"})
		_, err := readMappedSources(root, []SourceMapping{{Project: ".sqlglot-go.yaml", Dialect: "tsql"}, {Match: "sql/*.sql", Dialect: "tsql"}})
		Expect(err).To(MatchError(ContainSubstring("sql/a.sql matches glob:sql/*.sql and project:.sqlglot-go.yaml")))
	})
})
