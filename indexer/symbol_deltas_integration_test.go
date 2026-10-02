package indexer

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// symbolDeltas returns a snapshot's symbol delta rows keyed by canonical symbol id.
func symbolDeltas(ctx context.Context, database *gorm.DB, snapshotID string) map[string]storage.SymbolDelta {
	GinkgoHelper()
	var rows []storage.SymbolDelta
	var snapshot storage.ModuleSnapshot
	Expect(database.Where("id = ?", snapshotID).First(&snapshot).Error).To(Succeed())
	Expect(database.Where("snapshot_ordinal = ?", snapshot.Ordinal).Find(&rows).Error).To(Succeed())
	handles := make([]int64, len(rows))
	for i, row := range rows {
		handles[i] = row.SymbolHandle
	}
	ids, err := storage.SymbolIDs(ctx, database, handles)
	Expect(err).ToNot(HaveOccurred())
	result := make(map[string]storage.SymbolDelta, len(rows))
	for _, row := range rows {
		result[ids[row.SymbolHandle]] = row
	}
	return result
}

func operations(deltas map[string]storage.SymbolDelta) map[string]storage.SourceOperation {
	result := make(map[string]storage.SourceOperation, len(deltas))
	for id, delta := range deltas {
		result[id] = delta.Operation
	}
	return result
}

// definedFingerprints is the snapshot's defined-symbol set derived from its active documents alone.
func (snapshot typedSnapshot) definedFingerprints() map[string]storage.SymbolFingerprints {
	GinkgoHelper()
	declarations := map[string][]storage.SymbolFingerprints{}
	for _, content := range snapshot.contents {
		for _, symbol := range content.Symbols {
			if symbol.ID == nil {
				continue
			}
			shape, err := storage.SymbolFingerprint(symbol.ShapeHash)
			Expect(err).ToNot(HaveOccurred())
			body, err := storage.SymbolFingerprint(symbol.BodyHash)
			Expect(err).ToNot(HaveOccurred())
			declarations[*symbol.ID] = append(declarations[*symbol.ID], storage.SymbolFingerprints{Shape: shape, Body: body})
		}
	}
	defined := make(map[string]storage.SymbolFingerprints, len(declarations))
	for id, fingerprints := range declarations {
		defined[id] = storage.FoldDeclarations(fingerprints)
	}
	return defined
}

// expectEffectiveSymbols checks that the deltas along the snapshot's base chain reproduce exactly the
// defined set its documents declare.
func expectEffectiveSymbols(ctx context.Context, database *gorm.DB, snapshot typedSnapshot) {
	GinkgoHelper()
	defined := snapshot.definedFingerprints()
	ids := make([]string, 0, len(defined))
	for id := range defined {
		ids = append(ids, id)
	}
	handles, err := storage.SymbolHandles(ctx, database, ids)
	Expect(err).ToNot(HaveOccurred())
	expected := make([]storage.EffectiveSymbol, 0, len(defined))
	for id, fingerprints := range defined {
		expected = append(expected, storage.EffectiveSymbol{Handle: handles[id], ShapeFP: &fingerprints.Shape, BodyFP: &fingerprints.Body})
	}
	slices.SortFunc(expected, func(left, right storage.EffectiveSymbol) int { return cmp.Compare(left.Handle, right.Handle) })
	actual, err := storage.EffectiveSymbols(ctx, database, uuid.MustParse(snapshot.id))
	Expect(err).ToNot(HaveOccurred())
	Expect(actual).To(Equal(expected))
}

// publishAndLoad indexes the workspace, checks the effective symbols, and returns the snapshot and its deltas.
func publishAndLoad(ctx context.Context, database *gorm.DB, engine *Indexer, workspace string) (typedSnapshot, map[string]storage.SymbolDelta) {
	GinkgoHelper()
	snapshot := loadTypedSnapshot(ctx, database, indexOnce(ctx, engine, workspace).SnapshotID)
	expectEffectiveSymbols(ctx, database, snapshot)
	return snapshot, symbolDeltas(ctx, database, snapshot.id)
}

const (
	bootA        = "package boot\n\nvar order []string\n\nfunc init() { order = append(order, \"a\") }\n\nfunc A() {}\n"
	bootAEdited  = "package boot\n\nvar order []string\n\nfunc init() { order = append(order, \"a\", \"again\") }\n\nfunc A() {}\n"
	bootANoInit  = "package boot\n\nvar order []string\n\nfunc A() {}\n"
	bootU        = "package boot\n\nfunc init() { order = append(order, \"u\") }\n"
	bootUNoInit  = "package boot\n"
	bootModule   = "example.org/boot"
	bootPackage  = bootModule + "/boot"
	bootInitName = "init"
)

