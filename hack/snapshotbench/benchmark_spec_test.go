//go:build hack

package main

import (
	"testing"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSnapshotBenchmark(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Snapshot benchmark")
}

var _ = Describe("snapshot replay evidence", func() {
	It("retains every first-parent commit in order and isolates historical initialization and reuse", func() {
		corpus := corpus{Name: "rules", Producer: "oipa", Tip: "third", Commits: []string{"first", "second", "third"}}
		head := replayCases(corpus, "head")
		Expect(head).To(HaveLen(4))
		Expect([]string{head[0].Commit, head[1].Commit, head[2].Commit, head[3].Commit}).To(Equal([]string{"first", "second", "third", "third"}))
		history := replayCases(corpus, "historical")
		Expect(history).To(HaveLen(6))
		Expect([]string{history[0].Kind, history[1].Kind, history[4].Kind, history[5].Kind}).To(Equal([]string{"initial", "revision", "noop", "repeat"}))
		Expect([]string{history[1].Commit, history[2].Commit, history[3].Commit}).To(Equal(corpus.Commits))
	})

	DescribeTable("refuses an observation whose runtime evidence breaks an invariant", func(mutate func(*observation), message string) {
		id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
		result := observation{Case: measurement{ID: "sample", Commit: "commit", Path: "historical", Kind: "repeat"},
			Before:    databaseState{Heads: map[string]head{"root": {Snapshot: "head", Version: 1}}},
			After:     databaseState{Heads: map[string]head{"root": {Snapshot: "head", Version: 1}}},
			Modules:   []indexer.ModuleResult{{RootKey: "root", SnapshotID: id.String(), Unchanged: true}},
			Snapshots: []storage.ModuleSnapshotView{{ID: id, GitCommit: "commit"}}, SourceVerified: 1, Delta: map[string]int64{"snapshots": 0}}
		Expect(validateObservation(result)).To(Succeed())
		mutate(&result)
		Expect(validateObservation(result)).To(MatchError(ContainSubstring(message)))
	},
		Entry("moved head", func(r *observation) { r.After.Heads["root"] = head{Snapshot: "changed", Version: 2} }, "moved heads"),
		Entry("wrong commit", func(r *observation) { r.Snapshots[0].GitCommit = "wrong" }, "wrong snapshot/commit"),
		Entry("duplicate publication", func(r *observation) { r.Delta["snapshots"] = 1 }, "reuse changed snapshots"),
		Entry("missing source proof", func(r *observation) { r.SourceVerified = 0 }, "no source round-trip"),
	)
})
