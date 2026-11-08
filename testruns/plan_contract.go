package testruns

import (
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/internal/canonicalhash"
	"github.com/google/uuid"
)

const PlanContractVersion = 1

// PlanRequest is a producer's complete current ordinary-test inventory and the exact execution
// context for which it asks UIR to prove tests skippable.
type PlanRequest struct {
	Version      int               `json:"version"`
	GitCommit    string            `json:"git_commit"`
	Dirty        bool              `json:"dirty,omitempty"`
	GitTree      string            `json:"git_tree,omitempty"`
	Environment  map[string]string `json:"environment"`
	InputsDigest string            `json:"inputs_digest"`
	InputContext string            `json:"input_context"`
	Inventory    []InventoryTest   `json:"inventory"`
}

// InventoryTest is one producer scheduling unit. ID is opaque to UIR and returned in Skippable.
type InventoryTest struct {
	ID        string   `json:"id"`
	Framework string   `json:"framework"`
	Package   string   `json:"package"`
	Suite     []string `json:"suite,omitempty"`
	Name      string   `json:"name"`
	File      string   `json:"file,omitempty"`
	Line      int      `json:"line,omitempty"`
}

// PlanResponse is fail-closed: Mode run_all has no skippable tests; Mode selected carries only tests
// for which the baseline pass, closure, package setup and declared inputs are unchanged.
type PlanResponse struct {
	Version         int        `json:"version"`
	PlanID          string     `json:"plan_id"`
	SnapshotID      uuid.UUID  `json:"snapshot_id"`
	InventoryDigest string     `json:"inventory_digest"`
	Mode            string     `json:"mode"`
	Reason          string     `json:"reason,omitempty"`
	BaselineRunID   *uuid.UUID `json:"baseline_run_id,omitempty"`
	Skippable       []string   `json:"skippable"`
}

func (request PlanRequest) Validate() error {
	if request.Version != PlanContractVersion {
		return fmt.Errorf("plan version %d is not %d", request.Version, PlanContractVersion)
	}
	if !fullObject.MatchString(request.GitCommit) {
		return fmt.Errorf("git_commit %q is not a full lowercase commit SHA", request.GitCommit)
	}
	if request.Dirty && !fullObject.MatchString(request.GitTree) {
		return fmt.Errorf("a dirty plan requires a full lowercase git_tree")
	}
	if len(request.InputsDigest) != 64 {
		return fmt.Errorf("inputs_digest must be a 64-character digest")
	}
	seen := map[string]bool{}
	for i, test := range request.Inventory {
		if test.ID == "" || test.Framework == "" || test.Package == "" || test.Name == "" {
			return fmt.Errorf("inventory test %d requires id, framework, package and name", i)
		}
		if seen[test.ID] {
			return fmt.Errorf("inventory id %q is repeated", test.ID)
		}
		seen[test.ID] = true
		if test.File != "" && !validPath(test.File) {
			return fmt.Errorf("inventory test %q has invalid file %q", test.ID, test.File)
		}
	}
	return nil
}

func (request PlanRequest) InventoryHash() string {
	digest := canonicalhash.New("uir-test-inventory-v1")
	tests := slices.Clone(request.Inventory)
	slices.SortFunc(tests, func(a, b InventoryTest) int { return strings.Compare(a.ID, b.ID) })
	digest.Count(len(tests))
	for _, test := range tests {
		digest.Text(test.ID)
		digest.Text(test.Framework)
		digest.Text(test.Package)
		digest.Count(len(test.Suite))
		for _, segment := range test.Suite {
			digest.Text(segment)
		}
		digest.Text(test.Name)
		digest.Text(test.File)
		digest.Count(test.Line)
	}
	return digest.Sum()
}

func (test InventoryTest) asTest() Test {
	return Test{Framework: test.Framework, Package: test.Package, Suite: test.Suite, Name: test.Name, File: test.File, Line: test.Line}
}
