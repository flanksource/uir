package storage_test

import (
	"fmt"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// storedKinds is every symbol_kinds row, ordered by code.
func storedKinds(database *gorm.DB) []storage.SymbolKind {
	GinkgoHelper()
	var rows []storage.SymbolKind
	Expect(database.Order("code").Find(&rows).Error).To(Succeed())
	return rows
}

var _ = Describe("symbol kind registry", func() {
	DescribeTable("seeds the builtin kinds 1 through 18 on open and keeps them on reopen",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			builtins := make([]storage.SymbolKind, 0, symbolhandle.MaxBuiltinKind)
			for _, spec := range symbolhandle.BuiltinKinds() {
				builtins = append(builtins, storage.SymbolKind{Code: int16(spec.Code), Name: spec.Name, Category: string(spec.Category)})
			}
			Expect(storedKinds(openDB(ctx, config))).To(Equal(builtins))
			Expect(storedKinds(openDB(ctx, config))).To(Equal(builtins))
			code, err := storage.KindCode(ctx, openDB(ctx, config), "method")
			Expect(err).ToNot(HaveOccurred())
			Expect(code).To(Equal(symbolhandle.KindMethod))
		},
		Entry("SQLite", sqliteOptions("kinds-seeded.db")),
		Entry("PostgreSQL", postgresOptions("uir_kinds_seeded")),
	)

	DescribeTable("allocates custom kinds from 63 downward, idempotently, and fails loudly on a mismatch or when the field is full",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			rule, err := storage.RegisterKind(ctx, database, "oipa.rule", symbolhandle.CategoryCallable)
			Expect(err).ToNot(HaveOccurred())
			segment, err := storage.RegisterKind(ctx, database, "oipa.segment", symbolhandle.CategoryMember)
			Expect(err).ToNot(HaveOccurred())
			Expect([]symbolhandle.Kind{rule, segment}).To(Equal([]symbolhandle.Kind{63, 62}))

			again, err := storage.RegisterKind(ctx, database, "oipa.rule", symbolhandle.CategoryCallable)
			Expect(err).ToNot(HaveOccurred())
			Expect(again).To(Equal(rule), "registering the same name and category returns its code")
			_, err = storage.RegisterKind(ctx, database, "oipa.rule", symbolhandle.CategoryType)
			Expect(err).To(MatchError(ContainSubstring(`symbol kind "oipa.rule" is registered as callable, not type`)))
			_, err = storage.RegisterKind(ctx, database, "rule", symbolhandle.CategoryCallable)
			Expect(err).To(MatchError(ContainSubstring("must be namespaced")))
			_, err = storage.RegisterKind(ctx, database, "func", symbolhandle.CategoryCallable)
			Expect(err).To(MatchError(ContainSubstring("must be namespaced")), "a builtin name is never a custom kind")

			Expect(storedKinds(database)[len(storedKinds(database))-2:]).To(Equal([]storage.SymbolKind{
				{Code: 62, Name: "oipa.segment", Category: "member", Namespace: "oipa"},
				{Code: 63, Name: "oipa.rule", Category: "callable", Namespace: "oipa"},
			}))
			kinds, err := storage.LoadSymbolKinds(ctx, database)
			Expect(err).ToNot(HaveOccurred())
			Expect(kinds.Lookup("oipa.segment")).To(Equal(symbolhandle.KindSpec{Code: 62, Name: "oipa.segment", Category: symbolhandle.CategoryMember}))

			for code := 61; code > int(symbolhandle.MaxBuiltinKind); code-- {
				allocated, err := storage.RegisterKind(ctx, database, fmt.Sprintf("acme.kind%d", code), symbolhandle.CategoryMember)
				Expect(err).ToNot(HaveOccurred())
				Expect(allocated).To(Equal(symbolhandle.Kind(code)))
			}
			_, err = storage.RegisterKind(ctx, database, "acme.overflow", symbolhandle.CategoryMember)
			Expect(err).To(MatchError(ContainSubstring("no custom symbol kind code is free: the next would be 18, the highest builtin code")))
			_, err = storage.KindCode(ctx, database, "acme.overflow")
			Expect(err).To(MatchError(ContainSubstring(`symbol kind "acme.overflow" is not registered`)))
		},
		Entry("SQLite", sqliteOptions("kinds-custom.db")),
		Entry("PostgreSQL", postgresOptions("uir_kinds_custom")),
	)

	DescribeTable("refuses a symbol whose kind is not registered",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			symbol := storage.Symbol{
				ID: digest("acme.Rule"), IdentityVersion: 1, CanonicalKey: "acme.Rule", ModuleKey: "example.org/rules", PackagePath: "example.org/rules",
				Kind: "acme.rule", Name: "Rule", SearchName: "rule", Visibility: "exported", ParameterTypes: storage.JSON(`[]`),
			}
			err := storage.AssignSymbolHandles(ctx, database, []storage.Symbol{symbol})
			Expect(err).To(MatchError(ContainSubstring(`symbol kind "acme.rule" is not registered`)))

			_, err = storage.RegisterKind(ctx, database, "acme.rule", symbolhandle.CategoryCallable)
			Expect(err).ToNot(HaveOccurred())
			rows := []storage.Symbol{symbol}
			Expect(storage.AssignSymbolHandles(ctx, database, rows)).To(Succeed())
			fields, err := symbolhandle.Unpack(rows[0].Handle)
			Expect(err).ToNot(HaveOccurred())
			Expect(fields).To(Equal(symbolhandle.Fields{Module: 2, Package: 0, Visibility: symbolhandle.Exported, Kind: 63, Local: 0}))
			Expect(database.Create(&rows).Error).To(Succeed())

			unregistered := rows[0]
			unregistered.ID, unregistered.CanonicalKey, unregistered.Kind, unregistered.Handle = digest("acme.Other"), "acme.Other", "acme.other", rows[0].Handle+1
			Expect(database.Create(&unregistered).Error).To(HaveOccurred(), "symbols.kind references symbol_kinds.name")
		},
		Entry("SQLite", sqliteOptions("kinds-symbols.db")),
		Entry("PostgreSQL", postgresOptions("uir_kinds_symbols")),
	)
})
