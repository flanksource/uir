package testruns

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const plannerFixtureSource = `package deep

var deepestValue = "before"

func level1() string { return level2() }
func level2() string { return level3() }
func level3() string { return level4() }
func level4() string { return level5() }
func level5() string { return level6() }
func level6() string { return deepestValue }

func stable() string { return "stable" }
`

const plannerFixtureTests = `package deep

import "testing"

func TestDeepGlobal(t *testing.T) {
	if level1() != "before" {
		t.Fatal("unexpected value")
	}
}

func TestUnrelated(t *testing.T) {
	if stable() != "stable" {
		t.Fatal("unexpected value")
	}
}
`

func TestPlanTracksGlobalThroughDeepCallChain(t *testing.T) {
	const dependencyDepth = 7 // Test -> six functions -> global read.
	if graph.MaxDepth < dependencyDepth {
		t.Fatalf("fixture needs dependency depth %d, graph limit is %d", dependencyDepth, graph.MaxDepth)
	}
	ctx := context.Background()
	workspace := t.TempDir()
	writePlannerFile(t, filepath.Join(workspace, "go.mod"), "module example.com/deep\n\ngo 1.25\n")
	writePlannerFile(t, filepath.Join(workspace, "deep.go"), plannerFixtureSource)
	writePlannerFile(t, filepath.Join(workspace, "deep_test.go"), plannerFixtureTests)
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: filepath.Join(t.TempDir(), "planner.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, dbErr := database.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	engine, err := indexer.New(database)
	if err != nil {
		t.Fatal(err)
	}
	baseline := indexPlannerFixture(t, ctx, engine, database, workspace)
	request := PlanRequest{
		Version: PlanContractVersion, GitCommit: strings.Repeat("a", 40),
		Environment:  map[string]string{"GOOS": "linux", "GOARCH": "amd64", "go": "go-test", "race": "false", "tags": "", "count": ""},
		InputsDigest: strings.Repeat("b", 64), InputContext: "no-external-inputs:v1",
		Inventory: []InventoryTest{
			{ID: "deep", Framework: goTest, Package: "example.com/deep", Name: "TestDeepGlobal", File: "deep_test.go", Line: 5},
			{ID: "control", Framework: goTest, Package: "example.com/deep", Name: "TestUnrelated", File: "deep_test.go", Line: 11},
		},
	}
	now := time.Now().UTC()
	imported, err := ImportRuns(ctx, database, Import{Version: ContractVersion, Runs: []Run{{
		GitCommit: request.GitCommit, Producer: "planner-test", Environment: request.Environment,
		StartedAt: now, FinishedAt: now, InputsDigest: request.InputsDigest, SelectionMode: "full",
		InventoryDigest: request.InventoryHash(), Complete: true, IdentityVerified: true,
		Tests: []Test{
			{Framework: goTest, Package: "example.com/deep", Name: "TestDeepGlobal", File: "deep_test.go", Line: 5, Attempts: []Attempt{{Outcome: storage.TestPass}}},
			{Framework: goTest, Package: "example.com/deep", Name: "TestUnrelated", File: "deep_test.go", Line: 11, Attempts: []Attempt{{Outcome: storage.TestPass}}},
		},
	}}}, []uuid.UUID{baseline.ID})
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := Plan(ctx, database, baseline.root, baseline.ModuleSnapshot, request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(unchanged.Skippable, ",") != "deep,control" {
		t.Fatalf("unchanged skippable tests = %v, want [deep control]", unchanged.Skippable)
	}
	withNewTest := request
	withNewTest.Inventory = append(append([]InventoryTest{}, request.Inventory...), InventoryTest{
		ID: "new", Framework: goTest, Package: "example.com/deep", Name: "TestNew", File: "new_test.go", Line: 3,
	})
	newTestPlan, err := Plan(ctx, database, baseline.root, baseline.ModuleSnapshot, withNewTest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(newTestPlan.Skippable, ",") != "deep,control" {
		t.Fatalf("plan with unseen test skips %v, want only [deep control]", newTestPlan.Skippable)
	}

	changedInputs := request
	changedInputs.InputsDigest = strings.Repeat("c", 64)
	assertPlannerRunsAll(t, ctx, database, baseline, changedInputs, "no admissible full-pass baseline")
	changedBuild := request
	changedBuild.Environment = maps.Clone(request.Environment)
	changedBuild.Environment["GOARCH"] = "arm64"
	assertPlannerRunsAll(t, ctx, database, baseline, changedBuild, "no admissible full-pass baseline")
	undeclaredInputs := request
	undeclaredInputs.InputContext = ""
	assertPlannerRunsAll(t, ctx, database, baseline, undeclaredInputs, "external inputs are undeclared")
	taggedBuild := request
	taggedBuild.Environment = maps.Clone(request.Environment)
	taggedBuild.Environment["tags"] = "integration"
	assertPlannerRunsAll(t, ctx, database, baseline, taggedBuild, "build tags are not supported by affected-test planning")

	partialOnly := request
	partialOnly.Environment = maps.Clone(request.Environment)
	partialOnly.Environment["count"] = "partial-only"
	_, err = ImportRuns(ctx, database, Import{Version: ContractVersion, Runs: []Run{{
		GitCommit: partialOnly.GitCommit, Producer: "planner-test", Environment: partialOnly.Environment,
		StartedAt: now, FinishedAt: now, InputsDigest: partialOnly.InputsDigest, SelectionMode: "partial",
		InventoryDigest: partialOnly.InventoryHash(), Complete: true, IdentityVerified: true,
		Tests: []Test{{Framework: goTest, Package: "example.com/deep", Name: "TestDeepGlobal", File: "deep_test.go", Line: 5, Attempts: []Attempt{{Outcome: storage.TestPass}}}},
	}}}, []uuid.UUID{baseline.ID})
	if err != nil {
		t.Fatal(err)
	}
	assertPlannerRunsAll(t, ctx, database, baseline, partialOnly, "no admissible full-pass baseline")

	unverified := request
	unverified.Environment = maps.Clone(request.Environment)
	unverified.Environment["count"] = "unverified"
	_, err = ImportRuns(ctx, database, Import{Version: ContractVersion, Runs: []Run{{
		GitCommit: unverified.GitCommit, Producer: "planner-test", Environment: unverified.Environment,
		StartedAt: now, FinishedAt: now, InputsDigest: unverified.InputsDigest, SelectionMode: "full",
		InventoryDigest: unverified.InventoryHash(), Complete: true,
		Tests: []Test{{Framework: goTest, Package: "example.com/deep", Name: "TestDeepGlobal", File: "deep_test.go", Line: 5, Attempts: []Attempt{{Outcome: storage.TestPass}}}},
	}}}, []uuid.UUID{baseline.ID})
	if err != nil {
		t.Fatal(err)
	}
	assertPlannerRunsAll(t, ctx, database, baseline, unverified, "no admissible full-pass baseline")

	deepBaseline := plannerResult(t, database, imported.Runs[0].RunID, baseline.root.ID, request.Inventory[0].asTest())
	controlBaseline := plannerResult(t, database, imported.Runs[0].RunID, baseline.root.ID, request.Inventory[1].asTest())
	if deepBaseline.ClosureSource != storage.ClosureStatic || controlBaseline.ClosureSource != storage.ClosureStatic {
		t.Fatalf("baseline closures = %q and %q, want static", deepBaseline.ClosureSource, controlBaseline.ClosureSource)
	}

	writePlannerFile(t, filepath.Join(workspace, "deep.go"), strings.Replace(plannerFixtureSource, `"before"`, `"after"`, 1))
	changed := indexPlannerFixture(t, ctx, engine, database, workspace)
	deepCurrent := plannerClosure(t, ctx, database, changed, request.Inventory[0].asTest(), request.InputsDigest)
	controlCurrent := plannerClosure(t, ctx, database, changed, request.Inventory[1].asTest(), request.InputsDigest)
	if deepCurrent.source != storage.ClosureStatic || deepCurrent.fingerprint == deepBaseline.ClosureFingerprint {
		t.Fatalf("deep closure = %q %s; want changed static closure", deepCurrent.source, deepCurrent.fingerprint)
	}
	if controlCurrent.source != storage.ClosureStatic || controlCurrent.fingerprint != controlBaseline.ClosureFingerprint {
		t.Fatalf("control closure = %q %s; want unchanged static closure", controlCurrent.source, controlCurrent.fingerprint)
	}
	packageCurrent, complete, err := packageFingerprint(ctx, database, changed.ID, "example.com/deep")
	if err != nil || !complete {
		t.Fatalf("current package fingerprint incomplete: complete=%t err=%v", complete, err)
	}
	if packageCurrent == deepBaseline.PackageFingerprint {
		t.Fatal("package fingerprint did not change with the global")
	}
	planned, err := Plan(ctx, database, changed.root, changed.ModuleSnapshot, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(planned.Skippable) != 0 {
		t.Fatalf("changed skippable tests = %v, want none", planned.Skippable)
	}
}

type plannerSnapshot struct {
	storage.ModuleSnapshot
	root storage.ModuleRoot
}

func assertPlannerRunsAll(t *testing.T, ctx context.Context, database *gorm.DB, snapshot plannerSnapshot, request PlanRequest, reason string) {
	t.Helper()
	plan, err := Plan(ctx, database, snapshot.root, snapshot.ModuleSnapshot, request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != "run_all" || len(plan.Skippable) != 0 || plan.Reason != reason {
		t.Fatalf("plan = mode %q, skippable %v, reason %q; want run_all, none, %q", plan.Mode, plan.Skippable, plan.Reason, reason)
	}
}

func indexPlannerFixture(t *testing.T, ctx context.Context, engine *indexer.Indexer, database *gorm.DB, workspace string) plannerSnapshot {
	t.Helper()
	results, err := engine.IndexModules(ctx, indexer.ModuleOptions{Path: workspace, IncludeTests: true, Reason: storage.ReasonReindex})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Error != "" {
		t.Fatalf("index results = %+v", results)
	}
	var snapshot storage.ModuleSnapshot
	if err := database.Where("id = ?", results[0].SnapshotID).Take(&snapshot).Error; err != nil {
		t.Fatal(err)
	}
	var root storage.ModuleRoot
	if err := database.Where("id = ?", snapshot.RootID).Take(&root).Error; err != nil {
		t.Fatal(err)
	}
	return plannerSnapshot{ModuleSnapshot: snapshot, root: root}
}

func plannerResult(t *testing.T, database *gorm.DB, run uuid.UUID, root uuid.UUID, test Test) storage.TestResult {
	t.Helper()
	var testCase storage.TestCase
	if err := database.Where("root_id = ? AND framework = ? AND package_path = ? AND name_path = ?", root, test.Framework, test.Package, test.namePath()).Take(&testCase).Error; err != nil {
		t.Fatal(err)
	}
	var result storage.TestResult
	if err := database.Where("run_id = ? AND test_case_id = ?", run, testCase.ID).Take(&result).Error; err != nil {
		t.Fatal(err)
	}
	return result
}

func plannerClosure(t *testing.T, ctx context.Context, database *gorm.DB, snapshot plannerSnapshot, test Test, inputs string) closure {
	t.Helper()
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		t.Fatal(err)
	}
	documents, err := newSnapshotDocuments(ctx, database, snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	importer := runImporter{database: database, root: snapshot.root, snapshot: snapshot.ModuleSnapshot, documents: documents,
		closures: &closures{database: database, pipeline: pipeline, snapshot: snapshot.ModuleSnapshot, inputs: inputs, static: map[int64]*staticDependencies{}}}
	link, err := importer.link(ctx, test)
	if err != nil {
		t.Fatal(err)
	}
	current, err := importer.closures.of(ctx, nil, link)
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func writePlannerFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	modified := time.Now().Add(time.Second)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
}
