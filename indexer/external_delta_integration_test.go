package indexer

import (
	"strings"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func deltaFixture(commit string, bodies map[string]string) Publication {
	publication := Publication{RootKey: "example.org/catalog", Name: "Catalog", Revision: commit,
		Location: ExternalLocation{URI: "oipa://example/catalog"},
		Options:  PublishOptions{Historical: true, GitCommit: strings.Repeat(commit, 40), Producer: "catalog-v1"}}
	for name, source := range bodies {
		pkg := publication.RootKey + "/" + strings.Split(name, "/")[0]
		publication.Documents = append(publication.Documents, PublishedDocument{PathKey: name, PackagePath: pkg,
			Source: source, ContentHash: hashBytes([]byte(source)),
			Content: storage.DocumentContent{Version: storage.DocumentFormatVersion, PackagePath: pkg,
				Symbols: []storage.DocumentSymbol{}, Occurrences: []storage.DocumentOccurrence{}, Diagnostics: []storage.DocumentDiagnostic{}}})
	}
	return publication
}

var _ = Describe("external delta publication", func() {
	It("retains declarations and references from unchanged packages", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		base := prodPublication("first", "100")
		base.Options = PublishOptions{Historical: true, GitCommit: strings.Repeat("a", 40), Producer: "catalog-v1"}
		first := publish(ctx, database, base)
		next := prodPublication("next", "200")
		next.Options = PublishOptions{Historical: true, GitCommit: strings.Repeat("b", 40), Producer: "catalog-v1", BaseSnapshotID: first.SnapshotID}
		next.Documents = next.Documents[1:]
		next.Delta = &PublicationDelta{}
		result := publish(ctx, database, next)
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		page, err := pipeline.RunModules(ctx, "field:Amount", query.ModuleScopeOptions{SnapshotID: result.SnapshotID})
		Expect(err).NotTo(HaveOccurred())
		Expect(page.Total).To(Equal(1))
		uses, err := pipeline.RunModules(ctx, "field:Amount <", query.ModuleScopeOptions{SnapshotID: result.SnapshotID})
		Expect(err).NotTo(HaveOccurred())
		Expect(uses.Total).To(Equal(1))
		Expect(uses.Matches[0].Path).To(Equal("rules/Calc.xml"))
	})

	It("permits explicit deletion of every source", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		first := publish(ctx, database, deltaFixture("a", map[string]string{"removed/body.xml": "<Removed/>"}))
		publication := deltaFixture("b", nil)
		publication.Options.BaseSnapshotID = first.SnapshotID
		publication.Delta = &PublicationDelta{DeletedPaths: []string{"removed/body.xml"}}
		result := publish(ctx, database, publication)
		Expect(result.Files).To(BeZero())
		active, err := storage.ActiveDocuments(ctx, database, uuid.MustParse(result.SnapshotID), storage.ActiveDocumentOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(active).To(BeEmpty())
	})

	It("reuses unchanged documents and source metrics while replacing and deleting selected packages", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		first := publish(ctx, database, deltaFixture("a", map[string]string{"keep/body.xml": "<Keep/>\n", "change/body.xml": "<Before/>\n", "drop/body.xml": "<Drop/>\n"}))
		base, err := storage.ActiveDocuments(ctx, database, uuid.MustParse(first.SnapshotID), storage.ActiveDocumentOptions{})
		Expect(err).NotTo(HaveOccurred())
		publication := deltaFixture("b", map[string]string{"change/body.xml": "<After/>\n", "added/body.xml": "<Added/>\n"})
		publication.Options.BaseSnapshotID = first.SnapshotID
		publication.Delta = &PublicationDelta{DeletedPaths: []string{"drop/body.xml"}}
		result := publish(ctx, database, publication)
		Expect(result.Files).To(Equal(3))
		Expect(result.ReusedFiles).To(BeNumerically(">=", 1))
		current, err := storage.ActiveDocuments(ctx, database, uuid.MustParse(result.SnapshotID), storage.ActiveDocumentOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(current).To(HaveKey("keep/body.xml"))
		Expect(current["keep/body.xml"]).To(Equal(base["keep/body.xml"]))
		Expect(current).NotTo(HaveKey("drop/body.xml"))
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		contents, err := pipeline.ReadModuleSources(ctx, query.ModuleSourcesOptions{SnapshotID: result.SnapshotID, Paths: []string{"keep/body.xml", "change/body.xml", "added/body.xml"}})
		Expect(err).NotTo(HaveOccurred())
		Expect([]string{contents[0].Content, contents[1].Content, contents[2].Content}).To(Equal([]string{"<Keep/>\n", "<After/>\n", "<Added/>\n"}))
		old, err := pipeline.ReadModuleSource(ctx, first.SnapshotID, "drop/body.xml")
		Expect(err).NotTo(HaveOccurred())
		Expect(old.Content).To(Equal("<Drop/>\n"))
		var heads int64
		Expect(database.Model(&storage.ModuleLocationHead{}).Count(&heads).Error).To(Succeed())
		Expect(heads).To(BeZero())
	})

	DescribeTable("rejects invalid delta input without publishing", func(ctx SpecContext, change func(*Publication)) {
		database := openIndexerSQLite(ctx)
		first := publish(ctx, database, deltaFixture("a", map[string]string{"pair/one.xml": "<One/>", "pair/two.xml": "<Two/>"}))
		publication := deltaFixture("b", map[string]string{"pair/one.xml": "<Changed/>"})
		publication.Options.BaseSnapshotID = first.SnapshotID
		publication.Delta = &PublicationDelta{DeletedPaths: []string{"pair/two.xml"}}
		change(&publication)
		before := rowCounts(database, "snapshots", "documents", "source_revisions")
		_, err := Publish(ctx, database, publication)
		Expect(err).To(MatchError(ErrInvalidPublication))
		Expect(rowCounts(database, "snapshots", "documents", "source_revisions")).To(Equal(before))
	},
		Entry("missing base", func(publication *Publication) { publication.Options.BaseSnapshotID = "" }),
		Entry("changed producer", func(publication *Publication) { publication.Options.Producer = "catalog-v2" }),
		Entry("different root", func(publication *Publication) { publication.RootKey = "example.org/other" }),
		Entry("different repository", func(publication *Publication) { publication.Location.RepositoryURI = "/other/repository.git" }),
		Entry("partial package", func(publication *Publication) { publication.Delta.DeletedPaths = nil }),
		Entry("unknown deletion", func(publication *Publication) { publication.Delta.DeletedPaths = []string{"pair/missing.xml"} }),
		Entry("set and delete one path", func(publication *Publication) {
			publication.Delta.DeletedPaths = []string{"pair/one.xml", "pair/two.xml"}
		}),
	)
})
