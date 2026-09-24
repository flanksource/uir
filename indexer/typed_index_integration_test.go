package indexer

import (
	"os"
	"path/filepath"
	"time"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// findMember returns the one symbols row owned by owner with this kind and name.
func findMember(database *gorm.DB, owner storage.Symbol, kind, name string) storage.Symbol {
	GinkgoHelper()
	var rows []storage.Symbol
	Expect(database.Where("owner_id = ? AND kind = ? AND name = ?", owner.ID, kind, name).Find(&rows).Error).To(Succeed())
	Expect(rows).To(HaveLen(1), "%s %s.%s", kind, owner.Name, name)
	return rows[0]
}

func occurrencesOf(content storage.DocumentContent, symbolID string) []storage.DocumentOccurrence {
	var found []storage.DocumentOccurrence
	for _, occurrence := range content.Occurrences {
		if occurrence.Symbol != nil && *occurrence.Symbol == symbolID {
			found = append(found, occurrence)
		}
	}
	return found
}

var _ = Describe("typed extraction", func() {
	DescribeTable("records canonical identities, implements, duplicate references, and no builtin postings",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeLedgerModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			result := indexOnce(ctx, engine, workspace)
			snapshot := loadTypedSnapshot(ctx, database, result.SnapshotID)
			Expect(loadSnapshot(database, result.SnapshotID).Coverage).To(Equal(storage.CoverageIndexed))
			for _, packagePath := range []string{moneyPackage, bookPackage} {
				row := snapshot.packageRow(database, packagePath)
				Expect(row.Coverage).To(Equal(storage.CoverageIndexed), packagePath)
				Expect(row.ExportShapeHash).To(HaveValue(HaveLen(64)), packagePath)
			}
			for path, document := range snapshot.documents {
				Expect(document.Coverage).To(Equal(storage.CoverageIndexed), path)
			}

			add := findSymbol(database, moneyPackage, "func", "Add")
			Expect(add.CanonicalKey).To(Equal(symbolIdentity{
				ModuleKey: ledgerModule, PackagePath: moneyPackage, Kind: "func", Name: "Add",
				ParameterTypes: []string{moneyPackage + ".Amount", moneyPackage + ".Amount"},
			}.canonicalKey()))
			Expect(add.ParameterTypes).To(MatchJSON(`["example.org/ledger/money.Amount","example.org/ledger/money.Amount"]`))
			addEntry := snapshot.entry("money/money.go", add.ID)
			Expect([]string{addEntry.Kind, addEntry.Visibility, addEntry.Shape, addEntry.ShapeHash}).To(Equal([]string{
				"func", "exported", "func Add(a Amount, b Amount) Amount", shapeHash("func Add(a Amount, b Amount) Amount"),
			}))

			total := findSymbol(database, bookPackage, "func", "Total")
			calls := occurrencesOf(snapshot.contents["book/book.go"], add.ID)
			Expect(calls).To(HaveLen(1))
			Expect(calls[0].Role).To(Equal("call"))
			Expect(calls[0].Enclosing).To(HaveValue(Equal(total.ID)))
			Expect(calls[0].Target).To(HaveValue(Equal(uir.Identifier{Package: moneyPackage, Method: "Add", NodeType: uir.NodeTypeMethod})))

			book := findSymbol(database, bookPackage, "type", "Book")
			namer := findSymbol(database, bookPackage, "type", "Namer")
			name := findMember(database, book, "method", "Name")
			Expect(snapshot.entry("book/book.go", book.ID).Implements).To(Equal([]string{namer.ID}))
			Expect(snapshot.postings(database, namer.ID)).To(ConsistOf("book/book.go definition 1", "book/book.go reference 1", "book/book.go implements 1"))
			Expect(snapshot.postings(database, name.ID)).To(ConsistOf("book/book.go definition 1", "book/book.go reference 1", "book/use.go reference 2"))

			var builtinLen storage.Symbol
			Expect(database.Where("kind = ? AND name = ?", "builtin", "len").First(&builtinLen).Error).To(Succeed())
			Expect(builtinLen.ModuleKey).To(BeEmpty())
			Expect(occurrencesOf(snapshot.contents["book/book.go"], builtinLen.ID)).To(HaveLen(1))
			Expect(countRows(database, &storage.SymbolPosting{}, "symbol_id IN (?)", database.Model(&storage.Symbol{}).Select("id").Where("kind = ?", "builtin"))).To(BeZero())
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("keeps identity across a parameter rename and creates a new identity for a parameter type change",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeLedgerModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			first := loadTypedSnapshot(ctx, database, indexOnce(ctx, engine, workspace).SnapshotID)
			scale := findSymbol(database, moneyPackage, "func", "Scale")
			original := first.entry("money/money.go", scale.ID)

			renamed := "func Scale(a Amount, by int) Amount { return Amount{Cents: a.Cents * int64(by), Currency: a.Currency} }\n"
			writeFile(filepath.Join(workspace, "money", "money.go"), moneySource(moneyAmount, moneyAdd, renamed, moneyUnit, moneyZero))
			second := loadTypedSnapshot(ctx, database, indexOnce(ctx, engine, workspace).SnapshotID)
			Expect(findSymbol(database, moneyPackage, "func", "Scale").ID).To(Equal(scale.ID))
			renamedEntry := second.entry("money/money.go", scale.ID)
			Expect(renamedEntry.Shape).To(Equal("func Scale(a Amount, by int) Amount"))
			Expect(renamedEntry.ShapeHash).ToNot(Equal(original.ShapeHash))

			retyped := "func Scale(a Amount, factor float64) Amount { return Amount{Cents: int64(float64(a.Cents) * factor), Currency: a.Currency} }\n"
			writeFile(filepath.Join(workspace, "money", "money.go"), moneySource(moneyAmount, moneyAdd, retyped, moneyUnit, moneyZero))
			third := loadTypedSnapshot(ctx, database, indexOnce(ctx, engine, workspace).SnapshotID)
			var scales []storage.Symbol
			Expect(database.Where("package_path = ? AND kind = ? AND name = ?", moneyPackage, "func", "Scale").Find(&scales).Error).To(Succeed())
			Expect(scales).To(HaveLen(2), "the old identity is retained")
			current := scales[0]
			if current.ID == scale.ID {
				current = scales[1]
			}
			Expect(current.ParameterTypes).To(MatchJSON(`["example.org/ledger/money.Amount","float64"]`))
			Expect(third.entry("money/money.go", current.ID).Shape).To(Equal("func Scale(a Amount, factor float64) Amount"))
			Expect(occurrencesOf(third.contents["money/money.go"], scale.ID)).To(BeEmpty())
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("changes shape and export shape on return and field type changes, and only body_hash on a body edit",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeLedgerModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			moneyPath := filepath.Join(workspace, "money", "money.go")
			first := loadTypedSnapshot(ctx, database, indexOnce(ctx, engine, workspace).SnapshotID)
			unit := findSymbol(database, moneyPackage, "func", "Unit")
			amount := findSymbol(database, moneyPackage, "type", "Amount")
			currency := findMember(database, amount, "field", "Currency")
			add := findSymbol(database, moneyPackage, "func", "Add")

			pointerUnit := "func Unit() *Amount { return &Amount{Cents: 1} }\n"
			writeFile(moneyPath, moneySource(moneyAmount, moneyAdd, moneyScale, pointerUnit, moneyZero))
			returned := loadTypedSnapshot(ctx, database, indexOnce(ctx, engine, workspace).SnapshotID)
			Expect(findSymbol(database, moneyPackage, "func", "Unit").ID).To(Equal(unit.ID))
			Expect(returned.entry("money/money.go", unit.ID).Shape).To(Equal("func Unit() *Amount"))
			Expect(returned.entry("money/money.go", unit.ID).ShapeHash).ToNot(Equal(first.entry("money/money.go", unit.ID).ShapeHash))
			Expect(returned.packageRow(database, moneyPackage).ExportShapeHash).ToNot(Equal(first.packageRow(database, moneyPackage).ExportShapeHash))
			for _, path := range []string{"book/book.go", "book/use.go"} {
				Expect(returned.documents[path].ID).ToNot(Equal(first.documents[path].ID), "%s imports the changed export shape", path)
			}

			codedAmount := "type Code string\n\n" + "type Amount struct {\n\tCents    int64\n\tCurrency Code `json:\"currency\"`\n}\n"
			writeFile(moneyPath, moneySource(codedAmount, moneyAdd, moneyScale, pointerUnit, moneyZero))
			retyped := loadTypedSnapshot(ctx, database, indexOnce(ctx, engine, workspace).SnapshotID)
			Expect(findMember(database, amount, "field", "Currency").ID).To(Equal(currency.ID))
			Expect(retyped.entry("money/money.go", currency.ID).Shape).To(Equal("Currency Code `json:\"currency\"`"))
			Expect(retyped.entry("money/money.go", currency.ID).ShapeHash).ToNot(Equal(returned.entry("money/money.go", currency.ID).ShapeHash))
			Expect(retyped.entry("money/money.go", amount.ID).Shape).To(Equal("type Amount struct {\n\tCents    int64\n\tCurrency Code `json:\"currency\"`\n}"))
			Expect(retyped.packageRow(database, moneyPackage).ExportShapeHash).ToNot(Equal(returned.packageRow(database, moneyPackage).ExportShapeHash))
			Expect(retyped.documents["book/book.go"].ID).ToNot(Equal(returned.documents["book/book.go"].ID))

			reordered := "// Add sums two amounts.\nfunc Add(a Amount, b Amount) Amount {\n\treturn Amount{Cents: b.Cents + a.Cents, Currency: a.Currency}\n}\n"
			writeFile(moneyPath, moneySource(codedAmount, reordered, moneyScale, pointerUnit, moneyZero))
			bodied := loadTypedSnapshot(ctx, database, indexOnce(ctx, engine, workspace).SnapshotID)
			before, after := retyped.entry("money/money.go", add.ID), bodied.entry("money/money.go", add.ID)
			Expect(after.ShapeHash).To(Equal(before.ShapeHash))
			Expect(after.BodyHash).ToNot(Equal(before.BodyHash))
			Expect(bodied.packageRow(database, moneyPackage).ExportShapeHash).To(Equal(retyped.packageRow(database, moneyPackage).ExportShapeHash))
			Expect(bodied.documents["money/money.go"].ID).ToNot(Equal(retyped.documents["money/money.go"].ID))
			for _, path := range []string{"book/book.go", "book/use.go"} {
				Expect(bodied.documents[path].ID).To(Equal(retyped.documents[path].ID), "%s is reused after a body-only edit", path)
			}
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("keeps a symbol's id and moves its definition posting when it moves to another file",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeLedgerModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			first := loadTypedSnapshot(ctx, database, indexOnce(ctx, engine, workspace).SnapshotID)
			count := findSymbol(database, bookPackage, "func", "Count")
			Expect(first.postings(database, count.ID)).To(ConsistOf("book/book.go definition 1", "book/book.go reference 1"))

			writeFile(filepath.Join(workspace, "book", "book.go"), bookSource)
			writeFile(filepath.Join(workspace, "book", "count.go"), "package book\n\n"+countSource)
			moved := loadTypedSnapshot(ctx, database, indexOnce(ctx, engine, workspace).SnapshotID)
			Expect(findSymbol(database, bookPackage, "func", "Count").ID).To(Equal(count.ID))
			Expect(moved.postings(database, count.ID)).To(ConsistOf("book/count.go definition 1", "book/count.go reference 1"))
			Expect(moved.entry("book/count.go", count.ID).Shape).To(Equal("func Count(books []Book) int"))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("records a type error as partial with diagnostics and an unloadable package as syntax",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			for _, directory := range []string{"typed", "mixed", "fine"} {
				Expect(os.MkdirAll(filepath.Join(workspace, directory), 0o755)).To(Succeed())
			}
			writeFile(filepath.Join(workspace, "go.mod"), "module example.org/broken\n\ngo 1.26\n")
			writeFile(filepath.Join(workspace, "typed", "typed.go"), "package typed\n\nfunc Wrong() int { return \"text\" }\n\nfunc Right() int { return 1 }\n")
			writeFile(filepath.Join(workspace, "mixed", "a.go"), "package alpha\n\nfunc A() {}\n")
			writeFile(filepath.Join(workspace, "mixed", "b.go"), "package beta\n\nfunc B() {}\n")
			writeFile(filepath.Join(workspace, "fine", "fine.go"), "package fine\n\nfunc Fine() {}\n")
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			result := indexOnce(ctx, engine, workspace)
			snapshot := loadTypedSnapshot(ctx, database, result.SnapshotID)
			Expect(loadSnapshot(database, result.SnapshotID).Coverage).To(Equal(storage.CoverageSyntax), "a snapshot is as weak as its weakest package")

			partial := snapshot.packageRow(database, "example.org/broken/typed")
			Expect(partial.Coverage).To(Equal(storage.CoveragePartial))
			Expect(partial.ExportShapeHash).To(BeNil())
			Expect(partial.Diagnostics.String()).To(ContainSubstring("cannot use"))
			typed := snapshot.contents["typed/typed.go"]
			Expect(snapshot.documents["typed/typed.go"].Coverage).To(Equal(storage.CoveragePartial))
			Expect(typed.Diagnostics).To(ConsistOf(HaveField("Message", ContainSubstring("cannot use"))))
			Expect(typed.Diagnostics[0].Range[0]).To(Equal(3))
			right := findSymbol(database, "example.org/broken/typed", "func", "Right")
			Expect(snapshot.entry("typed/typed.go", right.ID).Shape).To(Equal("func Right() int"))

			syntax := snapshot.packageRow(database, "example.org/broken/mixed")
			Expect(syntax.Coverage).To(Equal(storage.CoverageSyntax))
			Expect(syntax.Diagnostics.String()).To(ContainSubstring("found packages alpha (a.go) and beta (b.go)"))
			Expect(syntax.Diagnostics.String()).ToNot(ContainSubstring(workspace), "diagnostics are location independent")
			for _, path := range []string{"mixed/a.go", "mixed/b.go"} {
				Expect(snapshot.documents[path].Coverage).To(Equal(storage.CoverageSyntax))
				Expect(snapshot.contents[path].Symbols).To(HaveLen(1))
				Expect(snapshot.contents[path].Symbols[0].ID).To(BeNil())
				Expect(countRows(database, &storage.SymbolPosting{}, "document_id = ?", snapshot.documents[path].ID)).To(BeZero())
			}
			Expect(snapshot.packageRow(database, "example.org/broken/fine").Coverage).To(Equal(storage.CoverageIndexed))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("re-extracts only the importing package when a go.work sibling's exported type changes",
		func(ctx SpecContext, options func() storage.DBOptions) {
			GinkgoT().Setenv("GOWORK", "")
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			app, lib := filepath.Join(workspace, "app"), filepath.Join(workspace, "lib")
			for _, directory := range []string{filepath.Join(app, "other"), lib} {
				Expect(os.MkdirAll(directory, 0o755)).To(Succeed())
			}
			writeFile(filepath.Join(workspace, "go.work"), "go 1.26\n\nuse (\n\t./app\n\t./lib\n)\n")
			writeFile(filepath.Join(lib, "go.mod"), "module example.org/lib\n\ngo 1.26\n")
			writeFile(filepath.Join(lib, "lib.go"), "package lib\n\ntype Config struct{ Name string }\n")
			writeFile(filepath.Join(app, "go.mod"), "module example.org/app\n\ngo 1.26\n")
			writeFile(filepath.Join(app, "app.go"), "package app\n\nimport \"example.org/lib\"\n\nfunc Use() lib.Config { return lib.Config{} }\n")
			writeFile(filepath.Join(app, "other", "other.go"), "package other\n\nfunc Alone() {}\n")
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			first := indexOnce(ctx, engine, app)
			before := loadTypedSnapshot(ctx, database, first.SnapshotID)
			config := findSymbol(database, "example.org/lib", "type", "Config")
			Expect(config.ModuleKey).To(Equal("example.org/lib"))
			Expect(outcomeOf(indexOnce(ctx, engine, app))).To(Equal(triggerOutcome{Unchanged: true, HeadVersion: 1, ReusedFiles: 2}))

			writeFile(filepath.Join(lib, "lib.go"), "package lib\n\ntype Config struct{ Name int }\n")
			second := indexOnce(ctx, engine, app)
			Expect(outcomeOf(second)).To(Equal(triggerOutcome{HeadVersion: 2, ParsedFiles: 1, ReusedFiles: 1}))
			Expect(countRows(database, &storage.SourceDelta{}, "snapshot_id = ?", second.SnapshotID)).To(BeZero())
			Expect(loadSnapshot(database, second.SnapshotID).ContextHash).ToNot(Equal(loadSnapshot(database, first.SnapshotID).ContextHash))
			after := loadTypedSnapshot(ctx, database, second.SnapshotID)
			Expect(after.documents["app.go"].ID).ToNot(Equal(before.documents["app.go"].ID))
			Expect(after.documents["other/other.go"].ID).To(Equal(before.documents["other/other.go"].ID))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("types test files through their test variants and excludes files build constraints drop",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			for _, directory := range []string{"calc", "scripts"} {
				Expect(os.MkdirAll(filepath.Join(workspace, directory), 0o755)).To(Succeed())
			}
			writeFile(filepath.Join(workspace, "go.mod"), "module example.org/calc\n\ngo 1.26\n")
			writeFile(filepath.Join(workspace, "calc", "calc.go"), "package calc\n\nfunc Double(value int) int { return value * 2 }\n")
			writeFile(filepath.Join(workspace, "calc", "helper_test.go"), "package calc\n\nfunc triple(value int) int { return Double(value) + value }\n")
			writeFile(filepath.Join(workspace, "calc", "calc_test.go"), "package calc_test\n\nimport \"example.org/calc/calc\"\n\nfunc useDouble() int { return calc.Double(1) }\n")
			writeFile(filepath.Join(workspace, "calc", "never.go"), "//go:build uirnevertagged\n\npackage calc\n\nfunc Never() {}\n")
			writeFile(filepath.Join(workspace, "scripts", "script.go"), "//go:build ignore\n\npackage main\n\nfunc main() {}\n")
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			results, err := engine.IndexModules(ctx, ModuleOptions{Path: workspace, IncludeTests: true})
			Expect(err).ToNot(HaveOccurred())
			snapshot := loadTypedSnapshot(ctx, database, results[0].SnapshotID)
			Expect(loadSnapshot(database, results[0].SnapshotID).Coverage).To(Equal(storage.CoverageIndexed), "excluded packages do not weaken the snapshot")

			calc := snapshot.packageRow(database, "example.org/calc/calc")
			Expect([]any{calc.Coverage, calc.FileCount}).To(Equal([]any{storage.CoverageIndexed, 4}))
			Expect(calc.ExportShapeHash).To(HaveValue(HaveLen(64)))
			double := findSymbol(database, "example.org/calc/calc", "func", "Double")
			Expect(snapshot.postings(database, double.ID)).To(ConsistOf(
				"calc/calc.go definition 1", "calc/calc.go reference 1", "calc/helper_test.go reference 1", "calc/calc_test.go reference 1",
			))
			Expect(snapshot.contents["calc/calc_test.go"].PackagePath).To(Equal("example.org/calc/calc_test"))
			external := findSymbol(database, "example.org/calc/calc_test", "func", "useDouble")
			Expect(external.ModuleKey).To(Equal("example.org/calc"))

			Expect(snapshot.documents["calc/never.go"].Coverage).To(Equal(storage.CoverageExcluded))
			Expect(snapshot.contents["calc/never.go"].Excluded).To(HavePrefix("build constraints exclude the file under GOOS="))
			scripts := snapshot.packageRow(database, "example.org/calc/scripts")
			Expect([]any{scripts.Coverage, scripts.ExportShapeHash}).To(Equal([]any{storage.CoverageExcluded, (*string)(nil)}))
			Expect(snapshot.contents["scripts/script.go"].Excluded).To(HavePrefix("no package builds from the directory"))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("duplicates no symbol, document, or posting on a forced re-run",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeLedgerModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			first := indexOnce(ctx, engine, workspace)
			before := tableCounts(database)
			forced, err := engine.IndexModules(ctx, ModuleOptions{Path: workspace, Force: true})
			Expect(err).ToNot(HaveOccurred())
			Expect(forced[0].ParsedFiles).To(Equal(3))
			expected := map[string]int64{}
			for table, count := range before {
				expected[table] = count
			}
			expected["snapshots"]++
			expected["package_coverage"] += 2
			Expect(tableCounts(database)).To(Equal(expected))
			Expect(loadTypedSnapshot(ctx, database, forced[0].SnapshotID).documentIDs()).To(Equal(loadTypedSnapshot(ctx, database, first.SnapshotID).documentIDs()))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("writes nothing when the head compare-and-swap fails",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeLedgerModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			indexOnce(ctx, engine, workspace)
			half := "func Half(a Amount) Amount { return Amount{Cents: a.Cents / 2, Currency: a.Currency} }\n"
			writeFile(filepath.Join(workspace, "money", "money.go"), moneySource(moneyAmount, moneyAdd, moneyScale, moneyUnit, moneyZero, half))
			roots, err := discoverModules(ctx, workspace, false)
			Expect(err).ToNot(HaveOccurred())
			extraction, err := extractModule(ctx, engine.loadPackages, roots[0], false)
			Expect(err).ToNot(HaveOccurred())
			before := tableCounts(database)

			err = database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
				root, location, err := ensureModuleLocation(ctx, transaction, roots[0], map[string]storage.ModuleLocation{})
				if err != nil {
					return err
				}
				base, err := loadModuleBase(ctx, transaction, root, location)
				if err != nil {
					return err
				}
				base.head.Version--
				_, err = publishSnapshot(ctx, transaction, snapshotPublication{
					root: root, location: location, base: base, extraction: extraction, startedAt: time.Now().UTC(),
				}, &ModuleResult{})
				return err
			})
			Expect(err).To(MatchError(ContainSubstring("head changed during indexing")))
			Expect(tableCounts(database)).To(Equal(before))
			Expect(countRows(database, &storage.Symbol{}, "name = ?", "Half")).To(BeZero())
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)
})
