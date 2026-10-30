package indexer

import (
	"encoding/json"
	"path/filepath"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Analysis build tags", func() {
	DescribeTable("selects tagged sources and isolates cached analysis", func(ctx SpecContext, mode storage.AnalysisMode) {
		root := canonicalTempDir()
		writeFile(filepath.Join(root, "go.mod"), "module example.org/tagged\n\ngo 1.26\n")
		writeFile(filepath.Join(root, "plain.go"), "package tagged\nfunc Plain() {}\n")
		writeFile(filepath.Join(root, "feature.go"), "//go:build feature\n\npackage tagged\nfunc Feature() {}\n")
		database := openIndexerSQLite(ctx)
		engine, err := New(database)
		Expect(err).NotTo(HaveOccurred())
		options := AnalysisOptions{Directory: root, Mode: mode}
		plain, err := engine.Analyze(ctx, options)
		Expect(err).NotTo(HaveOccurred())
		options.Tags, options.NoIndex = []string{"feature"}, true
		_, err = engine.Analyze(ctx, options)
		Expect(err).To(MatchError(ContainSubstring("missing or stale")))
		options.NoIndex = false
		tagged, err := engine.Analyze(ctx, options)
		Expect(err).NotTo(HaveOccurred())
		Expect(tagged.SnapshotID).NotTo(Equal(plain.SnapshotID))
		documents, err := storage.ActiveDocuments(ctx, database, uuid.MustParse(tagged.SnapshotID), storage.ActiveDocumentOptions{Content: true})
		Expect(err).NotTo(HaveOccurred())
		var content storage.DocumentContent
		Expect(json.Unmarshal(documents["feature.go"].Document.Content, &content)).To(Succeed())
		Expect(content.Symbols).To(ContainElement(HaveField("Identifier.Method", "Feature")))
		options.NoIndex = true
		reused, err := engine.Analyze(ctx, options)
		Expect(err).NotTo(HaveOccurred())
		Expect(reused.SnapshotID).To(Equal(tagged.SnapshotID))
	}, Entry("syntax", storage.AnalysisSyntax), Entry("typed", storage.AnalysisTyped))
})
