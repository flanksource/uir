package query_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// cacheQueries cover a scoped relation, a scoped intersection, a call relation, and a set of symbols.
var cacheQueries = []string{
	`Plan:SchemeNumber < mod:"Co/Product01/..."`,
	`field:* & mod:"Co/Product02/..."`,
	"kind:test.rule:Rule003 <",
	"Plan:* ~w (pkg:Co/Product00/rules | pkg:Co/Product03/Plan/rules)",
}

// sqlStatements is how many SQL statements the query's sql stage counts.
func sqlStatements(result query.ModuleQueryResult) int {
	GinkgoHelper()
	for _, stage := range result.Stages {
		if stage.Name == "sql" {
			var statements int
			_, err := fmt.Sscanf(stage.Value, "%d statements", &statements)
			Expect(err).ToNot(HaveOccurred(), stage.Value)
			return statements
		}
	}
	Fail("the query reports no sql stage")
	return 0
}

func answers(ctx context.Context, pipeline *query.Pipeline) []queryRows {
	GinkgoHelper()
	var rows []queryRows
	for _, expression := range cacheQueries {
		rows = append(rows, rowsOf(runQuery(ctx, pipeline, expression, query.ModuleScopeOptions{Limit: 1000})))
	}
	return rows
}

func newPipeline(database *gorm.DB, options ...query.PipelineOption) *query.Pipeline {
	GinkgoHelper()
	pipeline, err := query.NewPipeline(database, options...)
	Expect(err).ToNot(HaveOccurred())
	return pipeline
}

var _ = Describe("a pipeline serving many queries", func() {
	var database *gorm.DB
	var path string
	BeforeEach(func(ctx SpecContext) {
		path = filepath.Join(GinkgoT().TempDir(), "corpus.db")
		var err error
		database, err = storage.UirDB(ctx, storage.DBOptions{DSN: path})
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { Expect(closeDatabase(database)).To(Succeed()) })
		Expect(publishCorpus(ctx, database, smallCorpus)).To(Succeed())
	})

	It("answers a repeated query from what it kept, with fewer statements", func(ctx SpecContext) {
		pipeline := newPipeline(database)
		expression := cacheQueries[0]
		cold := runQuery(ctx, pipeline, expression, query.ModuleScopeOptions{})
		warm := runQuery(ctx, pipeline, expression, query.ModuleScopeOptions{})
		Expect(rowsOf(warm)).To(Equal(rowsOf(cold)))
		Expect(cold.Total).To(Equal(6))
		Expect(sqlStatements(warm)).To(BeNumerically("<", sqlStatements(cold)/2))
	})

	It("answers from the new head after a publication into the same database", func(ctx SpecContext) {
		pipeline := newPipeline(database)
		expression := "Plan:SchemeNumber < mod:Co/Product00"
		Expect(runQuery(ctx, pipeline, expression, query.ModuleScopeOptions{}).Total).To(Equal(3))
		grown := smallCorpus
		grown.rules += 2
		_, err := indexer.Publish(ctx, database, rootPublication(corpusProduct(0), grown))
		Expect(err).ToNot(HaveOccurred())
		Expect(runQuery(ctx, pipeline, expression, query.ModuleScopeOptions{}).Total).To(Equal(4), "Rule006 reads SchemeNumber too")
	})

	It("answers the same with no document cache and with one smaller than a query reads", func(ctx SpecContext) {
		expected := answers(ctx, newPipeline(database))
		for _, size := range []int{0, 1} {
			pipeline := newPipeline(database, query.WithDocumentCache(size))
			Expect(answers(ctx, pipeline)).To(Equal(expected), "document cache of %d", size)
			Expect(answers(ctx, pipeline)).To(Equal(expected), "repeated with a document cache of %d", size)
		}
	})

	It("answers concurrent queries as it answers them one at a time", func(ctx SpecContext) {
		expected := answers(ctx, newPipeline(database))
		pipeline := newPipeline(database, query.WithDocumentCache(8))
		var wait sync.WaitGroup
		failures := make(chan error, 8*len(cacheQueries))
		for worker := range 8 {
			wait.Go(func() {
				for offset := range cacheQueries {
					position := (worker + offset) % len(cacheQueries)
					result, err := pipeline.RunModules(ctx, cacheQueries[position], query.ModuleScopeOptions{Limit: 1000})
					if err != nil {
						failures <- err
						continue
					}
					if rows := rowsOf(result); !reflect.DeepEqual(rows, expected[position]) {
						failures <- fmt.Errorf("worker %d: %s answered %d rows, alone %d", worker, cacheQueries[position], rows.Total, expected[position].Total)
					}
				}
			})
		}
		wait.Wait()
		close(failures)
		var errors []string
		for err := range failures {
			errors = append(errors, err.Error())
		}
		Expect(errors).To(BeEmpty())
	})

	It("queries a database opened read-only", func(ctx SpecContext) {
		expected := answers(ctx, newPipeline(database))
		reader, err := storage.OpenReadOnly(ctx, storage.DBOptions{DSN: path})
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { Expect(closeDatabase(reader)).To(Succeed()) })
		Expect(answers(ctx, newPipeline(reader))).To(Equal(expected))
	})

	It("reports the statements at least as slow as its threshold", func(ctx SpecContext) {
		var logged []string
		pipeline := newPipeline(database, query.WithSlowQueryLog(time.Nanosecond, func(format string, arguments ...any) {
			logged = append(logged, fmt.Sprintf(format, arguments...))
		}))
		result := runQuery(ctx, pipeline, cacheQueries[0], query.ModuleScopeOptions{})
		Expect(logged).To(HaveLen(sqlStatements(result)))
		Expect(strings.Join(logged, "\n")).To(ContainSubstring("FROM `location_heads`"))
	})

	DescribeTable("refuses invalid options", func(option query.PipelineOption, message string) {
		_, err := query.NewPipeline(database, option)
		Expect(err).To(MatchError(ContainSubstring(message)))
	},
		Entry("a negative document cache", query.WithDocumentCache(-1), "must not be negative"),
		Entry("a slow query log without a threshold", query.WithSlowQueryLog(0, func(string, ...any) {}), "threshold must be positive"),
		Entry("a slow query threshold without a log", query.WithSlowQueryLog(time.Second, nil), "requires a log function"),
	)
})

func closeDatabase(database *gorm.DB) error {
	sqlDB, err := database.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
