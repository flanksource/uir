package storage_test

import (
	"path/filepath"
	"time"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func createAll(database *gorm.DB, rows ...any) {
	GinkgoHelper()
	for _, row := range rows {
		Expect(database.Create(row).Error).To(Succeed())
	}
}

func documentIDs(active map[string]storage.ActiveDocument) map[string]uuid.UUID {
	ids := make(map[string]uuid.UUID, len(active))
	for path, document := range active {
		Expect(document.Document.PathKey).To(Equal(path))
		Expect(document.Document.SourceRevisionID).To(Equal(document.Source.ID))
		ids[path] = document.Document.ID
	}
	return ids
}

func setSource(snapshot storage.ModuleSnapshot, revision storage.SourceRevision) *storage.SourceDelta {
	return &storage.SourceDelta{SnapshotID: snapshot.ID, RootID: snapshot.RootID, PathKey: revision.PathKey, RevisionID: &revision.ID, Operation: storage.SourceSet}
}

func coverage(snapshot storage.ModuleSnapshot, inputHash string, files int) *storage.PackageCoverage {
	return &storage.PackageCoverage{SnapshotID: snapshot.ID, RootID: snapshot.RootID, PackagePath: "example.org/service",
		InputHash: inputHash, Coverage: storage.CoverageSyntax, FileCount: files, Diagnostics: storage.JSON(`[]`)}
}

var _ = Describe("active document membership", func() {
	DescribeTable("resolves each path's document from the newest document delta along the base chain",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			now := time.Now().UTC()
			root := storage.ModuleRoot{ID: uuid.New(), RootKey: "example.org/service", Name: "service", CreatedAt: now, Ordinal: 1}
			location := storage.ModuleLocation{ID: uuid.New(), RootID: root.ID, CanonicalPath: "/workspace/service", Kind: "git", CreatedAt: now}
			base := publishedSnapshot(location, nil, "main", 1, now)
			edited := publishedSnapshot(location, &base.ID, "edit", 1, now)
			rekeyed := publishedSnapshot(location, &edited.ID, "dependency bump", 1, now)
			trimmed := publishedSnapshot(location, &rekeyed.ID, "delete", 1, now)
			alphaV1, alphaV2 := sourceRevision(root, "alpha.go", "alpha v1"), sourceRevision(root, "alpha.go", "alpha v2")
			beta := sourceRevision(root, "beta.go", "beta")
			baseHash, editedHash := digest("package input before edit"), digest("package input after edit")
			alphaV1Doc, alphaV2Doc, betaDoc := syntaxDocument(alphaV1, baseHash), syntaxDocument(alphaV2, editedHash), syntaxDocument(beta, baseHash)
			createAll(database, &root, &location, &base, &edited, &rekeyed, &trimmed, &alphaV1, &alphaV2, &beta, &alphaV1Doc, &alphaV2Doc, &betaDoc,
				setSource(base, alphaV1), setSource(base, beta), setSource(edited, alphaV2),
				&storage.SourceDelta{SnapshotID: trimmed.ID, RootID: root.ID, PathKey: "beta.go", Operation: storage.SourceDelete},
				coverage(base, baseHash, 2), coverage(edited, editedHash, 2), coverage(rekeyed, digest("package input after bump"), 2), coverage(trimmed, editedHash, 1))
			bindDocuments(database, base, alphaV1Doc, betaDoc)
			bindDocuments(database, edited, alphaV2Doc)
			createAll(database, &storage.DocumentDelta{SnapshotOrdinal: trimmed.Ordinal, RootID: root.ID, PathKey: "beta.go", Operation: storage.SourceDelete})

			membership := map[uuid.UUID]map[string]uuid.UUID{}
			for _, snapshot := range []storage.ModuleSnapshot{base, edited, rekeyed, trimmed} {
				active, err := storage.ActiveDocuments(ctx, database, snapshot.ID, storage.ActiveDocumentOptions{Content: true})
				Expect(err).ToNot(HaveOccurred(), snapshot.Revision)
				membership[snapshot.ID] = documentIDs(active)
			}
			Expect(membership).To(Equal(map[uuid.UUID]map[string]uuid.UUID{
				base.ID:    {"alpha.go": alphaV1Doc.ID, "beta.go": betaDoc.ID},
				edited.ID:  {"alpha.go": alphaV2Doc.ID, "beta.go": betaDoc.ID},
				rekeyed.ID: {"alpha.go": alphaV2Doc.ID, "beta.go": betaDoc.ID},
				trimmed.ID: {"alpha.go": alphaV2Doc.ID},
			}), "a document is bound by its delta, whatever input hash its package records")

			foreign := sourceRevision(root, "gamma.go", "gamma")
			foreignDoc := syntaxDocument(foreign, editedHash)
			createAll(database, &foreign, &foreignDoc)
			misbound := storage.DocumentDelta{SnapshotOrdinal: trimmed.Ordinal, RootID: root.ID, PathKey: "alpha.go", DocumentOrdinal: &foreignDoc.Ordinal, Operation: storage.SourceSet}
			Expect(database.Create(&misbound).Error).To(HaveOccurred(), "a delta binds only a document of its own path")
			Expect(database.Create(&storage.DocumentDelta{SnapshotOrdinal: trimmed.Ordinal, RootID: root.ID, PathKey: "alpha.go", Operation: storage.SourceSet}).Error).
				To(HaveOccurred(), "a set binds a document")

			createAll(database, &storage.DocumentDelta{SnapshotOrdinal: rekeyed.Ordinal, RootID: root.ID, PathKey: "alpha.go", DocumentOrdinal: &alphaV1Doc.Ordinal, Operation: storage.SourceSet})
			_, err := storage.ActiveDocuments(ctx, database, rekeyed.ID, storage.ActiveDocumentOptions{})
			Expect(err).To(MatchError(ContainSubstring("the snapshot has revision " + alphaV2.ID.String())))
			createAll(database, &storage.DocumentDelta{SnapshotOrdinal: edited.Ordinal, RootID: root.ID, PathKey: "beta.go", Operation: storage.SourceDelete})
			_, err = storage.ActiveDocuments(ctx, database, edited.ID, storage.ActiveDocumentOptions{})
			Expect(err).To(MatchError(ContainSubstring(`has no document for "beta.go"`)))
			Expect(database.Model(&storage.PackageCoverage{}).Where("snapshot_id = ?", base.ID).Update("file_count", 1).Error).To(Succeed())
			_, err = storage.ActiveDocuments(ctx, database, base.ID, storage.ActiveDocumentOptions{})
			Expect(err).To(MatchError(ContainSubstring("records 1 files, effective sources have 2")))
		},
		Entry("SQLite", func() storage.DBOptions {
			return storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "membership.db")}
		}),
		Entry("PostgreSQL", func() storage.DBOptions {
			return storage.DBOptions{DSN: dbtest.ForGinkgo(dbtest.Options{Name: "uir_membership"}).DSN(), Schema: "uir_membership"}
		}),
	)
})
