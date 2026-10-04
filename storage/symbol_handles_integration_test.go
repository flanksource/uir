package storage_test

import (
	"context"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const serviceModule = "example.org/service"

// handleSymbol is a valid symbols row; its canonical key is its fields joined, and its id their digest.
func handleSymbol(moduleKey, packagePath, kind, name, visibility string) storage.Symbol {
	key := moduleKey + "|" + packagePath + "|" + kind + "|" + name
	return storage.Symbol{
		ID: digest(key), IdentityVersion: 1, CanonicalKey: key, ModuleKey: moduleKey, PackagePath: packagePath,
		Kind: kind, Name: name, SearchName: storage.SearchName(name), Visibility: visibility, ParameterTypes: storage.JSON(`[]`),
	}
}

// publishSymbols assigns handles to rows and inserts them in one transaction, as publication does.
func publishSymbols(ctx context.Context, database *gorm.DB, rows ...storage.Symbol) ([]storage.Symbol, error) {
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := storage.AssignSymbolHandles(ctx, tx, rows); err != nil {
			return err
		}
		return storage.UpsertSymbols(ctx, tx, rows, 64)
	})
	return rows, err
}

func handleOf(module, pkg uint64, visibility symbolhandle.Visibility, kind symbolhandle.Kind, local uint64) int64 {
	GinkgoHelper()
	handle, err := symbolhandle.Pack(symbolhandle.Fields{Module: module, Package: pkg, Visibility: visibility, Kind: kind, Local: local})
	Expect(err).ToNot(HaveOccurred())
	return handle
}

func handlesByName(rows []storage.Symbol) map[string]int64 {
	handles := make(map[string]int64, len(rows))
	for _, row := range rows {
		handles[row.Name] = row.Handle
	}
	return handles
}

var (
	errorType  = handleSymbol("", "", "builtin", "error", "exported")
	fmtPackage = handleSymbol("std", "fmt", "package", "fmt", "exported")
	storeType  = handleSymbol(serviceModule, serviceModule+"/a", "type", "Store", "exported")
	saveMethod = handleSymbol(serviceModule, serviceModule+"/a", "method", "Save", "exported")
	loadFunc   = handleSymbol(serviceModule, serviceModule+"/a", "func", "load", "internal")
	parseFunc  = handleSymbol(serviceModule, serviceModule+"/a", "func", "parse", "internal")
	runFunc    = handleSymbol(serviceModule, serviceModule+"/b", "func", "Run", "exported")
)

