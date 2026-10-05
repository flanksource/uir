package query_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
)

// latencyCorpus has the shape of a lab catalog: 22 products of two roots and the entities root, 45
// roots in all, with 160 documents of their own package and some 1,000 symbols each.
var latencyCorpus = corpusShape{products: 22, rules: 100, screens: 60, screenFields: 15, planFields: 10, packagePerDocument: true}

// latencyQueries are what a fields page sends: the readers of one field within one product's tree,
// and the screen fields of that tree.
var latencyQueries = []struct{ name, expression string }{
	{"scoped-readers", `Plan:SchemeNumber < mod:"Co/Product07/..."`},
	{"scoped-fields", `field:* & mod:"Co/Product07/..."`},
}

// BenchmarkScopedQueries times the latency queries on a pipeline that has kept nothing (cold) and on
// one that has answered the query before (warm). It reports the SQL statements of a query as sql/op
// when the pipeline counts them.
func BenchmarkScopedQueries(b *testing.B) {
	ctx := context.Background()
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: filepath.Join(b.TempDir(), "latency.db")})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := closeDatabase(database); err != nil {
			b.Error(err)
		}
	})
	if err := publishCorpus(ctx, database, latencyCorpus); err != nil {
		b.Fatal(err)
	}
	for _, latency := range latencyQueries {
		b.Run("cold/"+latency.name, func(b *testing.B) {
			for b.Loop() {
				pipeline, err := query.NewPipeline(database)
				if err != nil {
					b.Fatal(err)
				}
				reportQuery(ctx, b, pipeline, latency.expression)
			}
		})
		b.Run("warm/"+latency.name, func(b *testing.B) {
			pipeline, err := query.NewPipeline(database)
			if err != nil {
				b.Fatal(err)
			}
			reportQuery(ctx, b, pipeline, latency.expression)
			for b.Loop() {
				reportQuery(ctx, b, pipeline, latency.expression)
			}
		})
	}
}

func reportQuery(ctx context.Context, b *testing.B, pipeline *query.Pipeline, expression string) {
	result, err := pipeline.RunModules(ctx, expression, query.ModuleScopeOptions{Limit: 1000})
	if err != nil {
		b.Fatal(err)
	}
	if result.Total == 0 {
		b.Fatalf("%s matched nothing", expression)
	}
	for _, stage := range result.Stages {
		var statements int
		if _, err := fmt.Sscanf(stage.Value, "%d statements", &statements); stage.Name == "sql" && err == nil {
			b.ReportMetric(float64(statements), "sql/op")
		}
	}
}
