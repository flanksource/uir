package query

import (
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("dependency selections a change query compares", func() {
	const (
		pricing = "example.org/pricing"
		billing = "example.org/billing"
		ledger  = "example.org/ledger"
		local   = "example.org/local"
	)
	target := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	captured := func(dependencies ...storage.SnapshotDependency) storage.SnapshotDependenciesResult {
		return storage.SnapshotDependenciesResult{Captured: true, Items: dependencies}
	}

	It("reports the modules whose selected version, replacement or target snapshot differs, and those added or dropped", func() {
		before := captured(
			storage.SnapshotDependency{ModulePath: pricing, DeclaredVersion: "v1.0.0", SelectedVersion: "v1.0.0"},
			storage.SnapshotDependency{ModulePath: billing, DeclaredVersion: "v1.0.0", SelectedVersion: "v1.2.0"},
			storage.SnapshotDependency{ModulePath: ledger, DeclaredVersion: "v0.1.0"},
			storage.SnapshotDependency{ModulePath: local, DeclaredVersion: "v0.0.0", ReplacePath: "../local"},
		)
		after := captured(
			storage.SnapshotDependency{ModulePath: pricing, DeclaredVersion: "v1.0.0", SelectedVersion: "v1.1.0"},
			storage.SnapshotDependency{ModulePath: billing, DeclaredVersion: "v1.1.0", SelectedVersion: "v1.2.0"},
			storage.SnapshotDependency{ModulePath: local, DeclaredVersion: "v0.0.0", ReplacePath: "../local", TargetSnapshotID: &target},
		)

		changes, err := changedDependencies(before, after)
		Expect(err).ToNot(HaveOccurred())
		Expect(changes).To(Equal(map[string]GraphDependencyChange{
			pricing: {Module: pricing, From: "v1.0.0", To: "v1.1.0"},
			ledger:  {Module: ledger, From: "v0.1.0"},
			local:   {Module: local, From: "v0.0.0 => ../local", To: "v0.0.0 => ../local @" + target.String()},
		}), "a declared version that selects the same version is no change")
	})

	It("refuses to compare a snapshot that captured its dependencies with one that did not", func() {
		_, err := changedDependencies(captured(), storage.SnapshotDependenciesResult{})
		Expect(err).To(MatchError(ContainSubstring("captured")))
	})

	It("retains an excluded dependency reached only in the earlier walk", func() {
		changes, err := changedDependencies(captured(storage.SnapshotDependency{ModulePath: pricing, SelectedVersion: "v1.0.0"}), captured())
		Expect(err).ToNot(HaveOccurred())
		facts := func(group string) graphPackageFacts {
			Expect(group).To(Equal(pricing + "/api"))
			return graphPackageFacts{module: pricing}
		}

		markExcludedDependencies(changes, map[string]int{pricing + "/api": 1}, facts)
		markExcludedDependencies(changes, nil, facts)

		Expect(changes).To(Equal(map[string]GraphDependencyChange{
			pricing: {Module: pricing, From: "v1.0.0", Excluded: true},
		}))
	})
})