var _ = Describe("symbol deltas", func() {
	DescribeTable("record every defined symbol initially and then only what changed in changed files",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeLedgerModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			moneyPath := filepath.Join(workspace, "money", "money.go")

			initial, deltas := publishAndLoad(ctx, database, engine, workspace)
			defined := initial.definedFingerprints()
			Expect(deltas).To(HaveLen(len(defined)))
			for id := range defined {
				Expect(deltas[id].Operation).To(Equal(storage.SourceSet), id)
			}
			add, unit := findSymbol(database, moneyPackage, "func", "Add"), findSymbol(database, moneyPackage, "func", "Unit")

			reordered := "// Add sums two amounts.\nfunc Add(a Amount, b Amount) Amount {\n\treturn Amount{Cents: b.Cents + a.Cents, Currency: a.Currency}\n}\n"
			writeFile(moneyPath, moneySource(moneyAmount, reordered, moneyScale, moneyUnit, moneyZero))
			_, bodied := publishAndLoad(ctx, database, engine, workspace)
			Expect(operations(bodied)).To(Equal(map[string]storage.SourceOperation{add.ID: storage.SourceSet}), "a body edit")
			Expect(*bodied[add.ID].ShapeFP).To(Equal(*deltas[add.ID].ShapeFP))
			Expect(*bodied[add.ID].BodyFP).ToNot(Equal(*deltas[add.ID].BodyFP))

			pointerUnit := "func Unit() *Amount { return &Amount{Cents: 1} }\n"
			writeFile(moneyPath, moneySource(moneyAmount, reordered, moneyScale, pointerUnit, moneyZero))
			signature, resigned := publishAndLoad(ctx, database, engine, workspace)
			Expect(operations(resigned)).To(Equal(map[string]storage.SourceOperation{unit.ID: storage.SourceSet}),
				"a signature edit re-extracts the importing book package, whose symbols keep their fingerprints")
			Expect(*resigned[unit.ID].ShapeFP).ToNot(Equal(*deltas[unit.ID].ShapeFP))
			Expect(signature.documents["book/book.go"].ID).ToNot(Equal(initial.documents["book/book.go"].ID))

			half := "func Half(a Amount) Amount { return Amount{Cents: a.Cents / 2, Currency: a.Currency} }\n"
			writeFile(moneyPath, moneySource(moneyAmount, reordered, moneyScale, pointerUnit, moneyZero, half))
			_, added := publishAndLoad(ctx, database, engine, workspace)
			Expect(operations(added)).To(Equal(map[string]storage.SourceOperation{findSymbol(database, moneyPackage, "func", "Half").ID: storage.SourceSet}))

			scale := findSymbol(database, moneyPackage, "func", "Scale")
			writeFile(moneyPath, moneySource(moneyAmount, reordered, pointerUnit, moneyZero, half))
			_, removed := publishAndLoad(ctx, database, engine, workspace)
			Expect(operations(removed)).To(Equal(map[string]storage.SourceOperation{scale.ID: storage.SourceDelete}))
			Expect(removed[scale.ID].ShapeFP).To(BeNil())

			writeFile(filepath.Join(workspace, "book", "book.go"), bookSource)
			writeFile(filepath.Join(workspace, "book", "count.go"), "package book\n\n"+countSource)
			_, moved := publishAndLoad(ctx, database, engine, workspace)
			Expect(moved).To(BeEmpty(), "Count moved between two changed files with the same fingerprints")

			twice := findSymbol(database, bookPackage, "func", "Twice")
			Expect(os.Remove(filepath.Join(workspace, "book", "use.go"))).To(Succeed())
			_, deleted := publishAndLoad(ctx, database, engine, workspace)
			Expect(operations(deleted)).To(Equal(map[string]storage.SourceOperation{twice.ID: storage.SourceDelete}))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("write no symbol delta for a dependency bump",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeTwoPackageModule(workspace)
			writeFile(filepath.Join(workspace, "go.sum"), goSumFor("v1.0.0"))
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			_, initial := publishAndLoad(ctx, database, engine, workspace)
			Expect(initial).ToNot(BeEmpty())

			writeFile(filepath.Join(workspace, "go.sum"), goSumFor("v1.1.0"))
			bumped, deltas := publishAndLoad(ctx, database, engine, workspace)
			Expect(loadSnapshot(database, bumped.id).BaseSnapshotID).ToNot(BeNil())
			Expect(deltas).To(BeEmpty())
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	// Every func init of a package shares one canonical id (gavel TODO 34ea4d17), and a package's input
	// hash covers all of its files, so a declaration can only disappear from a file whose document the
	// snapshot replaced: a file with unchanged bytes still gets a new document when its package changes.
	DescribeTable("fold the declarations of one id and keep a symbol still declared in a file whose bytes did not change",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			Expect(os.MkdirAll(filepath.Join(workspace, "boot"), 0o755)).To(Succeed())
			writeFile(filepath.Join(workspace, "go.mod"), "module "+bootModule+"\n\ngo 1.26\n")
			writeFile(filepath.Join(workspace, "boot", "a.go"), bootA)
			writeFile(filepath.Join(workspace, "boot", "u.go"), bootU)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())

			initial, deltas := publishAndLoad(ctx, database, engine, workspace)
			initFunc := findSymbol(database, bootPackage, "func", bootInitName)
			Expect(declarationsOf(initial, initFunc.ID)).To(Equal(2), "both files declare the one init id")

			writeFile(filepath.Join(workspace, "boot", "a.go"), bootAEdited)
			edited, rebodied := publishAndLoad(ctx, database, engine, workspace)
			Expect(edited.documents["boot/u.go"].ID).ToNot(Equal(initial.documents["boot/u.go"].ID), "u.go's bytes did not change, its document did")
			Expect(operations(rebodied)).To(Equal(map[string]storage.SourceOperation{initFunc.ID: storage.SourceSet}))
			Expect(*rebodied[initFunc.ID].ShapeFP).To(Equal(*deltas[initFunc.ID].ShapeFP), "every init has the same shape")
			Expect(*rebodied[initFunc.ID].BodyFP).ToNot(Equal(*deltas[initFunc.ID].BodyFP))

			writeFile(filepath.Join(workspace, "boot", "a.go"), bootANoInit)
			remaining, narrowed := publishAndLoad(ctx, database, engine, workspace)
			Expect(declarationsOf(remaining, initFunc.ID)).To(Equal(1))
			Expect(operations(narrowed)).To(Equal(map[string]storage.SourceOperation{initFunc.ID: storage.SourceSet}),
				"init is still declared in u.go, so it is set to u.go's fingerprints, not deleted")
			Expect(storage.SymbolFingerprints{Shape: *narrowed[initFunc.ID].ShapeFP, Body: *narrowed[initFunc.ID].BodyFP}).
				To(Equal(remaining.definedFingerprints()[initFunc.ID]))

			writeFile(filepath.Join(workspace, "boot", "u.go"), bootUNoInit)
			_, gone := publishAndLoad(ctx, database, engine, workspace)
			Expect(operations(gone)).To(Equal(map[string]storage.SourceOperation{initFunc.ID: storage.SourceDelete}))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	It("publishes two modules concurrently on PostgreSQL, retrying lost allocation races", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerPostgresOptions())
		workspaces := []string{GinkgoT().TempDir(), GinkgoT().TempDir()}
		writeLedgerModule(workspaces[0])
		writeTwoPackageModule(workspaces[1])
		results := make([]ModuleResult, len(workspaces))
		errs := make([]error, len(workspaces))
		var group sync.WaitGroup
		for i, workspace := range workspaces {
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			group.Go(func() {
				var published []ModuleResult
				published, errs[i] = engine.IndexModules(ctx, ModuleOptions{Path: workspace, Reason: storage.ReasonAdd})
				if errs[i] == nil {
					results[i] = published[0]
				}
			})
		}
		group.Wait()
		for i := range workspaces {
			Expect(errs[i]).ToNot(HaveOccurred())
			expectEffectiveSymbols(ctx, database, loadTypedSnapshot(ctx, database, results[i].SnapshotID))
		}
		var ordinals []int32
		Expect(database.Model(&storage.ModuleRoot{}).Order("ordinal").Pluck("ordinal", &ordinals).Error).To(Succeed())
		Expect(ordinals).To(Equal([]int32{1, 2}), "root ordinals stay dense")
		var numbers []int32
		Expect(database.Model(&storage.SymbolModule{}).Where("module_key IN ?", []string{ledgerModule, "example.org/shop"}).
			Order("number").Pluck("number", &numbers).Error).To(Succeed())
		Expect(numbers).To(Equal([]int32{2, 3}))
	})
})

// declarationsOf counts the declarations of id across the snapshot's documents.
func declarationsOf(snapshot typedSnapshot, id string) int {
	count := 0
	for _, content := range snapshot.contents {
		for _, symbol := range content.Symbols {
			if symbol.ID != nil && *symbol.ID == id {
				count++
			}
		}
	}
	return count
}
