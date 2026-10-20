package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime/trace"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Captain historical profile", func() {
	It("replays the exact first and second commits and then measures stored revision reuse", func() {
		first := "567b8ab33aec1c8aafcefec233894d16153e4337"
		second := "246a1cbfc2e48dc5567165a68890c734cc02e99d"
		measurements := captainMeasurements(captainSequence{First: first, Second: second, Path: "historical"})
		Expect(measurements).To(Equal([]measurement{
			{ID: "00-first", Corpus: "captain", Producer: "go", Path: "historical", Kind: "revision", Commit: first, RootKey: "github.com/flanksource/captain"},
			{ID: "01-second", Corpus: "captain", Producer: "go", Path: "historical", Kind: "revision", Commit: second, RootKey: "github.com/flanksource/captain"},
			{ID: "02-repeat", Corpus: "captain", Producer: "go", Path: "historical", Kind: "repeat", Commit: second, RootKey: "github.com/flanksource/captain"},
		}))
	})

	It("replays initial, changed and unchanged heads through the head indexing path", func() {
		sequence := captainMeasurements(captainSequence{First: captainFirst, Second: captainSecond, Path: "head"})
		Expect(sequence).To(Equal([]measurement{
			{ID: "00-first", Corpus: "captain", Producer: "go", Path: "head", Kind: "initial", Commit: captainFirst, RootKey: "github.com/flanksource/captain"},
			{ID: "01-second", Corpus: "captain", Producer: "go", Path: "head", Kind: "revision", Commit: captainSecond, RootKey: "github.com/flanksource/captain"},
			{ID: "02-repeat", Corpus: "captain", Producer: "go", Path: "head", Kind: "repeat", Commit: captainSecond, RootKey: "github.com/flanksource/captain"},
		}))
	})

	It("rejects an unknown indexing path before cloning or opening a database", func() {
		Expect(runCaptain(context.Background(), []string{"--path", "unknown"})).To(MatchError("captain indexing path must be historical or head"))
	})

	It("counts changed package inputs by all files that must receive a new document", func() {
		before := map[string]storage.PackageCoverage{
			"stable":  {PackagePath: "stable", InputHash: "same", FileCount: 8},
			"changed": {PackagePath: "changed", InputHash: "old", FileCount: 30},
			"removed": {PackagePath: "removed", InputHash: "gone", FileCount: 2},
		}
		after := map[string]storage.PackageCoverage{
			"stable":  {PackagePath: "stable", InputHash: "same", FileCount: 8},
			"changed": {PackagePath: "changed", InputHash: "new", FileCount: 31},
			"added":   {PackagePath: "added", InputHash: "added", FileCount: 3},
		}
		Expect(comparePackageCoverage(before, after)).To(Equal(packageChangeSummary{
			ChangedPackages: 2, ChangedFiles: 34, RemovedPackages: 1,
			Rows: []packageChange{{Path: "added", Files: 3, Kind: "added"}, {Path: "changed", Files: 31, Kind: "changed"}, {Path: "removed", Files: 2, Kind: "removed"}},
		}))
	})

	DescribeTable("labels SQL by operation and table for cost attribution", func(sql, expected string) {
		Expect(sqlLabel(sql)).To(Equal(expected))
	},
		Entry("select", `SELECT * FROM "documents" WHERE root_id = ?`, "SELECT documents"),
		Entry("insert", `INSERT INTO "symbol_postings" (symbol_id) VALUES (?)`, "INSERT symbol_postings"),
		Entry("update", `UPDATE "snapshots" SET completed_at = ?`, "UPDATE snapshots"),
		Entry("delete", `DELETE FROM "source_deltas" WHERE snapshot_id = ?`, "DELETE source_deltas"),
	)

	It("keeps dependency and root phases separate in one execution trace", func() {
		directory := filepath.Join("..", "..", ".tmp", "snapshotbench-trace-tests")
		Expect(os.MkdirAll(directory, 0o755)).To(Succeed())
		file, err := os.CreateTemp(directory, "paths-*.trace")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.Remove, file.Name())
		Expect(trace.Start(file)).To(Succeed())
		ctx := context.Background()
		trace.WithRegion(ctx, "versioned-deps", func() {
			trace.WithRegion(ctx, "extract", func() {})
		})
		trace.WithRegion(ctx, "extract", func() {})
		trace.Stop()
		Expect(file.Close()).To(Succeed())
		phases, paths, err := tracePhaseBreakdown(file.Name())
		Expect(err).ToNot(HaveOccurred())
		Expect(phases["extract"].Count).To(Equal(2))
		Expect(paths["versioned-deps/extract"].Count).To(Equal(1))
		Expect(paths["extract"].Count).To(Equal(1))
	})
})
