//go:build hack

package main

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("performance impact summaries", func() {
	It("reports the midpoint median and nearest-rank p95 without reordering the samples", func() {
		values := []float64{40, 10, 30, 20}
		summary, err := impactSummary(values)
		Expect(err).ToNot(HaveOccurred())
		Expect(summary).To(Equal(impactDistribution{Count: 4, Min: 10, Median: 25, P95: 40, Max: 40}))
		Expect(values).To(Equal([]float64{40, 10, 30, 20}))
	})

	It("refuses an empty cohort instead of reporting zero latency", func() {
		_, err := impactSummary(nil)
		Expect(err).To(MatchError(ContainSubstring("no samples")))
	})
})
