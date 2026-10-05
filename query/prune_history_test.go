package query_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"gorm.io/gorm"
)

// legacyRule is the rule Legacy, which calls Calc in the first revision of the insurance root only.
var legacyRule = indexer.Identity{ModuleKey: insRoot, PackagePath: insRules, Kind: "acme.rule", Name: "Legacy"}

const legacySource = "<Rule name=\"Legacy\">\n  <Call rule=\"Calc\"/>\n</Rule>\n"

// insuranceRevision is the insurance root at revision: Calc's source ends with the revision, so every
// revision publishes new rules documents, and the rule Legacy is there only when legacy is set.
func insuranceRevision(revision string, legacy bool) indexer.Publication {
	GinkgoHelper()
	publication := insurancePublication()
	publication.Revision = revision
	for i, document := range publication.Documents {
		if document.PathKey == calcPath {
			document.Source += "<!-- " + revision + " -->\n"
			document.ContentHash = sha256Text(document.Source)
			publication.Documents[i] = document
		}
	}
	if !legacy {
		return publication
	}
	identifiers := insuranceIdentifiers()
	identifiers["Legacy"] = uir.Identifier{Module: insRoot, Package: insRules, Method: "Legacy", NodeType: uir.NodeTypeFunction}
	source := publishedText(legacySource)
	publication.Symbols = append(publication.Symbols, indexer.PublishedSymbol{Identity: legacyRule, Visibility: "exported"})
	publication.Documents = append(publication.Documents, source.document("rules/Legacy.xml", insRules, []storage.DocumentSymbol{
		source.declaration(legacyRule, identifiers["Legacy"], strings.TrimSuffix(legacySource, "\n"), "Legacy"),
	}, []storage.DocumentOccurrence{
		source.call(insuranceIdentities().calc, legacyRule, identifiers, `<Call rule="Calc"/>`, "Calc"),
	}))
	return publication
}

func publish(ctx context.Context, database *gorm.DB, publication indexer.Publication) uuid.UUID {
	GinkgoHelper()
	result, err := indexer.Publish(ctx, database, publication)
	Expect(err).ToNot(HaveOccurred())
	return uuid.MustParse(result.SnapshotID)
}

// coProdRevision is the docs example root Co/Prod at revision.
func coProdRevision(revision string) indexer.Publication {
	GinkgoHelper()
	encoded, err := os.ReadFile(filepath.Join("..", "docs", "examples", "import", "co-prod.json"))
	Expect(err).ToNot(HaveOccurred())
	var publication indexer.Publication
	Expect(json.Unmarshal(encoded, &publication)).To(Succeed())
	publication.Revision = revision
	return publication
}

// rootRows counts, per table, the rows of one root.
func rootRows(database *gorm.DB, rootKey string) map[string]int64 {
	GinkgoHelper()
	var root storage.ModuleRoot
	Expect(database.Where("root_key = ?", rootKey).Take(&root).Error).To(Succeed())
	counts := map[string]int64{}
	count := func(table, where string, argument any) {
		var rows int64
		Expect(database.Table(table).Where(where, argument).Count(&rows).Error).To(Succeed())
		counts[table] = rows
	}
	count("snapshots", "root_id = ?", root.ID)
	count("documents", "root_id = ?", root.ID)
	count("source_revisions", "root_id = ?", root.ID)
	count("source_deltas", "root_id = ?", root.ID)
	count("symbol_postings", "root_ordinal = ?", root.Ordinal)
	count("symbol_deltas", "root_ordinal = ?", root.Ordinal)
	count("symbols", "module_key = ?", rootKey)
	return counts
}

// headState is what a root's head serves: its effective sources, symbols and active documents.
type headState struct {
	sources   map[string]uuid.UUID
	symbols   []storage.EffectiveSymbol
	documents []uuid.UUID
}

