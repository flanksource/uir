package storage_test

import (
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

var _ = Describe("source blobs", func() {
	const (
		stored   = "package stored\n"
		tampered = "package tampered\n"
		absent   = "package absent\n"
	)

	DescribeTable("reads only bytes whose SHA-256 is the content hash they are stored under",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			createAll(database,
				&storage.SourceBlob{ContentHash: digest(stored), Content: []byte(stored)},
				&storage.SourceBlob{ContentHash: digest(tampered), Content: []byte(stored)},
			)

			content, err := storage.ReadSourceBlob(ctx, database, digest(stored))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal(stored))

			_, err = storage.ReadSourceBlob(ctx, database, digest(tampered))
			Expect(err).To(MatchError(And(ContainSubstring(digest(tampered)), ContainSubstring(digest(stored)))),
				"a mismatch names the hash the blob is stored under and the hash its bytes have")
			Expect(err).ToNot(MatchError(storage.ErrSourceBlobNotStored))

			_, err = storage.ReadSourceBlob(ctx, database, digest(absent))
			Expect(err).To(MatchError(storage.ErrSourceBlobNotStored))
			Expect(err).To(MatchError(ContainSubstring(digest(absent))))
		},
		Entry("SQLite", sqliteOptions("source-blobs.db")),
		Entry("PostgreSQL", postgresOptions("uir_source_blobs")),
	)

	DescribeTable("batches generated source reads and verifies every hash", func(ctx SpecContext, options func() storage.DBOptions) {
		database := openDB(ctx, options())
		createAll(database,
			&storage.SourceBlob{ContentHash: digest(stored), Content: []byte(stored)},
			&storage.SourceBlob{ContentHash: digest(""), Content: []byte{}},
			&storage.SourceBlob{ContentHash: digest(tampered), Content: []byte(stored)},
		)
		counter := &statementCounter{}
		contents, err := storage.ReadSourceBlobs(ctx, database.Session(&gorm.Session{Logger: counter}), []string{digest(stored), digest(""), digest(stored)})
		Expect(err).NotTo(HaveOccurred())
		Expect(contents).To(Equal(map[string][]byte{digest(stored): []byte(stored), digest(""): {}}))
		Expect(counter.statements).To(HaveLen(1))
		_, err = storage.ReadSourceBlobs(ctx, database, []string{digest(stored), digest(absent)})
		Expect(err).To(MatchError(storage.ErrSourceBlobNotStored))
		Expect(err).To(MatchError(ContainSubstring(digest(absent))))
		_, err = storage.ReadSourceBlobs(ctx, database, []string{digest(stored), digest(tampered)})
		Expect(err).To(MatchError(And(ContainSubstring(digest(tampered)), ContainSubstring(digest(stored)))))
	}, Entry("SQLite", sqliteOptions("source-blob-batches.db")), Entry("PostgreSQL", postgresOptions("uir_source_blob_batches")))
})
