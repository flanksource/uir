package indexer

import (
	"os"
	"path/filepath"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	gapsModule     = "example.org/gaps"
	gapsMissing    = gapsModule + "/missing"
	gapsPresent    = gapsModule + "/present"
	gapsUses       = gapsModule + "/uses"
	gapsUsesSource = "package uses\n\n" +
		"import (\n" +
		"\t\"" + gapsMissing + "\"\n" +
		"\t\"" + gapsPresent + "\"\n" +
		")\n\n" +
		"func Run() int {\n" +
		"\tmissing.Record()\n" +
		"\treturn present.Value()\n" +
		"}\n"
)

// noteTexts maps each occurrence's source text to the note it carries, for occurrences without a symbol.
func noteTexts(source string, content storage.DocumentContent) map[string][]string {
	notes := map[string][]string{}
	for _, occurrence := range content.Occurrences {
		if occurrence.Symbol == nil {
			text := source[occurrence.Bytes[0]:occurrence.Bytes[1]]
			notes[text] = append(notes[text], occurrence.Note)
		}
	}
	return notes
}

var _ = Describe("unresolved imports", func() {
	DescribeTable("index a package importing an empty directory as partial with the import unresolved, and the rest of the module normally",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			for _, directory := range []string{"missing", "present", "uses"} {
				Expect(os.MkdirAll(filepath.Join(workspace, directory), 0o755)).To(Succeed())
			}
			writeFile(filepath.Join(workspace, "go.mod"), "module "+gapsModule+"\n\ngo 1.26\n")
			writeFile(filepath.Join(workspace, "present", "present.go"), "package present\n\nfunc Value() int { return 1 }\n")
			writeFile(filepath.Join(workspace, "uses", "uses.go"), gapsUsesSource)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())

			snapshot := loadTypedSnapshot(ctx, database, indexOnce(ctx, engine, workspace).SnapshotID)

			uses := snapshot.packageRow(database, gapsUses)
			Expect(uses.Coverage).To(Equal(storage.CoveragePartial))
			Expect(uses.Diagnostics.String()).To(ContainSubstring("could not import " + gapsMissing))
			Expect(noteTexts(gapsUsesSource, snapshot.contents["uses/uses.go"])).To(HaveKeyWithValue(`"`+gapsMissing+`"`, []string{noteUnresolvedImport}))
			Expect(noteTexts(gapsUsesSource, snapshot.contents["uses/uses.go"])).To(HaveKeyWithValue("missing", []string{noteUnresolvedImport}))

			Expect(snapshot.packageRow(database, gapsPresent).Coverage).To(Equal(storage.CoverageIndexed))
			value := findSymbol(database, gapsPresent, "func", "Value")
			Expect(snapshot.postings(database, value.ID)).To(ConsistOf("present/present.go definition 1", "present/present.go reference 1", "uses/uses.go reference 1"))
			Expect(snapshot.entry("uses/uses.go", findSymbol(database, gapsUses, "func", "Run").ID).Shape).To(Equal("func Run() int"))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)
})
