package query_test

import (
	"encoding/json"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Published callable metrics", func() {
	DescribeTable("requires producer metrics and preserves historical availability on republish", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		publication := rootPublication("example.org/metric-rules", corpusShape{rules: 1, planFields: 1})
		publication.Documents[0].Content.Version = 6
		first, err := indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		const expression = "SELECT symbol.metrics.param_count, symbol.metrics.body_lines FROM func:*"
		_, err = pipeline.RunModules(ctx, expression, query.ModuleScopeOptions{SnapshotID: first.SnapshotID})
		Expect(err).To(MatchError(And(ContainSubstring(first.SnapshotID), ContainSubstring("reindex or republish"))))
		body := 1
		publication.Documents[0].Content.Version = storage.DocumentFormatVersion
		publication.Documents[0].Content.Symbols[0].Metrics = &storage.CallableMetrics{Version: storage.MetricsVersion, ParamCount: 2, BodyLines: &body}
		encoded, err := json.Marshal(publication)
		Expect(err).NotTo(HaveOccurred())
		var decoded indexer.Publication
		Expect(json.Unmarshal(encoded, &decoded)).To(Succeed())
		second, err := indexer.Publish(ctx, database, decoded)
		Expect(err).NotTo(HaveOccurred())
		Expect(second.SnapshotID).NotTo(Equal(first.SnapshotID))
		result := runQuery(ctx, pipeline, expression, query.ModuleScopeOptions{SnapshotID: second.SnapshotID})
		Expect(result.Rows).To(Equal([]map[string]any{{"symbol.metrics.param_count": 2, "symbol.metrics.body_lines": 1}}))
		var stored storage.DeclarationMetrics
		Expect(database.Where("param_count IS NOT NULL").First(&stored).Error).To(Succeed())
		changed := 3
		stored.ParamCount = &changed
		Expect(storage.StoreDeclarationMetrics(ctx, database, []storage.DeclarationMetrics{stored})).To(MatchError(ContainSubstring("differ from the immutable document")))
		var source storage.SourceMetrics
		Expect(database.First(&source).Error).To(Succeed())
		source.Lines++
		Expect(storage.StoreSourceMetrics(ctx, database, []storage.SourceMetrics{source})).To(MatchError(ContainSubstring("without a metric version change")))
		_, err = pipeline.RunModules(ctx, expression, query.ModuleScopeOptions{SnapshotID: first.SnapshotID})
		Expect(err).To(MatchError(ContainSubstring("reindex or republish")))
		publication.Documents[0].Content.Symbols[0].Metrics.ParamCount = -1
		_, err = indexer.Publish(ctx, database, publication)
		Expect(err).To(MatchError(ContainSubstring("invalid callable metrics")))
		Expect(runQuery(ctx, pipeline, expression, query.ModuleScopeOptions{RootKey: publication.RootKey}).Rows).To(Equal(result.Rows))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
})
