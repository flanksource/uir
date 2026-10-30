package storage_test

import (
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// preMembershipSQL turns a current database into one published before document membership: no document
// deltas or cutover marker, no content digest, and documents keyed (root, path, input hash).
var preMembershipSQL = map[string][]string{
	"sqlite": {
		"DROP TABLE document_deltas", "DROP TABLE document_membership",
		"DROP INDEX documents_root_id_path_key_content_digest_key", "DROP INDEX documents_root_id_path_key_ordinal_key",
		"DROP INDEX snapshots_root_id_ordinal_key", "ALTER TABLE documents DROP COLUMN content_digest",
		"CREATE UNIQUE INDEX documents_root_id_path_key_input_hash_key ON documents (root_id, path_key, input_hash)",
	},
	"postgres": {
		"DROP TABLE document_deltas", "DROP TABLE document_membership", "ALTER TABLE documents DROP COLUMN content_digest",
		"ALTER TABLE documents DROP CONSTRAINT IF EXISTS documents_root_id_path_key_ordinal_key", "DROP INDEX IF EXISTS documents_root_id_path_key_ordinal_key",
		"ALTER TABLE snapshots DROP CONSTRAINT IF EXISTS snapshots_root_id_ordinal_key", "DROP INDEX IF EXISTS snapshots_root_id_ordinal_key",
		"CREATE UNIQUE INDEX documents_root_id_path_key_input_hash_key ON documents (root_id, path_key, input_hash)",
	},
}

// legacyDocument is a document as a pre-membership publication wrote it, without a content digest.
func legacyDocument(revision storage.SourceRevision, inputHash string) storage.Document {
	GinkgoHelper()
	document := syntaxDocument(revision, inputHash)
	document.ContentDigest = nil
	return document
}

func activeIDs(database *gorm.DB, snapshots ...storage.ModuleSnapshot) map[string]map[string]uuid.UUID {
	GinkgoHelper()
	result := map[string]map[string]uuid.UUID{}
	for _, snapshot := range snapshots {
		active, err := storage.ActiveDocuments(GinkgoT().Context(), database, snapshot.ID, storage.ActiveDocumentOptions{})
		Expect(err).ToNot(HaveOccurred(), snapshot.Revision)
		result[snapshot.Revision] = documentIDs(active)
	}
	return result
}

var _ = Describe("document membership cutover", func() {
	DescribeTable("backfills the membership package input hashes selected, as full sets for roots and differences for descendants",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			database := openDB(ctx, config)
			fixture := newModuleFixture(database, "/workspace/service")
			base := publishedSnapshot(fixture.location, nil, "base", 1, fixture.now)
			rekeyed := publishedSnapshot(fixture.location, &base.ID, "rekeyed", 1, fixture.now)
			repeat := publishedSnapshot(fixture.location, &rekeyed.ID, "repeat", 1, fixture.now)
			trimmed := publishedSnapshot(fixture.location, &repeat.ID, "trimmed", 1, fixture.now)
			alphaV1, alphaV2 := sourceRevision(fixture.root, "alpha.go", "alpha v1"), sourceRevision(fixture.root, "alpha.go", "alpha v2")
			beta := sourceRevision(fixture.root, "beta.go", "beta")
			first, second, third := digest("first package input"), digest("second package input"), digest("third package input")
			alpha1, beta1 := legacyDocument(alphaV1, first), legacyDocument(beta, first)
			alpha2, beta2, alpha3 := legacyDocument(alphaV2, second), legacyDocument(beta, second), legacyDocument(alphaV2, third)
			createAll(database, &base, &rekeyed, &repeat, &trimmed, &alphaV1, &alphaV2, &beta, &alpha1, &beta1, &alpha2, &beta2, &alpha3,
				setSource(base, alphaV1), setSource(base, beta), setSource(rekeyed, alphaV2),
				&storage.SourceDelta{SnapshotID: trimmed.ID, RootID: fixture.root.ID, PathKey: "beta.go", Operation: storage.SourceDelete},
				coverage(base, first, 2), coverage(rekeyed, second, 2), coverage(repeat, second, 2), coverage(trimmed, third, 1))
			for _, statement := range preMembershipSQL[database.Name()] {
				Expect(database.Exec(statement).Error).To(Succeed(), statement)
			}
			_, err := storage.OpenReadOnly(ctx, config)
			Expect(err).To(MatchError(storage.ErrDocumentMembershipCutover))

			cutover := openDB(ctx, config)
			Expect(activeIDs(cutover, base, rekeyed, repeat, trimmed)).To(Equal(map[string]map[string]uuid.UUID{
				"base":    {"alpha.go": alpha1.ID, "beta.go": beta1.ID},
				"rekeyed": {"alpha.go": alpha2.ID, "beta.go": beta2.ID},
				"repeat":  {"alpha.go": alpha2.ID, "beta.go": beta2.ID},
				"trimmed": {"alpha.go": alpha3.ID},
			}), "every snapshot keeps the documents its package input hashes selected")
			var deltas []storage.DocumentDelta
			Expect(cutover.Order("snapshot_ordinal, path_key").Find(&deltas).Error).To(Succeed())
			bound := map[string][]string{}
			for _, delta := range deltas {
				for revision, snapshot := range map[string]storage.ModuleSnapshot{"base": base, "rekeyed": rekeyed, "repeat": repeat, "trimmed": trimmed} {
					if snapshot.Ordinal == delta.SnapshotOrdinal {
						bound[revision] = append(bound[revision], string(delta.Operation)+" "+delta.PathKey)
					}
				}
			}
			Expect(bound).To(Equal(map[string][]string{
				"base": {"set alpha.go", "set beta.go"}, "rekeyed": {"set alpha.go", "set beta.go"}, "trimmed": {"set alpha.go", "delete beta.go"},
			}))
			var digests []storage.Document
			Expect(cutover.Select("id", "content_digest").Order("ordinal").Find(&digests).Error).To(Succeed())
			digested := map[uuid.UUID]bool{}
			for _, document := range digests {
				digested[document.ID] = document.ContentDigest != nil
			}
			Expect(digested).To(Equal(map[uuid.UUID]bool{alpha1.ID: true, beta1.ID: true, alpha2.ID: true, beta2.ID: false, alpha3.ID: false}),
				"a byte-identical republished document leaves the digest to the earliest one of its path")

			readOnly, err := storage.OpenReadOnly(ctx, config)
			Expect(err).ToNot(HaveOccurred())
			Expect(activeIDs(readOnly, trimmed)).To(Equal(activeIDs(openDB(ctx, config), trimmed)), "a reopened database runs no second cutover")
		},
		Entry("SQLite", sqliteOptions("pre-membership.db")),
		Entry("PostgreSQL", postgresOptions("uir_membership_cutover")),
	)
})
