package query_test

import (
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL navigation composition", func() {
	It("feeds the symbols a native traverse reaches into references and returns their use sites", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		publication := domainPublication()
		handle, consume := publication.Symbols[1].Identity, publication.Symbols[3].Identity
		source := publishedText("Consume\n")
		consumer := &publication.Documents[3]
		consumer.Content.Occurrences = append(consumer.Content.Occurrences, source.use(handle, "reference", "Consume\n", "Consume", consume))
		_, err := indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{RootKey: publication.RootKey}
		result := runQuery(ctx, pipeline, "SELECT path, line FROM references(traverse(http.route:Orders, via => 'http.handled_by', depth => 1))", scope)
		Expect(result.Rows).To(Equal([]map[string]any{{"path": "Consume.txt", "line": 1}}))
	})
})
