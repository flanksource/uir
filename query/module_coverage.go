package query

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
)

// ModuleCoverage is one package in a queried snapshot whose facts are not fully proven: `partial`
// (type errors; only proven facts recorded), `syntax` (no type information, no postings), or
// `excluded` (not extracted).
type ModuleCoverage struct {
	RootKey     string `json:"root_key"`
	Location    string `json:"location"`
	SnapshotID  string `json:"snapshot_id"`
	PackagePath string `json:"package_path"`
	Coverage    string `json:"coverage"`
	Diagnostics int    `json:"diagnostics"`
}

var coverageOrder = []storage.Coverage{storage.CoveragePartial, storage.CoverageSyntax, storage.CoverageExcluded}

// scopeCoverage lists, for every snapshot in scope, the packages whose coverage is not `indexed`.
func (pipeline *Pipeline) scopeCoverage(ctx context.Context, scopes []moduleScope) ([]ModuleCoverage, error) {
	bySnapshot := make(map[uuid.UUID]moduleScope, len(scopes))
	ids := make([]uuid.UUID, 0, len(scopes))
	for _, scope := range scopes {
		bySnapshot[scope.snapshot.ID] = scope
		ids = append(ids, scope.snapshot.ID)
	}
	coverage := []ModuleCoverage{}
	if len(ids) == 0 {
		return coverage, nil
	}
	var rows []storage.PackageCoverage
	if err := pipeline.database.WithContext(ctx).Where("snapshot_id IN ? AND coverage <> ?", ids, storage.CoverageIndexed).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load package coverage for %d snapshots: %w", len(ids), err)
	}
	for _, row := range rows {
		scope := bySnapshot[row.SnapshotID]
		var diagnostics []json.RawMessage
		if err := json.Unmarshal(row.Diagnostics, &diagnostics); err != nil {
			return nil, fmt.Errorf("decode diagnostics of package %s in snapshot %s: %w", row.PackagePath, row.SnapshotID, err)
		}
		coverage = append(coverage, ModuleCoverage{
			RootKey: scope.root.RootKey, Location: scope.location.CanonicalPath, SnapshotID: row.SnapshotID.String(),
			PackagePath: row.PackagePath, Coverage: string(row.Coverage), Diagnostics: len(diagnostics),
		})
	}
	sort.Slice(coverage, func(i, j int) bool {
		left, right := coverage[i], coverage[j]
		return left.RootKey+"\x00"+left.Location+"\x00"+left.PackagePath < right.RootKey+"\x00"+right.Location+"\x00"+right.PackagePath
	})
	return coverage, nil
}

// coverageStage states whether an empty or short result is proven: complete only when every package
// in scope type-checked without diagnostics.
func coverageStage(coverage []ModuleCoverage) ResolutionStage {
	return ResolutionStage{Name: "coverage", Value: coverageSummary(coverage)}
}

func coverageSummary(coverage []ModuleCoverage) string {
	if len(coverage) == 0 {
		return "complete: every package in scope is indexed"
	}
	counts := map[string]int{}
	for _, entry := range coverage {
		counts[entry.Coverage]++
	}
	parts := make([]string, 0, len(coverageOrder))
	for _, level := range coverageOrder {
		if counts[string(level)] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[string(level)], level))
		}
	}
	if len(coverage) == 1 {
		return fmt.Sprintf("incomplete: 1 package is not fully indexed (%s)", strings.Join(parts, ", "))
	}
	return fmt.Sprintf("incomplete: %d packages are not fully indexed (%s)", len(coverage), strings.Join(parts, ", "))
}
