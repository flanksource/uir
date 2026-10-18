package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm/logger"
)

type sqlGroup struct {
	Label   string  `json:"label"`
	Count   int     `json:"count"`
	TotalMS float64 `json:"total_ms"`
	MaxMS   float64 `json:"max_ms"`
}

type sqlProfile struct {
	Count   int        `json:"count"`
	TotalMS float64    `json:"total_ms"`
	MaxMS   float64    `json:"max_ms"`
	Groups  []sqlGroup `json:"groups"`
}

type sqlRecorder struct {
	base   logger.Interface
	mu     sync.Mutex
	result sqlProfile
	groups map[string]*sqlGroup
}

func newSQLRecorder(base logger.Interface) *sqlRecorder {
	return &sqlRecorder{base: base, groups: map[string]*sqlGroup{}}
}

func (record *sqlRecorder) LogMode(level logger.LogLevel) logger.Interface {
	record.base = record.base.LogMode(level)
	return record
}

func (record *sqlRecorder) Info(ctx context.Context, message string, args ...interface{}) {
	record.base.Info(ctx, message, args...)
}

func (record *sqlRecorder) Warn(ctx context.Context, message string, args ...interface{}) {
	record.base.Warn(ctx, message, args...)
}

func (record *sqlRecorder) Error(ctx context.Context, message string, args ...interface{}) {
	record.base.Error(ctx, message, args...)
}

func (record *sqlRecorder) Trace(_ context.Context, begin time.Time, sql func() (string, int64), _ error) {
	elapsed := float64(time.Since(begin).Microseconds()) / 1000
	query, _ := sql()
	label := sqlLabel(query)
	record.mu.Lock()
	defer record.mu.Unlock()
	record.result.Count++
	record.result.TotalMS += elapsed
	record.result.MaxMS = max(record.result.MaxMS, elapsed)
	group := record.groups[label]
	if group == nil {
		group = &sqlGroup{Label: label}
		record.groups[label] = group
	}
	group.Count++
	group.TotalMS += elapsed
	group.MaxMS = max(group.MaxMS, elapsed)
}

func (record *sqlRecorder) snapshot() sqlProfile {
	record.mu.Lock()
	defer record.mu.Unlock()
	result := record.result
	for _, group := range record.groups {
		result.Groups = append(result.Groups, *group)
	}
	sort.Slice(result.Groups, func(i, j int) bool { return result.Groups[i].TotalMS > result.Groups[j].TotalMS })
	return result
}

func sqlLabel(query string) string {
	words := strings.Fields(query)
	if len(words) == 0 {
		return "UNKNOWN"
	}
	verb := strings.ToUpper(words[0])
	if verb == "WITH" {
		return "WITH"
	}
	for i := 1; i+1 < len(words); i++ {
		keyword := strings.ToUpper(words[i])
		if keyword == "FROM" || keyword == "INTO" {
			return fmt.Sprintf("%s %s", verb, strings.Trim(words[i+1], "\"`(),;"))
		}
	}
	if verb == "UPDATE" && len(words) > 1 {
		return fmt.Sprintf("UPDATE %s", strings.Trim(words[1], "\"`(),;"))
	}
	return verb
}
