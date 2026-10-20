//go:build metricbench

package query_test

import (
	"context"
	"os"
	"testing"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

// BenchmarkStoredMetrics reads an explicitly selected, already indexed real corpus without migration.
func BenchmarkStoredMetrics(b *testing.B) {
	dsn, root := os.Getenv("UIR_METRICS_BENCH_DSN"), os.Getenv("UIR_METRICS_BENCH_ROOT")
	if dsn == "" || root == "" {
		b.Fatal("UIR_METRICS_BENCH_DSN and UIR_METRICS_BENCH_ROOT are required")
	}
	ctx := context.Background()
	database, err := storage.OpenReadOnly(ctx, storage.DBOptions{DSN: dsn})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := closeDatabase(database); err != nil {
			b.Error(err)
		}
	})
	for _, sample := range []struct{ name, expression string }{
		{"files", "SELECT path, file.lines, file.definitions FROM files() WHERE file.lines < 100 AND file.definitions < 3 ORDER BY path LIMIT 1000"},
		{"callables", "SELECT symbol.name, path, line, symbol.metrics.param_count, symbol.metrics.body_lines FROM func:* WHERE symbol.metrics.param_count > 3 OR symbol.metrics.body_lines > 50 ORDER BY path, line LIMIT 1000"},
	} {
		for _, warm := range []bool{false, true} {
			temperature := "cold"
			if warm {
				temperature = "warm"
			}
			b.Run(sample.name+"/"+temperature, func(b *testing.B) {
				observed := &metricIO{}
				counted := database.Session(&gorm.Session{Logger: observed})
				pipeline, err := query.NewPipeline(counted)
				if err != nil {
					b.Fatal(err)
				}
				scope := query.ModuleScopeOptions{RootKey: root}
				if warm {
					if _, err := pipeline.RunModules(ctx, sample.expression, scope); err != nil {
						b.Fatal(err)
					}
				}
				*observed = metricIO{}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if !warm {
						pipeline, err = query.NewPipeline(counted)
					}
					if err != nil {
						b.Fatal(err)
					}
					result, err := pipeline.RunModules(ctx, sample.expression, scope)
					if err != nil {
						b.Fatal(err)
					}
					if result.Total == 0 {
						b.Fatal("real-corpus metric query matched nothing")
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(observed.statements)/float64(b.N), "sql/op")
				b.ReportMetric(float64(observed.sourceReads)/float64(b.N), "source-reads/op")
				b.ReportMetric(float64(observed.contentReads)/float64(b.N), "document-reads/op")
				if observed.sourceReads != 0 || observed.contentReads != 0 {
					b.Fatal("metric projection read sources or document content")
				}
			})
		}
	}
}