func readHead(ctx context.Context, database *gorm.DB, head uuid.UUID) headState {
	GinkgoHelper()
	sources, err := storage.EffectiveSources(ctx, database, head)
	Expect(err).ToNot(HaveOccurred())
	symbols, err := storage.EffectiveSymbols(ctx, database, head)
	Expect(err).ToNot(HaveOccurred())
	active, err := storage.ActiveDocuments(ctx, database, head, storage.ActiveDocumentOptions{})
	Expect(err).ToNot(HaveOccurred())
	state := headState{sources: map[string]uuid.UUID{}, symbols: symbols}
	for path, source := range sources {
		state.sources[path] = source.ID
	}
	for _, document := range active {
		state.documents = append(state.documents, document.Document.ID)
	}
	slices.SortFunc(state.documents, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return state
}

// servedAnswers are what the queries the specs compare return over the insurance and Co/Prod roots.
func servedAnswers(ctx context.Context, pipeline *query.Pipeline) []any {
	GinkgoHelper()
	insurance, coProd := query.ModuleScopeOptions{RootKey: insRoot}, query.ModuleScopeOptions{RootKey: "Co/Prod"}
	screen := callGraph(ctx, pipeline, query.GraphOptions{
		Symbol: indexer.SymbolID(insuranceIdentities().screen), Direction: graph.DirectionCallers, Depth: 1, Access: []uir.RelationshipType{uir.RelationshipTypeCall, uir.RelationshipTypeRead}, Scope: insurance,
	})
	return []any{
		runQuery(ctx, pipeline, "func:Calc <", insurance).Matches, runQuery(ctx, pipeline, "kind:record", insurance).Matches,
		runQuery(ctx, pipeline, "func:Legacy", insurance).Matches, graphEdges(screen),
		runQuery(ctx, pipeline, "func:Calc <", coProd).Matches, runQuery(ctx, pipeline, "kind:test.screen", coProd).Matches,
	}
}

var _ = Describe("pruning a root's history", func() {
	DescribeTable("deletes the root's superseded snapshots and what only they used, and keeps what the head and other roots serve",
		func(ctx SpecContext, backend string) {
			database := openQueryDatabase(ctx, backend)
			publish(ctx, database, coProdRevision("b1"))
			publish(ctx, database, coProdRevision("b2"))
			publish(ctx, database, insuranceRevision("r1", true))
			publish(ctx, database, insuranceRevision("r2", false))
			head := publish(ctx, database, insuranceRevision("r3", false))
			cached, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())
			served, headBefore := servedAnswers(ctx, cached), readHead(ctx, database, head)
			insuranceBefore, coProdBefore := rootRows(database, insRoot), rootRows(database, "Co/Prod")
			var legacy int64
			Expect(database.Model(&storage.Symbol{}).Where("id = ?", indexer.SymbolID(legacyRule)).Count(&legacy).Error).To(Succeed())
			Expect(legacy).To(Equal(int64(1)), "the first revision published Legacy")

			result, err := storage.PruneHistory(ctx, database, insRoot, 1)
			Expect(err).ToNot(HaveOccurred())
			Expect([]any{result.RootKey, result.Kept, result.Rebased, result.Snapshots}).To(Equal([]any{insRoot, 1, 1, int64(2)}))

			Expect(readHead(ctx, database, head)).To(Equal(headBefore), "the head serves the same sources, symbols and documents")
			after := rootRows(database, insRoot)
			Expect(after["snapshots"]).To(Equal(int64(1)))
			Expect(after["documents"]).To(Equal(int64(len(headBefore.documents))), "only the head's documents are left")
			Expect(after["source_revisions"]).To(Equal(int64(len(headBefore.sources))), "only the head's source revisions are left")
			Expect(after["source_deltas"]).To(Equal(int64(len(headBefore.sources))), "the head, rebased, sets every path itself")
			Expect(after["symbol_deltas"]).To(Equal(int64(len(headBefore.symbols))), "the head, rebased, sets every symbol itself")
			Expect([]int64{result.Documents, result.SourceRevisions, result.Symbols}).To(Equal([]int64{
				insuranceBefore["documents"] - after["documents"], insuranceBefore["source_revisions"] - after["source_revisions"],
				insuranceBefore["symbols"] - after["symbols"],
			}))
			Expect(result.Postings).To(Equal(insuranceBefore["symbol_postings"] - after["symbol_postings"]))
			Expect(result.Postings).To(BeNumerically(">", 0))
			Expect(result.SourceBlobs).To(BeNumerically(">", 0), "the earlier Calc and Legacy sources are gone")
			Expect(database.Model(&storage.Symbol{}).Where("id = ?", indexer.SymbolID(legacyRule)).Count(&legacy).Error).To(Succeed())
			Expect(legacy).To(BeZero(), "Legacy, which no remaining document names, is deleted")
			Expect(rootRows(database, "Co/Prod")).To(Equal(coProdBefore), "another root keeps all of its history")
			var unbacked int64
			Expect(database.Table("source_revisions").Where("content_hash NOT IN (SELECT content_hash FROM source_blobs)").Count(&unbacked).Error).To(Succeed())
			Expect(unbacked).To(BeZero(), "every remaining source revision keeps its stored bytes")

			fresh, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())
			Expect(servedAnswers(ctx, fresh)).To(Equal(served), "a new pipeline answers as before")
			Expect(servedAnswers(ctx, cached)).To(Equal(served), "a pipeline that read the pruned history answers as before")

			again, err := storage.PruneHistory(ctx, database, insRoot, 1)
			Expect(err).ToNot(HaveOccurred())
			Expect(again).To(Equal(storage.PruneResult{RootKey: insRoot, Kept: 1}), "a pruned root has nothing more to prune")
		},
		Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"),
	)

	DescribeTable("keeps a pipeline's queries right when a pruned symbol is published again under another handle",
		func(ctx SpecContext, backend string) {
			database := openQueryDatabase(ctx, backend)
			scope := query.ModuleScopeOptions{RootKey: insRoot}
			publish(ctx, database, insuranceRevision("r1", true))
			pipeline, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())
			Expect(runQuery(ctx, pipeline, "func:Legacy", scope).Matches).To(HaveLen(1))
			handles, err := storage.SymbolHandles(ctx, database, []string{indexer.SymbolID(legacyRule)})
			Expect(err).ToNot(HaveOccurred())

			publish(ctx, database, insuranceRevision("r2", false))
			_, err = storage.PruneHistory(ctx, database, insRoot, 1)
			Expect(err).ToNot(HaveOccurred())
			publish(ctx, database, insuranceRevision("r3", true))
			republished, err := storage.SymbolHandles(ctx, database, []string{indexer.SymbolID(legacyRule)})
			Expect(err).ToNot(HaveOccurred())
			Expect(republished).ToNot(Equal(handles), "Legacy came back under another handle")

			legacy := runQuery(ctx, pipeline, "func:Legacy", scope).Matches
			Expect(legacy).To(HaveLen(1))
			Expect(legacy[0].SymbolID).To(Equal(indexer.SymbolID(legacyRule)))
		},
		Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"),
	)

	DescribeTable("keeps the head of every checkout of a Go root, however small keep is",
		func(ctx SpecContext, backend string) {
			database := openQueryDatabase(ctx, backend)
			primary, branch := shopWorkspace()
			first := indexCheckout(ctx, database, primary)
			indexCheckout(ctx, database, branch)
			Expect(os.WriteFile(filepath.Join(primary, "app", "app.go"), []byte(shopRun), 0o644)).To(Succeed())
			indexCheckout(ctx, database, primary)
			pipeline, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())
			checkouts := func() []any {
				return []any{
					runQuery(ctx, pipeline, "store.Store.Save <", query.ModuleScopeOptions{RootKey: shopModule, Location: primary}).Matches,
					runQuery(ctx, pipeline, "store.Store.Save <", query.ModuleScopeOptions{RootKey: shopModule, Location: branch}).Matches,
					runQuery(ctx, pipeline, "app.Run >", query.ModuleScopeOptions{RootKey: shopModule, Location: branch}).Matches,
				}
			}
			served := checkouts()

			result, err := storage.PruneHistory(ctx, database, shopModule, 1)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(MatchFields(IgnoreExtras, Fields{"Kept": Equal(2), "Rebased": Equal(2), "Snapshots": Equal(int64(1))}),
				"the primary's first snapshot goes; both heads, each based on it, stay and are rebased")
			Expect(checkouts()).To(Equal(served))
			_, err = pipeline.RunModules(ctx, "store.Store.Save <", query.ModuleScopeOptions{SnapshotID: first.SnapshotID})
			Expect(err).To(HaveOccurred(), "the pruned snapshot is gone")
		},
		Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"),
	)

	DescribeTable("refuses a keep below one and a root it does not know",
		func(ctx SpecContext, rootKey string, keep int, message string) {
			database := openQueryDatabase(ctx, "sqlite")
			publish(ctx, database, insuranceRevision("r1", false))
			_, err := storage.PruneHistory(ctx, database, rootKey, keep)
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("keep 0", insRoot, 0, "keep 0: at least the newest snapshot must be kept"),
		Entry("an unknown root", "Acme/Missing", 1, `module root "Acme/Missing" is not registered`),
	)
})
