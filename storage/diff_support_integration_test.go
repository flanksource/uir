package storage_test

import (
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// ownedField is a field row owned by owner.
func ownedField(owner storage.Symbol, name string) storage.Symbol {
	field := handleSymbol(serviceModule, owner.PackagePath, "field", owner.Name+"."+name, "exported")
	field.Name, field.SearchName, field.OwnerID = name, storage.SearchName(name), &owner.ID
	return field
}

var _ = Describe("diff support reads", func() {
	DescribeTable("lists active documents without content when asked and loads content, definitions, and owners on demand",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			fixture := newModuleFixture(database, "/workspace/service")
			snapshot := publishedSnapshot(fixture.location, nil, "main", 1, fixture.now)
			alpha, beta := sourceRevision(fixture.root, "alpha.go", "alpha"), sourceRevision(fixture.root, "beta.go", "beta")
			inputHash := digest("package input")
			documents := []storage.Document{syntaxDocument(alpha, inputHash), syntaxDocument(beta, inputHash)}
			createAll(database, &snapshot, &alpha, &beta, &documents[0], &documents[1],
				&storage.SourceDelta{SnapshotID: snapshot.ID, RootID: fixture.root.ID, PathKey: "alpha.go", RevisionID: &alpha.ID, Operation: storage.SourceSet},
				&storage.SourceDelta{SnapshotID: snapshot.ID, RootID: fixture.root.ID, PathKey: "beta.go", RevisionID: &beta.ID, Operation: storage.SourceSet},
				&storage.PackageCoverage{SnapshotID: snapshot.ID, RootID: fixture.root.ID, PackagePath: fixture.root.RootKey,
					InputHash: inputHash, Coverage: storage.CoverageSyntax, FileCount: 2, Diagnostics: storage.JSON(`[]`)})

			headers, err := storage.ActiveDocuments(ctx, database, snapshot.ID, storage.ActiveDocumentOptions{})
			Expect(err).ToNot(HaveOccurred())
			full, err := storage.ActiveDocuments(ctx, database, snapshot.ID, storage.ActiveDocumentOptions{Content: true})
			Expect(err).ToNot(HaveOccurred())
			Expect(documentIDs(headers)).To(Equal(documentIDs(full)))
			for path, header := range headers {
				withContent := full[path]
				Expect(withContent.Document.Content).ToNot(BeEmpty())
				withContent.Document.Content = nil
				Expect(header).To(Equal(withContent), "a header is the active document less its content")
			}

			contents, err := storage.DocumentContents(ctx, database, []uuid.UUID{documents[1].ID})
			Expect(err).ToNot(HaveOccurred())
			Expect(contents).To(HaveLen(1))
			Expect(string(contents[documents[1].ID])).To(MatchJSON(documents[1].Content), "PostgreSQL jsonb normalizes the bytes")
			missing := uuid.New()
			_, err = storage.DocumentContents(ctx, database, []uuid.UUID{documents[0].ID, missing})
			Expect(err).To(MatchError(ContainSubstring(missing.String())))

			store := handleSymbol(serviceModule, serviceModule+"/a", "type", "Store", "exported")
			inner := ownedField(store, "Inner")
			depth := ownedField(inner, "Depth")
			save := handleSymbol(serviceModule, serviceModule+"/a", "method", "Save", "exported")
			save.OwnerID = &store.ID
			rows, err := publishSymbols(ctx, database, store, inner, depth, save, runFunc)
			Expect(err).ToNot(HaveOccurred())
			handles := handlesByName(rows)
			owned, err := storage.OwnedSymbols(ctx, database, []int64{handles["Depth"], handles["Save"], handles["Run"]})
			Expect(err).ToNot(HaveOccurred())
			Expect(map[string][]string{
				"Depth": owned[handles["Depth"]].Owners, "Save": owned[handles["Save"]].Owners, "Run": owned[handles["Run"]].Owners,
			}).To(Equal(map[string][]string{"Depth": {"Store", "Inner"}, "Save": {"Store"}, "Run": nil}), "owner names run outermost first")
			Expect(owned[handles["Depth"]].Symbol).To(HaveField("ID", depth.ID))
			_, err = storage.OwnedSymbols(ctx, database, []int64{handles["Run"] + 1})
			Expect(err).To(MatchError(ContainSubstring("handle")))

			definition := storage.SymbolPosting{DocumentOrdinal: documents[0].Ordinal, RootOrdinal: fixture.root.Ordinal, SymbolHandle: handles["Save"], Role: storage.RoleDefinition, OccurrenceCount: 2}
			createAll(database, &definition,
				&storage.SymbolPosting{DocumentOrdinal: documents[0].Ordinal, RootOrdinal: fixture.root.Ordinal, SymbolHandle: handles["Store"], Role: storage.RoleReference, OccurrenceCount: 1},
				&storage.SymbolPosting{DocumentOrdinal: documents[1].Ordinal, RootOrdinal: fixture.root.Ordinal, SymbolHandle: handles["Run"], Role: storage.RoleDefinition, OccurrenceCount: 1})
			definitions, err := storage.DefinitionPostings(ctx, database, []int64{documents[0].Ordinal})
			Expect(err).ToNot(HaveOccurred())
			Expect(definitions).To(Equal([]storage.SymbolPosting{definition}), "only the requested documents' definitions")
		},
		Entry("SQLite", sqliteOptions("diff-support.db")),
		Entry("PostgreSQL", postgresOptions("uir_diff_support")),
	)
})
