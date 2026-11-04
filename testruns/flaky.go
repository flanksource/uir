package testruns

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// FlakyOptions selects the results Flaky reads: a root's runs that started at or after Since (zero
// for all), and only tests with at least MinRuns runs.
type FlakyOptions struct {
	RootID  uuid.UUID
	Since   time.Time
	MinRuns int
}

// FlakyClosure is one group of results of a test on identical code in one environment that both passed
// and failed.
type FlakyClosure struct {
	Source      storage.ClosureSource `json:"source"`
	Fingerprint string                `json:"fingerprint"`
	Environment string                `json:"environment"`
	Passes      int                   `json:"passes"`
	Failures    int                   `json:"failures"`
}

// FlakyTest is a test that flaked: outcomes differed for the same closure, closure source and
// environment, or a run's attempts both failed and passed.
type FlakyTest struct {
	Framework   string              `json:"framework"`
	Package     string              `json:"package"`
	Name        string              `json:"name"`
	Runs        int                 `json:"runs"`
	Failures    int                 `json:"failures"`
	FlakeRate   float64             `json:"flake_rate"`
	LastOutcome storage.TestOutcome `json:"last_outcome"`
	RetryFlakes int                 `json:"retry_flakes"`
	Closures    []FlakyClosure      `json:"closures,omitempty"`
}

type flakyRow struct {
	TestCaseID         uuid.UUID
	Framework          string
	PackagePath        string
	Display            string
	RunID              uuid.UUID
	StartedAt          time.Time
	EnvironmentHash    string
	Attempt            int
	Outcome            storage.TestOutcome
	ClosureFingerprint string
	ClosureSource      storage.ClosureSource
}

type closureKey struct {
	source      storage.ClosureSource
	fingerprint string
	environment string
}

// Flaky lists the root's flaky tests, most failures first. Nothing is stored: it is computed from
// test_results each time. FlakeRate is the share of failed attempts among the attempts of the groups
// that flaked.
func Flaky(ctx context.Context, database *gorm.DB, options FlakyOptions) ([]FlakyTest, error) {
	var rows []flakyRow
	statement := database.WithContext(ctx).Table("test_results r").
		Select("c.id AS test_case_id, c.framework, c.package_path, c.display, r.run_id, u.started_at, u.environment_hash, r.attempt, r.outcome, r.closure_fingerprint, r.closure_source").
		Joins("JOIN test_cases c ON c.id = r.test_case_id").
		Joins("JOIN test_runs u ON u.id = r.run_id").
		Where("u.root_id = ?", options.RootID)
	if !options.Since.IsZero() {
		statement = statement.Where("u.started_at >= ?", options.Since.UTC())
	}
	if err := statement.Order("c.id, u.started_at, r.run_id, r.attempt").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load test results: %w", err)
	}
	var flaky []FlakyTest
	for start := 0; start < len(rows); {
		end := start
		for end < len(rows) && rows[end].TestCaseID == rows[start].TestCaseID {
			end++
		}
		if test, found := flakyTest(rows[start:end]); found && test.Runs >= options.MinRuns {
			flaky = append(flaky, test)
		}
		start = end
	}
	slices.SortFunc(flaky, func(left, right FlakyTest) int {
		return cmp.Or(cmp.Compare(right.Failures, left.Failures), cmp.Compare(left.Package, right.Package), cmp.Compare(left.Name, right.Name))
	})
	return flaky, nil
}

// flakyTest judges one test's results, ordered by run start then attempt.
func flakyTest(rows []flakyRow) (FlakyTest, bool) {
	test := FlakyTest{Framework: rows[0].Framework, Package: rows[0].PackagePath, Name: rows[0].Display, LastOutcome: rows[len(rows)-1].Outcome}
	groups := map[closureKey]*FlakyClosure{}
	var order []closureKey
	runs := map[uuid.UUID]*[2]bool{}
	for _, row := range rows {
		seen, found := runs[row.RunID]
		if !found {
			seen = &[2]bool{}
			runs[row.RunID] = seen
		}
		key := closureKey{source: row.ClosureSource, fingerprint: row.ClosureFingerprint, environment: row.EnvironmentHash}
		group, found := groups[key]
		if !found {
			group = &FlakyClosure{Source: key.source, Fingerprint: key.fingerprint, Environment: key.environment}
			groups[key] = group
			order = append(order, key)
		}
		switch {
		case row.Outcome == storage.TestPass:
			group.Passes++
			seen[0] = true
		case row.Outcome.Failed():
			group.Failures++
			test.Failures++
			seen[1] = true
		}
	}
	test.Runs = len(runs)
	for _, seen := range runs {
		if seen[0] && seen[1] {
			test.RetryFlakes++
		}
	}
	var attempts, failures int
	for _, key := range order {
		if group := groups[key]; group.Passes > 0 && group.Failures > 0 {
			test.Closures = append(test.Closures, *group)
			attempts, failures = attempts+group.Passes+group.Failures, failures+group.Failures
		}
	}
	if attempts > 0 {
		test.FlakeRate = float64(failures) / float64(attempts)
	}
	return test, len(test.Closures) > 0 || test.RetryFlakes > 0
}
