package indexer

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/flanksource/gavel/models"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"gorm.io/gorm"
)

const (
	degradedShop = "example.org/shop"
	// duplicateTotal declares one method twice: a type error, and two declarations that share one
	// canonical id, so the file's typed document cannot be rendered.
	duplicateTotal = "package shop\n\nfunc Checkout(c *Cart) int { return len(c.Items) }\n\nfunc (c *Cart) Total() int { return 1 }\n\nfunc (c *Cart) Total() int { return 2 }\n"
)

// indexWarning matches the one lint-shaped warning of a file published degraded.
func indexWarning(file, message string) types.GomegaMatcher {
	return MatchAllFields(Fields{
		"File": Equal(file), "Line": BeZero(), "Column": BeZero(), "Message": PointTo(MatchRegexp(message)),
		"Source": Equal("uir"), "Rule": PointTo(Equal(models.Rule{Type: models.RuleTypeDeny, Package: "uir", Method: RuleIndex})),
		"Severity": Equal(models.SeverityWarning), "Fixable": BeFalse(), "FixApplicability": BeEmpty(), "Code": BeNil(), "CreatedAt": BeZero(),
	})
}

// extractShop discovers and extracts the shop module at workspace without publishing it.
func extractShop(ctx context.Context, engine *Indexer, workspace string) ([]discoveredRoot, []moduleExtraction, ModuleOptions) {
	GinkgoHelper()
	options := ModuleOptions{Path: workspace, Reason: storage.ReasonAdd}
	roots, err := discoverRequestedModules(ctx, options)
	Expect(err).ToNot(HaveOccurred())
	_, _, err = readModuleDependencies(ctx, roots, false)
	Expect(err).ToNot(HaveOccurred())
	extractions, err := engine.extractModules(ctx, roots, options)
	Expect(err).ToNot(HaveOccurred())
	return roots, extractions, options
}

// repeatFirstSymbol gives the document at path a second entry under its first symbol's key.
func repeatFirstSymbol(extraction moduleExtraction, path string) {
	GinkgoHelper()
	document := extraction.documents[path]
	var content storage.DocumentContent
	Expect(json.Unmarshal(document.content, &content)).To(Succeed())
	content.Symbols = append(content.Symbols, content.Symbols[0])
	document.content, document.symbolCount = mustJSON(content), len(content.Symbols)
	extraction.documents[path] = document
}

// warningRules is the rule of each warning a module result carries, in order; nil for none.
func warningRules(result ModuleResult) []string {
	var rules []string
	for _, warning := range result.Warnings {
		rules = append(rules, warning.Rule.Method)
	}
	return rules
}

func shopCoverage(database *gorm.DB, snapshotID string) map[string]storage.Coverage {
	GinkgoHelper()
	var rows []storage.PackageCoverage
	Expect(database.Where("snapshot_id = ?", snapshotID).Find(&rows).Error).To(Succeed())
	coverage := map[string]storage.Coverage{}
	for _, row := range rows {
		coverage[row.PackagePath] = row.Coverage
	}
	return coverage
}

