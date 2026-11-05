package testruns

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/uir/internal/canonicalhash"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// runNamespace derives a run's id from its snapshot and content, so importing a run again is a no-op.
var runNamespace = uuid.MustParse("6f1d0c52-8a3e-4b8e-9d0e-2f5b7f3c9a41")

// goTest is gavel's framework name for `go test`.
const goTest = "go test"

// RunResult is what importing one run did.
type RunResult struct {
	RunID    uuid.UUID                     `json:"run_id"`
	Reused   bool                          `json:"reused"`
	Tests    int                           `json:"tests"`
	Results  int                           `json:"results"`
	Linked   int                           `json:"linked"`
	Closures map[storage.ClosureSource]int `json:"closures,omitempty"`
	Coverage int                           `json:"coverage_rows"`
}

// Result is what an import did, run by run.
type Result struct {
	RootKey    string      `json:"root_key"`
	SnapshotID uuid.UUID   `json:"snapshot_id"`
	Runs       []RunResult `json:"runs"`
}

// ImportRuns records imported against snapshotID, which the caller proved is the snapshot whose bytes
// the tests executed (indexer.CheckCurrent on the checkout). Each run is one transaction; a run already
// imported against the same snapshot is reused, not written again.
func ImportRuns(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID, imported Import) (Result, error) {
	if err := imported.Validate(); err != nil {
		return Result{}, err
	}
	var snapshot storage.ModuleSnapshot
	if err := database.WithContext(ctx).Where("id = ?", snapshotID).Take(&snapshot).Error; err != nil {
		return Result{}, fmt.Errorf("load snapshot %s: %w", snapshotID, err)
	}
	var root storage.ModuleRoot
	if err := database.WithContext(ctx).Where("id = ?", snapshot.RootID).Take(&root).Error; err != nil {
		return Result{}, fmt.Errorf("load the root of snapshot %s: %w", snapshotID, err)
	}
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return Result{}, err
	}
	documents, err := newSnapshotDocuments(ctx, database, snapshot.ID)
	if err != nil {
		return Result{}, err
	}
	result := Result{RootKey: root.RootKey, SnapshotID: snapshot.ID, Runs: []RunResult{}}
	for i, run := range imported.Runs {
		importer := runImporter{
			database: database, root: root, snapshot: snapshot, documents: documents,
			closures: &closures{database: database, pipeline: pipeline, snapshot: snapshot, inputs: run.InputsDigest, static: map[int64]*staticDependencies{}},
		}
		recorded, err := importer.importRun(ctx, run)
		if err != nil {
			return result, fmt.Errorf("run %d: %w", i, err)
		}
		result.Runs = append(result.Runs, recorded)
	}
	return result, nil
}

type runImporter struct {
	database  *gorm.DB
	root      storage.ModuleRoot
	snapshot  storage.ModuleSnapshot
	documents *snapshotDocuments
	closures  *closures
}

// pendingCoverage is coverage attributed for one test, or for the run when test is nil.
type pendingCoverage struct {
	test   *uuid.UUID
	counts map[coverageKey]coverageCount
}

