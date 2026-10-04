package indexer

import (
	"context"
	"slices"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func publish(ctx context.Context, database *gorm.DB, publication Publication) ModuleResult {
	GinkgoHelper()
	result, err := Publish(ctx, database, publication)
	Expect(err).ToNot(HaveOccurred())
	return result
}

// externalQuery runs a compact query over every primary head.
func externalQuery(ctx context.Context, pipeline *query.Pipeline, expression string) query.ModuleQueryResult {
	GinkgoHelper()
	result, err := pipeline.RunModules(ctx, expression, query.ModuleScopeOptions{Limit: 100})
	Expect(err).ToNot(HaveOccurred(), expression)
	return result
}

func queryNames(result query.ModuleQueryResult) []string {
	names := make([]string, len(result.Symbols))
	for i, symbol := range result.Symbols {
		names[i] = symbol.QueryName
	}
	slices.Sort(names)
	return names
}

// enclosingPaths is, per match, the document path and the id of the declaration enclosing it.
func enclosingPaths(result query.ModuleQueryResult) []string {
	rows := make([]string, len(result.Matches))
	for i, match := range result.Matches {
		rows[i] = match.Path + " " + match.Role + " in " + match.EnclosingID
	}
	slices.Sort(rows)
	return rows
}

var _ = Describe("external publication", func() {
	DescribeTable("publishes producer roots that the query pipeline answers over",
		func(ctx SpecContext, open func(context.Context) *gorm.DB) {
			database := open(ctx)
			prod := publish(ctx, database, prodPublication("r1", "100"))
			Expect(prod).To(Equal(ModuleResult{RootKey: prodRoot, Location: prodURI, SnapshotID: prod.SnapshotID, HeadVersion: 1, Files: 2, ParsedFiles: 2}))
			planA := publish(ctx, database, planAPublication())
			Expect(planA).To(Equal(ModuleResult{RootKey: planARoot, Location: planAURI, SnapshotID: planA.SnapshotID, HeadVersion: 1, Files: 2, ParsedFiles: 2}))
			var snapshot storage.ModuleSnapshot
			Expect(database.Where("id = ?", planA.SnapshotID).First(&snapshot).Error).To(Succeed())
			Expect([]any{snapshot.Reason, snapshot.Coverage, snapshot.Revision}).To(Equal([]any{storage.ReasonImport, storage.CoverageIndexed, "r1"}))
			var location storage.ModuleLocation
			Expect(database.Where("canonical_path = ?", planAURI).First(&location).Error).To(Succeed())
			Expect(location.Kind).To(Equal(storage.LocationExternal))

			pipeline, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())
			ids := fixtureIdentities()
			apply, calc := SymbolID(ids.apply), SymbolID(ids.calc)
			Expect(queryNames(externalQuery(ctx, pipeline, "kind:test.rule"))).To(Equal([]string{"Co/Prod/PlanA/rules.Apply", "Co/Prod/rules.Calc"}))
			Expect(queryNames(externalQuery(ctx, pipeline, "field:* & mod:Co/Prod/PlanA"))).To(Equal([]string{"Co/Prod/PlanA/screens.Entry.Premium"}))
			Expect(queryNames(externalQuery(ctx, pipeline, "type:* & mod:Co/Prod/PlanA"))).To(Equal([]string{"Co/Prod/PlanA/screens.Entry"}))
			Expect(enclosingPaths(externalQuery(ctx, pipeline, "field:Policy.Amount <"))).To(Equal([]string{
				"rules/Apply.xml reference in " + apply, "rules/Calc.xml write in " + calc,
			}), "a Co/Prod field is referenced from both roots")
			Expect(enclosingPaths(externalQuery(ctx, pipeline, "field:Policy.Amount < mod:Co/Prod/PlanA"))).To(Equal([]string{"rules/Apply.xml reference in " + apply}))
			Expect(enclosingPaths(externalQuery(ctx, pipeline, "field:Entry.Premium < mod:Co/Prod/PlanA"))).To(Equal([]string{"rules/Apply.xml write in " + apply}))
			Expect(enclosingPaths(externalQuery(ctx, pipeline, "records.Policy.Amount ~w"))).To(Equal([]string{"rules/Calc.xml write in " + calc}))
			Expect(enclosingPaths(externalQuery(ctx, pipeline, "func:Calc <"))).To(Equal([]string{"rules/Apply.xml call in " + apply}), "a custom callable kind has callers")
			callees := externalQuery(ctx, pipeline, "func:Apply >")
			Expect(callees.Matches).To(HaveLen(1))
			Expect([]string{callees.Matches[0].Path, callees.Matches[0].SymbolID, callees.Matches[0].Identifier.Method}).To(Equal([]string{"rules/Apply.xml", calc, "Calc"}),
				"a custom callable kind has callees in another root")

			source, err := pipeline.ReadModuleSource(ctx, planA.SnapshotID, "rules/Apply.xml")
			Expect(err).ToNot(HaveOccurred())
			Expect([]string{source.Content, source.Origin}).To(Equal([]string{planAPublication().Documents[0].Source, "snapshot"}))
		},
		Entry("SQLite", openIndexerSQLite),
		Entry("PostgreSQL", openIndexerPostgres),
	)

	DescribeTable("republishes only a changed document and skips an unchanged publication",
		func(ctx SpecContext, open func(context.Context) *gorm.DB) {
			database := open(ctx)
			first := publish(ctx, database, prodPublication("r1", "100"))
			again := publish(ctx, database, prodPublication("r1", "100"))
			Expect(again).To(Equal(ModuleResult{RootKey: prodRoot, Location: prodURI, SnapshotID: first.SnapshotID, HeadVersion: 1, Files: 2, ReusedFiles: 2, Unchanged: true}))

			changed := publish(ctx, database, prodPublication("r2", "200"))
			Expect(changed).To(Equal(ModuleResult{RootKey: prodRoot, Location: prodURI, SnapshotID: changed.SnapshotID, HeadVersion: 2, Files: 2, ParsedFiles: 1, ReusedFiles: 1}))
			Expect(operations(symbolDeltas(ctx, database, changed.SnapshotID))).To(Equal(map[string]storage.SourceOperation{
				SymbolID(fixtureIdentities().calc): storage.SourceSet,
			}), "only the changed rule's body differs from the base")
			var sourceDeltas []storage.SourceDelta
			Expect(database.Where("snapshot_id = ?", changed.SnapshotID).Find(&sourceDeltas).Error).To(Succeed())
			Expect(sourceDeltas).To(HaveLen(1))
			Expect(sourceDeltas[0].PathKey).To(Equal("rules/Calc.xml"))
			var documents int64
			Expect(database.Model(&storage.Document{}).Count(&documents).Error).To(Succeed())
			Expect(documents).To(Equal(int64(3)), "the unchanged record document is reused")
			var indexerVersions []string
			Expect(database.Model(&storage.Document{}).Distinct().Pluck("indexer_version", &indexerVersions).Error).To(Succeed())
			Expect(indexerVersions).To(Equal([]string{ExternalIndexerVersion}))
		},
		Entry("SQLite", openIndexerSQLite),
		Entry("PostgreSQL", openIndexerPostgres),
	)

	DescribeTable("refuses an invalid publication without writing anything",
		func(ctx SpecContext, mutate func(*Publication), message string) {
			database := openIndexerSQLite(ctx)
			publication := prodPublication("r1", "100")
			mutate(&publication)
			_, err := Publish(ctx, database, publication)
			Expect(err).To(MatchError(ErrInvalidPublication))
			Expect(err).To(MatchError(ContainSubstring(message)))
			Expect(rowCounts(database, "modules", "snapshots", "documents", "symbols")).To(HaveEach(BeZero()))
			var customKinds int64
			Expect(database.Model(&storage.SymbolKind{}).Where("code > ?", int16(symbolhandle.MaxBuiltinKind)).Count(&customKinds).Error).To(Succeed())
			Expect(customKinds).To(BeZero(), "not even the declared kinds are registered")
		},
		Entry("an empty root key", func(publication *Publication) { publication.RootKey = "" }, `publish "": invalid publication: root_key is required`),
		Entry("a location without a URI", func(publication *Publication) { publication.Location.URI = "" }, `publish "Co/Prod": invalid publication: location.uri is required`),
		Entry("an undeclared custom kind", func(publication *Publication) { publication.Kinds = publication.Kinds[1:] },
			`symbol Co/Prod/rules.Calc (`+SymbolID(fixtureIdentities().calc)+`): symbol kind "test.rule" is not registered`),
		Entry("a malformed content hash", func(publication *Publication) { publication.Documents[0].ContentHash = "abc" },
			`document "records/Policy.xml": content_hash "abc" is not a 64-character hex SHA-256`),
		Entry("a content hash of other bytes", func(publication *Publication) { publication.Documents[0].ContentHash = hashBytes([]byte("other")) },
			`document "records/Policy.xml": content_hash `+hashBytes([]byte("other"))+` is not the SHA-256 of its source`),
		Entry("a path outside the root", func(publication *Publication) { publication.Documents[0].PathKey = "../Policy.xml" },
			`document "../Policy.xml": path_key must be a relative slash-separated path inside the root`),
		Entry("an enclosing symbol declared in another document", func(publication *Publication) {
			amount := SymbolID(fixtureIdentities().amount)
			publication.Documents[1].Content.Occurrences[0].Enclosing = &amount
		}, `for "rules/Calc.xml": occurrence 0: enclosing "`+SymbolID(fixtureIdentities().amount)+`" is not declared in the document`),
		Entry("a symbol the publication does not list", func(publication *Publication) { publication.Symbols = publication.Symbols[:2] },
			`document "rules/Calc.xml": symbol `+SymbolID(fixtureIdentities().calc)+` is not a known symbol`),
		Entry("a declaration of another root", func(publication *Publication) {
			apply := SymbolID(fixtureIdentities().apply)
			publication.Symbols = append(publication.Symbols, PublishedSymbol{Identity: fixtureIdentities().apply, Visibility: "exported"})
			publication.Documents[1].Content.Symbols[0].ID = &apply
			publication.Documents[1].Content.Occurrences[0].Enclosing = &apply
		}, `document "rules/Calc.xml" declares Co/Prod/PlanA/rules.Apply of root "Co/Prod/PlanA" and package "Co/Prod/PlanA/rules"; it may declare only symbols of root "Co/Prod" and package "Co/Prod/rules"`),
	)

	It("refuses a declared kind the database registered under another category", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		_, err := storage.RegisterKind(ctx, database, "test.rule", symbolhandle.CategoryType)
		Expect(err).ToNot(HaveOccurred())
		_, err = Publish(ctx, database, prodPublication("r1", "100"))
		Expect(err).To(MatchError(ErrInvalidPublication))
		Expect(err).To(MatchError(ContainSubstring(`kind "test.rule" is registered as type, not callable`)))
	})

	It("refuses an external location in a root indexed from Go checkouts", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		checkout := canonicalTempDir()
		writeFile(checkout+"/go.mod", "module Co/Prod\n\ngo 1.26\n")
		writeFile(checkout+"/prod.go", "package prod\n\nfunc Run() {}\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: checkout, Reason: storage.ReasonAdd})
		Expect(err).ToNot(HaveOccurred())
		_, err = Publish(ctx, database, prodPublication("r1", "100"))
		Expect(err).To(MatchError(ContainSubstring(`root "Co/Prod" has a module location "` + checkout + `"; it cannot also take the external location "` + prodURI + `"`)))
	})

	It("leaves producer-owned roots to their producer on reindex", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		published := publish(ctx, database, prodPublication("r1", "100"))
		checkout := canonicalTempDir()
		writeFile(checkout+"/go.mod", "module example.org/goside\n\ngo 1.26\n")
		writeFile(checkout+"/goside.go", "package goside\n\nfunc Run() {}\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: checkout, Reason: storage.ReasonAdd})
		Expect(err).ToNot(HaveOccurred())
		Expect(database.Exec("DELETE FROM location_heads").Error).To(Succeed(), "as the handle cutover leaves every location headless")

		run, err := StartMissingModulesTask(ctx, engine, false)
		Expect(err).ToNot(HaveOccurred())
		results, err := run.Wait(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(1))
		Expect(results[0].RootKey).To(Equal("example.org/goside"))
		var externalHeads, externalSnapshots int64
		Expect(database.Table("location_heads AS head").Joins("JOIN locations AS location ON location.id = head.location_id").
			Where("location.kind = ?", storage.LocationExternal).Count(&externalHeads).Error).To(Succeed())
		Expect(database.Model(&storage.ModuleSnapshot{}).Where("id = ?", published.SnapshotID).Count(&externalSnapshots).Error).To(Succeed())
		Expect([]int64{externalHeads, externalSnapshots}).To(Equal([]int64{0, 1}), "reindex --all neither reads nor republishes the external root")

		for _, path := range []string{prodURI, prodRoot} {
			_, err = StartModulesTask(ctx, engine, []ModuleOptions{{Path: path, ExistingOnly: true, Reason: storage.ReasonReindex}})
			Expect(err).To(MatchError(ContainSubstring(`root "Co/Prod" at "`+prodURI+`" is published by an external producer; republish it with uir import, since add and reindex index only Go modules`)), path)
		}
	})
})

// rowCounts counts the rows of each table.
func rowCounts(database *gorm.DB, tables ...string) []int64 {
	GinkgoHelper()
	counts := make([]int64, len(tables))
	for i, table := range tables {
		Expect(database.Table(table).Count(&counts[i]).Error).To(Succeed(), table)
	}
	return counts
}