var _ = Describe("per-file indexing failures", func() {
	DescribeTable("publish a partial snapshot with a uir/index warning for a file whose document cannot be extracted",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := canonicalTempDir()
			writeTwoPackageModule(workspace)
			writeFile(filepath.Join(workspace, "checkout.go"), duplicateTotal)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())

			first := indexOnce(ctx, engine, workspace)
			Expect(first.Warnings).To(HaveExactElements(
				MatchFields(IgnoreExtras, Fields{"File": Equal("checkout.go"), "Line": Equal(7), "Rule": PointTo(HaveField("Method", RuleCoverage))}),
				indexWarning("checkout.go", `^describe symbols of "checkout.go": Total and another declaration share symbol id [0-9a-f]{64} .*; `+
					`indexed as an empty document, since its syntax document failed too: validate extracted document: .*symbol 2: key is already declared by symbol 1 .*$`),
			))
			Expect(shopCoverage(database, first.SnapshotID)).To(Equal(map[string]storage.Coverage{degradedShop: storage.CoveragePartial, degradedShop + "/tax": storage.CoverageIndexed}))
			snapshot := loadTypedSnapshot(ctx, database, first.SnapshotID)
			Expect(snapshot.documents["checkout.go"].Coverage).To(Equal(storage.CoverageSyntax))
			Expect(snapshot.documents["cart.go"].Coverage).To(Equal(storage.CoveragePartial))
			cart := findSymbol(database, degradedShop, "type", "Cart")
			Expect(snapshot.entry("cart.go", cart.ID).Name).To(Equal(storage.Range{3, 6, 3, 10}))
			Expect(snapshot.postings(database, cart.ID)).To(ContainElement("cart.go definition 1"))

			again := indexOnce(ctx, engine, workspace)
			Expect(again.Unchanged).To(BeTrue())
			Expect(again.Warnings).To(Equal(first.Warnings))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("index the rest of a package from syntax when one of its files does not parse",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := canonicalTempDir()
			writeTwoPackageModule(workspace)
			writeFile(filepath.Join(workspace, "broken.go"), "package shop\n\nfunc Broken() {\n")
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())

			first := indexOnce(ctx, engine, workspace)
			Expect(first.Warnings).To(HaveExactElements(
				MatchFields(IgnoreExtras, Fields{"File": Equal("broken.go"), "Line": Equal(3), "Message": PointTo(Equal("expected ';', found 'EOF'")), "Rule": PointTo(HaveField("Method", RuleCoverage))}),
				MatchFields(IgnoreExtras, Fields{"File": Equal("broken.go"), "Line": Equal(3), "Message": PointTo(Equal("expected '}', found 'EOF'")), "Rule": PointTo(HaveField("Method", RuleCoverage))}),
				indexWarning("broken.go", `^parse Go source "broken.go": broken.go:3:17: expected ';', found 'EOF' \(and 1 more errors\); indexed as an empty document$`),
			))
			Expect(shopCoverage(database, first.SnapshotID)).To(Equal(map[string]storage.Coverage{degradedShop: storage.CoverageSyntax, degradedShop + "/tax": storage.CoverageIndexed}))
			snapshot := loadTypedSnapshot(ctx, database, first.SnapshotID)
			Expect([]storage.Coverage{snapshot.documents["broken.go"].Coverage, snapshot.documents["cart.go"].Coverage}).To(Equal([]storage.Coverage{storage.CoverageSyntax, storage.CoverageSyntax}))
			var cart storage.DocumentContent
			Expect(json.Unmarshal(snapshot.documents["cart.go"].Content, &cart)).To(Succeed())
			Expect(cart.Symbols).To(ContainElement(And(HaveField("Key", ContainSubstring(`"Cart"`)), HaveField("Name", Equal(storage.Range{3, 6, 3, 10})))),
				"the parsing file of the package keeps its syntax symbols")

			again := indexOnce(ctx, engine, workspace)
			Expect(again.Unchanged).To(BeTrue())
			Expect(again.Warnings).To(Equal(first.Warnings))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("substitute a degraded document before any write for a rendered document that fails publication checks",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := canonicalTempDir()
			writeTwoPackageModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			roots, extractions, moduleOptions := extractShop(ctx, engine, workspace)
			repeatFirstSymbol(extractions[0], "cart.go")

			results, err := engine.publishModules(ctx, roots, extractions, moduleOptions)
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Warnings).To(HaveExactElements(
				indexWarning("cart.go", `^validate extracted document: .*symbol 3: key is already declared by symbol 0 .*; indexed from syntax only$`),
			))
			Expect(shopCoverage(database, results[0].SnapshotID)).To(Equal(map[string]storage.Coverage{degradedShop: storage.CoveragePartial, degradedShop + "/tax": storage.CoverageIndexed}))
			snapshot := loadTypedSnapshot(ctx, database, results[0].SnapshotID)
			Expect(snapshot.documents["cart.go"].Coverage).To(Equal(storage.CoverageSyntax))
			Expect(countRows(database, &storage.Document{}, "path_key = ?", "cart.go")).To(BeEquivalentTo(1))
			checkout := findSymbol(database, degradedShop, "func", "Checkout")
			Expect(snapshot.postings(database, checkout.ID)).To(ContainElement("checkout.go definition 1"))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	It("still fails the module for a failure that is not confined to one document", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		writeTwoPackageModule(workspace)
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		roots, extractions, moduleOptions := extractShop(ctx, engine, workspace)
		delete(extractions[0].documents, "cart.go")

		_, err = engine.publishModules(ctx, roots, extractions, moduleOptions)
		Expect(err).To(MatchError(ContainSubstring(`no document was extracted for "cart.go"`)))
		Expect(asDocumentError(err)).To(BeNil())
		Expect(countRows(database, &storage.ModuleSnapshot{}, "1 = 1")).To(BeZero())
	})
})
