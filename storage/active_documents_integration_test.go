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

var _ = Describe("active document membership", func() {
	DescribeTable("selects each path's document by its package input hash in the snapshot",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			now := time.Now().UTC()
			root := storage.ModuleRoot{ID: uuid.New(), RootKey: "example.org/service", Name: "service", CreatedAt: now}
			location := storage.ModuleLocation{ID: uuid.New(), RootID: root.ID, CanonicalPath: "/workspace/service", Kind: "git", CreatedAt: now}
			base := publishedSnapshot(location, nil, "main", 1, now)
			edited := publishedSnapshot(location, &base.ID, "edit", 1, now)
			revisionOnly := publishedSnapshot(location, &edited.ID, "retag", 1, now)
			alphaV1, alphaV2 := sourceRevision(root, "alpha.go", "alpha v1"), sourceRevision(root, "alpha.go", "alpha v2")
			beta := sourceRevision(root, "beta.go", "beta")
			baseHash, editedHash := digest("package input before edit"), digest("package input after edit")
			baseDocs := []storage.Document{syntaxDocument(alphaV1, baseHash), syntaxDocument(beta, baseHash)}
			editedDocs := []storage.Document{syntaxDocument(alphaV2, editedHash), syntaxDocument(beta, editedHash)}
			createAll(database, &root, &location, &base, &edited, &revisionOnly, &alphaV1, &alphaV2, &beta,
				&baseDocs[0], &baseDocs[1], &editedDocs[0], &editedDocs[1])
			createAll(database,
				&storage.SourceDelta{SnapshotID: base.ID, RootID: root.ID, PathKey: "alpha.go", RevisionID: &alphaV1.ID, Operation: storage.SourceSet},
				&storage.SourceDelta{SnapshotID: base.ID, RootID: root.ID, PathKey: "beta.go", RevisionID: &beta.ID, Operation: storage.SourceSet},
				&storage.SourceDelta{SnapshotID: edited.ID, RootID: root.ID, PathKey: "alpha.go", RevisionID: &alphaV2.ID, Operation: storage.SourceSet})
			for snapshot, hash := range map[uuid.UUID]string{base.ID: baseHash, edited.ID: editedHash, revisionOnly.ID: editedHash} {
				createAll(database, &storage.PackageCoverage{SnapshotID: snapshot, RootID: root.ID, PackagePath: root.RootKey,
					InputHash: hash, Coverage: storage.CoverageSyntax, FileCount: 2, Diagnostics: storage.JSON(`[]`)})
			}

			historical, err := storage.ActiveDocuments(ctx, database, base.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(documentIDs(historical)).To(Equal(map[string]uuid.UUID{"alpha.go": baseDocs[0].ID, "beta.go": baseDocs[1].ID}))
			current, err := storage.ActiveDocuments(ctx, database, edited.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(documentIDs(current)).To(Equal(map[string]uuid.UUID{"alpha.go": editedDocs[0].ID, "beta.go": editedDocs[1].ID}))
			reused, err := storage.ActiveDocuments(ctx, database, revisionOnly.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(documentIDs(reused)).To(Equal(documentIDs(current)))

			Expect(database.Model(&storage.PackageCoverage{}).Where("snapshot_id = ?", revisionOnly.ID).Update("input_hash", digest("never extracted")).Error).To(Succeed())
			_, err = storage.ActiveDocuments(ctx, database, revisionOnly.ID)
			Expect(err).To(MatchError(ContainSubstring(`has no document for "alpha.go"`)))
			Expect(database.Model(&storage.PackageCoverage{}).Where("snapshot_id = ?", edited.ID).Update("file_count", 1).Error).To(Succeed())
			_, err = storage.ActiveDocuments(ctx, database, edited.ID)
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