var _ = Describe("symbol handle allocation", func() {
	exported, internal := symbolhandle.Exported, symbolhandle.Internal

	DescribeTable("numbers modules and packages on demand and packs dense locals per bucket",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			first, err := publishSymbols(ctx, database, errorType, fmtPackage, storeType, saveMethod, loadFunc, parseFunc, runFunc)
			Expect(err).ToNot(HaveOccurred())
			Expect(handlesByName(first)).To(Equal(map[string]int64{
				"error": handleOf(0, 0, exported, symbolhandle.KindBuiltin, 0),
				"fmt":   handleOf(1, 0, exported, symbolhandle.KindPackage, 0),
				"Store": handleOf(2, 0, exported, symbolhandle.KindType, 0),
				"Save":  handleOf(2, 0, exported, symbolhandle.KindMethod, 0),
				"load":  handleOf(2, 0, internal, symbolhandle.KindFunc, 0),
				"parse": handleOf(2, 0, internal, symbolhandle.KindFunc, 1),
				"Run":   handleOf(2, 1, exported, symbolhandle.KindFunc, 0),
			}), "builtins are module 0, std module 1, the first other module 2; packages number from 0 in path order")

			var modules []storage.SymbolModule
			Expect(database.Order("number").Find(&modules).Error).To(Succeed())
			Expect(modules).To(Equal([]storage.SymbolModule{{Number: 0, ModuleKey: ""}, {Number: 1, ModuleKey: "std"}, {Number: 2, ModuleKey: serviceModule}}))

			walk := handleSymbol(serviceModule, serviceModule+"/a", "func", "walk", "internal")
			other := handleSymbol(serviceModule, serviceModule+"/c", "var", "Default", "exported")
			second, err := publishSymbols(ctx, database, storeType, walk, parseFunc, other)
			Expect(err).ToNot(HaveOccurred())
			Expect(handlesByName(second)).To(Equal(map[string]int64{
				"Store":   handleOf(2, 0, exported, symbolhandle.KindType, 0),
				"walk":    handleOf(2, 0, internal, symbolhandle.KindFunc, 2),
				"parse":   handleOf(2, 0, internal, symbolhandle.KindFunc, 1),
				"Default": handleOf(2, 2, exported, symbolhandle.KindVar, 0),
			}), "stored symbols keep their handle; a new symbol takes the next local of an existing bucket")
		},
		Entry("SQLite", sqliteOptions("handles.db")),
		Entry("PostgreSQL", postgresOptions("uir_handles")),
	)

	DescribeTable("fails loudly on a stored handle that disagrees with its row and on a full bucket",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			_, err := publishSymbols(ctx, database, loadFunc)
			Expect(err).ToNot(HaveOccurred())
			Expect(database.Model(&storage.Symbol{}).Where("id = ?", loadFunc.ID).
				Update("handle", handleOf(2, 0, internal, symbolhandle.KindVar, 0)).Error).To(Succeed())
			_, err = publishSymbols(ctx, database, loadFunc)
			Expect(err).To(MatchError(ContainSubstring("stored handle")))

			full := handleSymbol(serviceModule, serviceModule+"/a", "const", "Last", "exported")
			full.Handle = handleOf(2, 0, exported, symbolhandle.KindConst, symbolhandle.MaxLocal)
			Expect(database.Create(&full).Error).To(Succeed())
			_, err = publishSymbols(ctx, database, handleSymbol(serviceModule, serviceModule+"/a", "const", "Next", "exported"))
			Expect(err).To(MatchError(ContainSubstring("local 536870912 exceeds 29 bits")))
		},
		Entry("SQLite", sqliteOptions("handle-errors.db")),
		Entry("PostgreSQL", postgresOptions("uir_handle_errors")),
	)

	DescribeTable("maps symbol ids, document ids, and roots to their surrogates and back",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			rows, err := publishSymbols(ctx, database, storeType, runFunc)
			Expect(err).ToNot(HaveOccurred())
			handles, err := storage.SymbolHandles(ctx, database, []string{storeType.ID, runFunc.ID})
			Expect(err).ToNot(HaveOccurred())
			Expect(handles).To(Equal(map[string]int64{storeType.ID: rows[0].Handle, runFunc.ID: rows[1].Handle}))
			ids, err := storage.SymbolIDs(ctx, database, []int64{rows[0].Handle, rows[1].Handle})
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal(map[int64]string{rows[0].Handle: storeType.ID, rows[1].Handle: runFunc.ID}))
			_, err = storage.SymbolHandles(ctx, database, []string{storeType.ID, loadFunc.ID})
			Expect(err).To(MatchError(ContainSubstring(loadFunc.ID)))
			_, err = storage.SymbolIDs(ctx, database, []int64{rows[0].Handle + 1})
			Expect(err).To(MatchError(ContainSubstring("handle")))

			fixture := newModuleFixture(database, "/workspace/service")
			ordinal, err := storage.RootOrdinal(ctx, database, fixture.root.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(ordinal).To(Equal(fixture.root.Ordinal))
			next, err := storage.NextModuleOrdinal(ctx, database)
			Expect(err).ToNot(HaveOccurred())
			Expect(next).To(Equal(fixture.root.Ordinal + 1))
			_, err = storage.RootOrdinal(ctx, database, uuid.New())
			Expect(err).To(MatchError(ContainSubstring("root")))

			next64, err := storage.NextDocumentOrdinal(ctx, database)
			Expect(err).ToNot(HaveOccurred())
			Expect(next64).To(Equal(int64(1)), "an empty documents table starts at 1")
			revision := sourceRevision(fixture.root, "service.go", "service")
			document := syntaxDocument(revision, digest("input"))
			document.Ordinal = 7
			createAll(database, &revision, &document)
			next64, err = storage.NextDocumentOrdinal(ctx, database)
			Expect(err).ToNot(HaveOccurred())
			Expect(next64).To(Equal(int64(8)))
			ordinals, err := storage.DocumentOrdinals(ctx, database, []uuid.UUID{document.ID})
			Expect(err).ToNot(HaveOccurred())
			Expect(ordinals).To(Equal(map[uuid.UUID]int64{document.ID: 7}))
			documents, err := storage.DocumentIDs(ctx, database, []int64{7})
			Expect(err).ToNot(HaveOccurred())
			Expect(documents).To(Equal(map[int64]uuid.UUID{7: document.ID}))
			_, err = storage.DocumentIDs(ctx, database, []int64{8})
			Expect(err).To(MatchError(ContainSubstring("document ordinal 8")))
		},
		Entry("SQLite", sqliteOptions("lookups.db")),
		Entry("PostgreSQL", postgresOptions("uir_lookups")),
	)
})