func (importer runImporter) importRun(ctx context.Context, run Run) (RunResult, error) {
	encoded, err := json.Marshal(run)
	if err != nil {
		return RunResult{}, fmt.Errorf("encode the run: %w", err)
	}
	recorded := RunResult{RunID: uuid.NewSHA1(runNamespace, append([]byte(importer.snapshot.ID.String()), encoded...)), Closures: map[storage.ClosureSource]int{}}
	var existing int64
	if err := importer.database.WithContext(ctx).Model(&storage.TestRun{}).Where("id = ?", recorded.RunID).Count(&existing).Error; err != nil {
		return RunResult{}, fmt.Errorf("look up run %s: %w", recorded.RunID, err)
	}
	if existing > 0 {
		recorded.Reused = true
		return recorded, nil
	}
	if run.Environment == nil {
		run.Environment = map[string]string{}
	}
	environment, err := json.Marshal(run.Environment)
	if err != nil {
		return RunResult{}, fmt.Errorf("encode the environment: %w", err)
	}
	snapshotID := importer.snapshot.ID
	row := storage.TestRun{
		ID: recorded.RunID, RootID: importer.root.ID, SnapshotID: &snapshotID, Producer: run.Producer,
		Environment: environment, EnvironmentHash: EnvironmentHash(run.Environment),
		StartedAt: run.StartedAt.UTC(), FinishedAt: run.FinishedAt.UTC(),
	}
	if run.TaskRunID != "" {
		row.TaskRunID = &run.TaskRunID
	}
	planned, coverage, err := importer.plan(ctx, run, &recorded)
	if err != nil {
		return RunResult{}, err
	}
	rows, err := importer.coverageRows(ctx, recorded.RunID, coverage)
	if err != nil {
		return RunResult{}, err
	}
	err = importer.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("record run %s: %w", recorded.RunID, err)
		}
		var results []storage.TestResult
		caseIDs := map[uuid.UUID]uuid.UUID{}
		for _, test := range planned {
			testCase, err := importer.testCase(ctx, tx, test.test, test.link)
			if err != nil {
				return fmt.Errorf("test %s: %w", test.test.display(), err)
			}
			caseIDs[test.key] = testCase.ID
			for attempt, outcome := range test.test.Attempts {
				results = append(results, storage.TestResult{
					RunID: recorded.RunID, TestCaseID: testCase.ID, Attempt: attempt + 1, Outcome: outcome.Outcome,
					DurationMS: outcome.DurationMS, FailureDigest: failureDigest(outcome.Failure),
					ClosureFingerprint: test.closure.fingerprint, ClosureSource: test.closure.source,
				})
			}
		}
		if len(results) > 0 {
			if err := tx.CreateInBatches(results, 500).Error; err != nil {
				return fmt.Errorf("record %d test results: %w", len(results), err)
			}
		}
		recorded.Results = len(results)
		for i := range rows {
			if rows[i].TestCaseID != nil {
				id := caseIDs[*rows[i].TestCaseID]
				rows[i].TestCaseID = &id
			}
		}
		if len(rows) > 0 {
			if err := tx.CreateInBatches(rows, 500).Error; err != nil {
				return fmt.Errorf("record %d coverage rows: %w", len(rows), err)
			}
		}
		recorded.Coverage = len(rows)
		return nil
	})
	return recorded, err
}

// plannedTest is a test with everything read before the run's transaction: its link, its closure, and
// key, the placeholder its coverage rows carry until its test case id is known.
type plannedTest struct {
	test    Test
	key     uuid.UUID
	link    *int64
	closure closure
}

// plan reads, outside any transaction, each test's link and closure and attributes the run's and each
// test's coverage.
func (importer runImporter) plan(ctx context.Context, run Run, recorded *RunResult) ([]plannedTest, []pendingCoverage, error) {
	coverage := []pendingCoverage{}
	if len(run.Coverage) > 0 {
		counts, err := importer.documents.attribute(ctx, run.Coverage)
		if err != nil {
			return nil, nil, err
		}
		coverage = append(coverage, pendingCoverage{counts: counts})
	}
	planned := make([]plannedTest, 0, len(run.Tests))
	for _, test := range run.Tests {
		link, err := importer.link(ctx, test)
		if err != nil {
			return nil, nil, fmt.Errorf("test %s: %w", test.display(), err)
		}
		next := plannedTest{test: test, key: uuid.New(), link: link}
		var covered []int64
		if len(test.Coverage) > 0 {
			counts, err := importer.documents.attribute(ctx, test.Coverage)
			if err != nil {
				return nil, nil, fmt.Errorf("test %s: %w", test.display(), err)
			}
			coverage = append(coverage, pendingCoverage{test: &next.key, counts: counts})
			for key, count := range counts {
				if key.handle != storage.FileScope && count.covered > 0 {
					covered = append(covered, key.handle)
				}
			}
		}
		if next.closure, err = importer.closures.of(ctx, covered, link); err != nil {
			return nil, nil, fmt.Errorf("test %s: %w", test.display(), err)
		}
		recorded.Tests++
		if link != nil {
			recorded.Linked++
		}
		recorded.Closures[next.closure.source]++
		planned = append(planned, next)
	}
	return planned, coverage, nil
}

