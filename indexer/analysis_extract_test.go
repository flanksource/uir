package indexer

import (
	"encoding/json"
	"path/filepath"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Syntax analysis build selection", func() {
	It("keeps external test identities distinct while grouping coverage with their source package", func() {
		directory := GinkgoT().TempDir()
		source := []byte("package work_test\nfunc Work() {}\n")
		path := filepath.Join(directory, "work_test.go")
		writeFile(path, string(source))
		root := discoveredRoot{LocalPath: directory, Variant: buildVariant{GOOS: "linux", GOARCH: "amd64", CGOEnabled: "0"},
			Files: []discoveredFile{{AbsolutePath: path, PathKey: "work_test.go", PackagePath: "example.org/work", Content: source}}}
		extraction, err := extractSyntaxAnalysis(root)
		Expect(err).NotTo(HaveOccurred())
		var content storage.DocumentContent
		Expect(json.Unmarshal(extraction.documents["work_test.go"].content, &content)).To(Succeed())
		Expect(content.PackagePath).To(Equal("example.org/work_test"))
		Expect(content.Symbols).To(ConsistOf(HaveField("Identifier.Package", "example.org/work_test")))
		Expect(extraction.packages).To(HaveLen(1))
		Expect(extraction.packages[0].path).To(Equal("example.org/work"))
	})

	DescribeTable("honors build constraints without loading packages", func(source, cgo string, expected storage.Coverage) {
		directory := GinkgoT().TempDir()
		path := filepath.Join(directory, "work.go")
		writeFile(path, source)
		root := discoveredRoot{LocalPath: directory, Variant: buildVariant{GOOS: "linux", GOARCH: "amd64", CGOEnabled: cgo},
			Files: []discoveredFile{{AbsolutePath: path, PathKey: "work.go", PackagePath: "example.org/work", Content: []byte(source)}}}
		extraction, err := extractSyntaxAnalysis(root)
		Expect(err).NotTo(HaveOccurred())
		Expect(extraction.documents["work.go"].coverage).To(Equal(expected))
		var content storage.DocumentContent
		Expect(json.Unmarshal(extraction.documents["work.go"].content, &content)).To(Succeed())
		if expected == storage.CoverageExcluded {
			Expect(content.Excluded).NotTo(BeEmpty())
			Expect(content.Symbols).To(BeEmpty())
		}
	},
		Entry("cgo disabled", "package work\nimport \"C\"\nfunc Work() {}\n", "0", storage.CoverageExcluded),
		Entry("cgo enabled", "package work\nimport \"C\"\nfunc Work() {}\n", "1", storage.CoverageSyntax),
		Entry("excluded build tag", "//go:build special\n\npackage work\nfunc Work() {}\n", "0", storage.CoverageExcluded),
		Entry("ordinary source", "package work\nfunc Work() {}\n", "0", storage.CoverageSyntax),
	)
})
