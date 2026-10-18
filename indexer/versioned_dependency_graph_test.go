package indexer

import (
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("stored version matching an extraction", func() {
	const context = "context"
	deterministic, undecided := true, false
	stored := func(coverage storage.Coverage, flag *bool) storage.ModuleSnapshot {
		return storage.ModuleSnapshot{ContextHash: context, Coverage: coverage, CoverageDeterministic: flag}
	}
	extracted := func(coverage storage.Coverage, flag bool) moduleExtraction {
		return moduleExtraction{contextHash: context, coverage: coverage, coverageDeterministic: flag}
	}
	DescribeTable("matches only the same context, coverage and, for degraded coverage, the same determinism",
		func(snapshot storage.ModuleSnapshot, extraction moduleExtraction, expected bool) {
			Expect(storedMatchesExtraction(snapshot, extraction)).To(Equal(expected))
		},
		Entry("complete coverage ignores the flag", stored(storage.CoverageIndexed, nil), extracted(storage.CoverageIndexed, false), true),
		Entry("degraded and both deterministic", stored(storage.CoverageSyntax, &deterministic), extracted(storage.CoverageSyntax, true), true),
		Entry("degraded, stored before the flag existed", stored(storage.CoverageSyntax, nil), extracted(storage.CoverageSyntax, true), false),
		Entry("degraded, stored undecided, now deterministic", stored(storage.CoverageSyntax, &undecided), extracted(storage.CoverageSyntax, true), false),
		Entry("degraded, both undecided", stored(storage.CoverageSyntax, &undecided), extracted(storage.CoverageSyntax, false), true),
		Entry("different coverage", stored(storage.CoverageIndexed, nil), extracted(storage.CoverageSyntax, true), false),
		Entry("different context", storage.ModuleSnapshot{ContextHash: "other", Coverage: storage.CoverageIndexed}, extracted(storage.CoverageIndexed, true), false),
	)
})