// testCase finds or creates the test's row, linking it to handle, the declaration the snapshot proved
// it is, when the row has no link yet.
func (importer runImporter) testCase(ctx context.Context, tx *gorm.DB, test Test, handle *int64) (storage.TestCase, error) {
	var found []storage.TestCase
	if err := tx.WithContext(ctx).Where("root_id = ? AND framework = ? AND package_path = ? AND name_path = ?", importer.root.ID, test.Framework, test.Package, test.namePath()).
		Find(&found).Error; err != nil {
		return storage.TestCase{}, fmt.Errorf("look up the test case: %w", err)
	}
	if len(found) == 1 {
		if found[0].SymbolHandle == nil && handle != nil {
			if err := tx.WithContext(ctx).Model(&found[0]).Update("symbol_handle", *handle).Error; err != nil {
				return storage.TestCase{}, fmt.Errorf("link the test case: %w", err)
			}
			found[0].SymbolHandle = handle
		}
		return found[0], nil
	}
	created := storage.TestCase{ID: uuid.New(), RootID: importer.root.ID, Framework: test.Framework, PackagePath: test.Package,
		NamePath: test.namePath(), Display: test.display(), SymbolHandle: handle}
	if err := tx.WithContext(ctx).Create(&created).Error; err != nil {
		return storage.TestCase{}, fmt.Errorf("record the test case: %w", err)
	}
	return created, nil
}

// link is the handle of the declaration a test is, when the snapshot proves it: for `go test`, the
// func of the test's top-level name in its file; otherwise the innermost proven declaration enclosing
// its line. A test whose file the snapshot does not serve, such as a test file the index excluded, has
// no link.
func (importer runImporter) link(ctx context.Context, test Test) (*int64, error) {
	if test.File == "" {
		return nil, nil
	}
	document, found, err := importer.documents.document(ctx, test.File)
	if err != nil || !found {
		return nil, err
	}
	if test.Framework == goTest {
		name, _, _ := strings.Cut(test.Name, "/")
		for _, declared := range document.declarations {
			if declared.kind == "func" && declared.name == name && inTestPackage(declared, test) {
				return &declared.handle, nil
			}
		}
		return nil, nil
	}
	if test.Line < 1 {
		return nil, nil
	}
	handle, err := document.enclosing(test.Line, test.Line)
	if err != nil || handle == storage.FileScope {
		return nil, nil
	}
	for _, declared := range document.declarations {
		if declared.handle == handle && !inTestPackage(declared, test) {
			return nil, nil
		}
	}
	return &handle, nil
}

// inTestPackage reports whether declared belongs to the package the producer ran test in, or to its
// external _test package. A producer that reports another package's file, such as for two tests of one
// name, leaves the test unlinked rather than linked to the other package's declaration.
func inTestPackage(declared declaration, test Test) bool {
	return declared.packagePath == test.Package || declared.packagePath == test.Package+"_test"
}

// coverageRows turns attributed coverage into rows, giving each declaration the content fingerprint
// and generation it has at the run's snapshot.
func (importer runImporter) coverageRows(ctx context.Context, runID uuid.UUID, coverage []pendingCoverage) ([]storage.TestCoverage, error) {
	handles := map[int64]bool{}
	for _, pending := range coverage {
		for key := range pending.counts {
			if key.handle != storage.FileScope {
				handles[key.handle] = true
			}
		}
	}
	generations, err := storage.SymbolGenerations(ctx, importer.database, importer.snapshot.ID, slices.Collect(maps.Keys(handles)))
	if err != nil {
		return nil, err
	}
	var rows []storage.TestCoverage
	for _, pending := range coverage {
		for _, key := range slices.SortedFunc(maps.Keys(pending.counts), func(left, right coverageKey) int {
			return cmp.Or(cmp.Compare(left.document, right.document), cmp.Compare(left.handle, right.handle))
		}) {
			count := pending.counts[key]
			row := storage.TestCoverage{RunID: runID, TestCaseID: pending.test, DocumentOrdinal: key.document, SymbolHandle: key.handle,
				Statements: count.statements, Covered: count.covered}
			if key.handle != storage.FileScope {
				generation, defined := generations[key.handle]
				if !defined || generation.ContentFP == nil {
					return nil, errors.New("coverage names a declaration its snapshot does not define")
				}
				row.ContentFP, row.ContentGeneration = generation.ContentFP, &generation.Generation
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// EnvironmentHash digests a run's environment in key order.
func EnvironmentHash(environment map[string]string) string {
	digest := canonicalhash.New("uir-test-environment-v1")
	digest.Count(len(environment))
	for _, key := range slices.Sorted(maps.Keys(environment)) {
		digest.Text(key)
		digest.Text(environment[key])
	}
	return digest.Sum()
}

func failureDigest(failure string) *string {
	if failure == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(failure))
	digest := hex.EncodeToString(sum[:])
	return &digest
}
