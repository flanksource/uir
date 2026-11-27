package storage_test

import (
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("domain catalog registration", func() {
	const producer = "acme-sql/1.0.0"

	DescribeTable("refuses a producer's property schema for a builtin kind, naming the producer and the kind",
		func(ctx SpecContext, schema string) {
			database := openDB(ctx, sqliteOptions("domain-catalog-builtin.db")())
			catalog := storage.DomainCatalog{Symbols: map[string]storage.JSON{"column": storage.JSON(schema)}}
			err := storage.RegisterDomainCatalog(ctx, database, producer, catalog)
			Expect(err).To(MatchError(`producer "` + producer + `" registers a property schema for builtin kind "column"; builtin kinds carry their own`))
			loaded, err := storage.LoadDomainCatalog(ctx, database)
			Expect(err).ToNot(HaveOccurred())
			Expect(loaded.Symbols).To(HaveKeyWithValue("column", storage.JSON(storage.BuiltinColumnProperties)))
		},
		Entry("a schema equal to the builtin", storage.BuiltinColumnProperties),
		Entry("a different schema", `{"type":"object","properties":{"width":{"type":"integer"}}}`),
	)
})
