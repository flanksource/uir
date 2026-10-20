package query_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var metricContentColumn = regexp.MustCompile(`(?i)(^|[^[:alnum:]_])content([^[:alnum:]_]|$)`)

type metricIO struct {
	statements, sourceReads, contentReads int
}

func (observed *metricIO) LogMode(logger.LogLevel) logger.Interface { return observed }
func (*metricIO) Info(context.Context, string, ...any)              {}
func (*metricIO) Warn(context.Context, string, ...any)              {}
func (*metricIO) Error(context.Context, string, ...any)             {}
func (observed *metricIO) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	statement, _ := sql()
	observed.statements++
	statement = strings.ToLower(statement)
	if !strings.HasPrefix(strings.TrimSpace(statement), "select") {
		return
	}
	if strings.Contains(statement, "source_blobs") {
		observed.sourceReads++
	}
	projection, _, _ := strings.Cut(statement, " from ")
	if strings.Contains(statement, "documents") && (metricContentColumn.MatchString(projection) || strings.TrimSpace(projection) == "select *") {
		observed.contentReads++
	}
}

var _ = Describe("Metric query IO", func() {
	DescribeTable("answers cold and warm queries without sources or document content", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		checkout := metricCheckout()
		indexed := indexCheckout(ctx, database, checkout)
		Expect(os.RemoveAll(checkout)).To(Succeed())
		Expect(database.Exec("DELETE FROM source_blobs").Error).To(Succeed())
		observed := &metricIO{}
		pipeline, err := query.NewPipeline(database.Session(&gorm.Session{Logger: observed}))
		Expect(err).NotTo(HaveOccurred())
		for range 2 {
			files := runQuery(ctx, pipeline, "SELECT path, file.lines, file.definitions FROM files() WHERE file.lines < 100 AND file.definitions < 3 ORDER BY path", query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID})
			Expect(files.Total).To(Equal(4))
			callables := runQuery(ctx, pipeline, "SELECT symbol.name, path, line, symbol.metrics.param_count, symbol.metrics.body_lines FROM func:* WHERE symbol.metrics.param_count > 3 OR symbol.metrics.body_lines > 50", query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID})
			Expect(callables.Rows).To(Equal([]map[string]any{{"symbol.name": "Large", "path": "large.go", "line": 2, "symbol.metrics.param_count": 4, "symbol.metrics.body_lines": 4}}))
		}
		Expect(observed.statements).To(BeNumerically(">", 0))
		Expect([]int{observed.sourceReads, observed.contentReads}).To(Equal([]int{0, 0}))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
})
