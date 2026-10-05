package query

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DefaultDocumentCache is how many decoded documents a pipeline keeps across queries unless
// WithDocumentCache sets another size.
const DefaultDocumentCache = 4096

// PipelineOption configures NewPipeline.
type PipelineOption func(*pipelineOptions)

type pipelineOptions struct {
	documents int
	slow      time.Duration
	logf      func(format string, arguments ...any)
}

// WithDocumentCache keeps up to size decoded documents, least recently used first out, across the
// queries of one pipeline. A document is immutable once published, so a cached one never goes stale;
// zero keeps none, and each query still decodes a document at most once.
func WithDocumentCache(size int) PipelineOption {
	return func(options *pipelineOptions) { options.documents = size }
}

// WithSlowQueryLog reports, through logf, every SQL statement a query runs that takes threshold or
// longer, with its text and row count.
func WithSlowQueryLog(threshold time.Duration, logf func(format string, arguments ...any)) PipelineOption {
	return func(options *pipelineOptions) { options.slow, options.logf = threshold, logf }
}

func (options pipelineOptions) validate() error {
	switch {
	case options.documents < 0:
		return fmt.Errorf("UIR query document cache size must not be negative, got %d", options.documents)
	case options.logf != nil && options.slow <= 0:
		return fmt.Errorf("UIR slow query threshold must be positive, got %s", options.slow)
	case options.logf == nil && options.slow != 0:
		return errors.New("UIR slow query log requires a log function")
	}
	return nil
}

// sqlTrace counts the statements one query runs and their time, and reports the slow ones. It wraps
// the database's own logger, which still sees every statement.
type sqlTrace struct {
	base       logger.Interface
	statements atomic.Int64
	elapsed    atomic.Int64
	slow       time.Duration
	logf       func(format string, arguments ...any)
}

func (trace *sqlTrace) LogMode(level logger.LogLevel) logger.Interface {
	return &sqlTrace{base: trace.base.LogMode(level), slow: trace.slow, logf: trace.logf}
}

func (trace *sqlTrace) Info(ctx context.Context, message string, data ...any) {
	trace.base.Info(ctx, message, data...)
}

func (trace *sqlTrace) Warn(ctx context.Context, message string, data ...any) {
	trace.base.Warn(ctx, message, data...)
}

func (trace *sqlTrace) Error(ctx context.Context, message string, data ...any) {
	trace.base.Error(ctx, message, data...)
}

func (trace *sqlTrace) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	trace.statements.Add(1)
	trace.elapsed.Add(int64(elapsed))
	if trace.logf != nil && elapsed >= trace.slow {
		sql, rows := fc()
		trace.logf("uir query: slow SQL (%s, %d rows): %s", elapsed.Round(time.Microsecond), rows, sql)
	}
	trace.base.Trace(ctx, begin, fc, err)
}

// session is the pipeline one query runs on: the same options and cache over a database session whose
// logger counts the query's statements.
func (pipeline *Pipeline) session() (*Pipeline, *sqlTrace) {
	trace := &sqlTrace{base: pipeline.database.Logger, slow: pipeline.options.slow, logf: pipeline.options.logf}
	return &Pipeline{
		database: pipeline.database.Session(&gorm.Session{Logger: trace}), options: pipeline.options, cache: pipeline.cache, query: true,
	}, trace
}

// stageClock times the phases of one query as timing stages: one per phase, then the total and the
// SQL the query ran.
type stageClock struct {
	started, last time.Time
	phases        []string
}

func newStageClock() *stageClock {
	now := time.Now()
	return &stageClock{started: now, last: now}
}

// lap ends the current phase, naming it.
func (clock *stageClock) lap(phase string) {
	now := time.Now()
	clock.phases = append(clock.phases, phase+" "+roundElapsed(now.Sub(clock.last)))
	clock.last = now
}

// stages are the timing stages of the phases lapped so far, the total, and the SQL statements traced.
func (clock *stageClock) stages(trace *sqlTrace) []ResolutionStage {
	phases := slices.Concat(clock.phases, []string{"total " + roundElapsed(time.Since(clock.started))})
	return []ResolutionStage{
		{Name: "timing", Value: strings.Join(phases, ", ")},
		{Name: "sql", Value: fmt.Sprintf("%d statements in %s", trace.statements.Load(), roundElapsed(time.Duration(trace.elapsed.Load())))},
	}
}

func roundElapsed(elapsed time.Duration) string {
	if elapsed < 10*time.Millisecond {
		return elapsed.Round(10 * time.Microsecond).String()
	}
	return elapsed.Round(time.Millisecond).String()
}
