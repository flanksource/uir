package testruns

import (
	"context"
	"fmt"

	"github.com/flanksource/uir/internal/canonicalhash"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

// Plan returns only tests proved skippable against the latest admissible full-pass baseline. A caller
// runs every inventory item not returned.
func Plan(ctx context.Context, database *gorm.DB, root storage.ModuleRoot, snapshot storage.ModuleSnapshot, request PlanRequest) (PlanResponse, error) {
	response := PlanResponse{Version: PlanContractVersion, SnapshotID: snapshot.ID, InventoryDigest: request.InventoryHash(), Mode: "run_all", Skippable: []string{}}
	if request.InputContext == "" {
		response.Reason = "external inputs are undeclared"
		response.PlanID = planID(response, request)
		return response, nil
	}
	if request.Environment["tags"] != "" {
		response.Reason = "build tags are not supported by affected-test planning"
		response.PlanID = planID(response, request)
		return response, nil
	}
	if !snapshot.Coverage.Complete() {
		response.Reason = "the current index is incomplete"
		response.PlanID = planID(response, request)
		return response, nil
	}
	var baseline storage.TestRun
	err := database.WithContext(ctx).
		Where("root_id = ? AND environment_hash = ? AND inputs_digest = ? AND selection_mode = 'full' AND complete = ? AND identity_verified = ? AND snapshot_id IS NOT NULL",
			root.ID, EnvironmentHash(request.Environment), request.InputsDigest, true, true).
		Where("NOT EXISTS (SELECT 1 FROM test_results WHERE test_results.run_id = test_runs.id AND test_results.outcome IN ?)",
			[]storage.TestOutcome{storage.TestFail, storage.TestTimeout, storage.TestAborted}).
		Order("finished_at DESC").Order("id").Take(&baseline).Error
	if err == gorm.ErrRecordNotFound {
		response.Reason = "no admissible full-pass baseline"
		response.PlanID = planID(response, request)
		return response, nil
	}
	if err != nil {
		return response, fmt.Errorf("load affected-test baseline: %w", err)
	}
	response.BaselineRunID = &baseline.ID
	var baselineSnapshot storage.ModuleSnapshot
	if err := database.WithContext(ctx).Where("id = ?", *baseline.SnapshotID).Take(&baselineSnapshot).Error; err != nil {
		return response, fmt.Errorf("load baseline snapshot %s: %w", *baseline.SnapshotID, err)
	}
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return response, err
	}
	documents, err := newSnapshotDocuments(ctx, database, snapshot.ID)
	if err != nil {
		return response, err
	}
	currentPackages, err := newPackageFingerprints(ctx, database, snapshot.ID)
	if err != nil {
		return response, err
	}
	baselinePackages, err := newPackageFingerprints(ctx, database, baselineSnapshot.ID)
	if err != nil {
		return response, err
	}
	importer := runImporter{database: database, root: root, snapshot: snapshot, documents: documents, packages: currentPackages,
		closures: &closures{database: database, pipeline: pipeline, snapshot: snapshot, inputs: request.InputsDigest, static: map[int64]*staticDependencies{}}}
	baselineClosures := closures{database: database, pipeline: pipeline, snapshot: baselineSnapshot, inputs: request.InputsDigest, static: map[int64]*staticDependencies{}}
	for _, inventory := range request.Inventory {
		test := inventory.asTest()
		var testCase storage.TestCase
		err := database.WithContext(ctx).
			Where("root_id = ? AND framework = ? AND package_path = ? AND name_path = ?", root.ID, test.Framework, test.Package, test.namePath()).
			Take(&testCase).Error
		if err == gorm.ErrRecordNotFound {
			continue
		}
		if err != nil {
			return response, fmt.Errorf("load baseline test %q: %w", inventory.ID, err)
		}
		var result storage.TestResult
		err = database.WithContext(ctx).Where("run_id = ? AND test_case_id = ?", baseline.ID, testCase.ID).
			Order("attempt DESC").Take(&result).Error
		if err == gorm.ErrRecordNotFound {
			continue
		}
		if err != nil {
			return response, fmt.Errorf("load baseline result %q: %w", inventory.ID, err)
		}
		if result.Outcome != storage.TestPass {
			continue
		}
		baselineClosure, err := baselineClosures.of(ctx, nil, testCase.SymbolHandle)
		if err != nil {
			return response, fmt.Errorf("fingerprint baseline test %q: %w", inventory.ID, err)
		}
		baselinePackage := baselinePackages.of(test.Package)
		link, err := importer.link(ctx, test)
		if err != nil {
			return response, fmt.Errorf("link current test %q: %w", inventory.ID, err)
		}
		current, err := importer.closures.of(ctx, nil, link)
		if err != nil {
			return response, fmt.Errorf("fingerprint current test %q: %w", inventory.ID, err)
		}
		currentPackage := currentPackages.of(test.Package)
		if baselinePackage.complete && currentPackage.complete && baselineClosure.source == current.source && baselineClosure.fingerprint == current.fingerprint && baselinePackage.fingerprint == currentPackage.fingerprint {
			response.Skippable = append(response.Skippable, inventory.ID)
		}
	}
	response.Mode = "selected"
	response.Reason = ""
	response.PlanID = planID(response, request)
	return response, nil
}

func planID(response PlanResponse, request PlanRequest) string {
	digest := canonicalhash.New("uir-test-plan-v1")
	digest.Text(response.SnapshotID.String())
	digest.Text(response.InventoryDigest)
	digest.Text(response.Mode)
	digest.Text(response.Reason)
	if response.BaselineRunID != nil {
		digest.Text(response.BaselineRunID.String())
	} else {
		digest.Text("")
	}
	digest.Text(request.GitCommit)
	digest.Text(request.GitTree)
	digest.Text(EnvironmentHash(request.Environment))
	digest.Text(request.InputsDigest)
	return digest.Sum()
}
