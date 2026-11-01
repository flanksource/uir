package storage_test

import (
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("document digest", func() {
	revision := uuid.MustParse("5a0c7e52-1b8f-4f43-9d67-0d1c2b3a4f50")
	rendered := storage.Document{
		SourceRevisionID: revision, PackagePath: workerPackage, IndexerVersion: "go-types-v5", Coverage: storage.CoverageIndexed,
		InputHash: digest("first input"), Content: storage.JSON(`{"version":6,"package_path":"example.org/acme/worker","symbols":[{"key":"a<b","props":{"z":1,"a":1.5}}]}`),
	}
	stored := func(change func(*storage.Document)) storage.Document {
		document := rendered
		change(&document)
		return document
	}

	DescribeTable("keys a document by its revision, rendering, and canonical content, not by its package input hash",
		func(other storage.Document, same bool) {
			expected, err := storage.DocumentDigest(rendered)
			Expect(err).ToNot(HaveOccurred())
			Expect(expected).To(HaveLen(64))
			actual, err := storage.DocumentDigest(other)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual == expected).To(Equal(same))
		},
		Entry("another package input hash", stored(func(document *storage.Document) { document.InputHash = digest("second input") }), true),
		Entry("the key order and spacing PostgreSQL jsonb stores", stored(func(document *storage.Document) {
			document.Content = storage.JSON(`{"symbols": [{"props": {"a": 1.5, "z": 1}, "key": "a<b"}], "version": 6, "package_path": "example.org/acme/worker"}`)
		}), true),
		Entry("another source revision", stored(func(document *storage.Document) { document.SourceRevisionID = uuid.New() }), false),
		Entry("another coverage", stored(func(document *storage.Document) { document.Coverage = storage.CoveragePartial }), false),
		Entry("another indexer version", stored(func(document *storage.Document) { document.IndexerVersion = "go-types-v6" }), false),
		Entry("another content", stored(func(document *storage.Document) {
			document.Content = storage.JSON(`{"version":6,"package_path":"example.org/acme/worker","symbols":[{"key":"a<b","props":{"z":2,"a":1.5}}]}`)
		}), false),
	)

	It("rejects content that is not JSON", func() {
		_, err := storage.DocumentDigest(stored(func(document *storage.Document) { document.Content = storage.JSON(`{"version":`) }))
		Expect(err).To(MatchError(ContainSubstring("canonicalize")))
	})
})
