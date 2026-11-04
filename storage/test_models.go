package storage

import (
	"time"

	"github.com/google/uuid"
)

// TestOutcome is the stored outcome of one test attempt.
type TestOutcome string

const (
	TestPass    TestOutcome = "pass"
	TestFail    TestOutcome = "fail"
	TestSkip    TestOutcome = "skip"
	TestTimeout TestOutcome = "timeout"
	TestAborted TestOutcome = "aborted"
)

// Failed reports whether the outcome counts as a failure for flakiness.
func (outcome TestOutcome) Failed() bool { return outcome == TestFail || outcome == TestTimeout }

// ClosureSource says how a result's closure, the symbols its test depends on, was found.
type ClosureSource string

const (
	ClosureCoverage ClosureSource = "coverage"
	ClosureStatic   ClosureSource = "static"
	ClosureSnapshot ClosureSource = "snapshot"
)

// TestCase is one test a run reported for a root; see 24_tests.hcl.
type TestCase struct {
	ID           uuid.UUID `gorm:"column:id;primaryKey"`
	RootID       uuid.UUID `gorm:"column:root_id"`
	Framework    string    `gorm:"column:framework"`
	PackagePath  string    `gorm:"column:package_path"`
	NamePath     string    `gorm:"column:name_path"`
	Display      string    `gorm:"column:display"`
	SymbolHandle *int64    `gorm:"column:symbol_handle"`
}

func (TestCase) TableName() string { return "test_cases" }

// TestRun is one execution of a root's tests against a snapshot; see 24_tests.hcl.
type TestRun struct {
	ID              uuid.UUID  `gorm:"column:id;primaryKey"`
	RootID          uuid.UUID  `gorm:"column:root_id"`
	SnapshotID      *uuid.UUID `gorm:"column:snapshot_id"`
	Producer        string     `gorm:"column:producer"`
	Environment     JSON       `gorm:"column:environment"`
	EnvironmentHash string     `gorm:"column:environment_hash"`
	StartedAt       time.Time  `gorm:"column:started_at"`
	FinishedAt      time.Time  `gorm:"column:finished_at"`
	TaskRunID       *string    `gorm:"column:task_run_id"`
}

func (TestRun) TableName() string { return "test_runs" }

// TestResult is one attempt of one test in one run; see 24_tests.hcl.
type TestResult struct {
	RunID              uuid.UUID     `gorm:"column:run_id;primaryKey"`
	TestCaseID         uuid.UUID     `gorm:"column:test_case_id;primaryKey"`
	Attempt            int           `gorm:"column:attempt;primaryKey;autoIncrement:false"`
	Outcome            TestOutcome   `gorm:"column:outcome"`
	DurationMS         int64         `gorm:"column:duration_ms"`
	FailureDigest      *string       `gorm:"column:failure_digest"`
	ClosureFingerprint string        `gorm:"column:closure_fingerprint"`
	ClosureSource      ClosureSource `gorm:"column:closure_source"`
}

func (TestResult) TableName() string { return "test_results" }

// FileScope is the symbol handle of coverage outside every proven declaration of a document.
const FileScope int64 = 0

// TestCoverage is the statements one run, or one of its tests, executed in one document and declaration;
// see 24_tests.hcl.
type TestCoverage struct {
	RunID             uuid.UUID  `gorm:"column:run_id"`
	TestCaseID        *uuid.UUID `gorm:"column:test_case_id"`
	DocumentOrdinal   int64      `gorm:"column:document_ordinal"`
	SymbolHandle      int64      `gorm:"column:symbol_handle"`
	Statements        int        `gorm:"column:statements"`
	Covered           int        `gorm:"column:covered"`
	ContentFP         *int64     `gorm:"column:content_fp"`
	ContentGeneration *int64     `gorm:"column:content_generation"`
}

func (TestCoverage) TableName() string { return "test_coverage" }
