package query

import (
	"fmt"

	"github.com/flanksource/uir/internal/buildconstraint"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL build constraint header cache", func() {
	It("evicts old hashes and clears parsed facts when the index cache resets", func() {
		cache := newIndexCache(1)
		header, err := buildconstraint.Parse([]byte("//go:build hack\n\npackage sample\n"))
		Expect(err).NotTo(HaveOccurred())
		const entries = 4096
		for index := range entries + 1 {
			cache.buildHeaders.put(fmt.Sprintf("hash-%d", index), header)
		}
		_, old := cache.buildHeaders.get("hash-0")
		latest, kept := cache.buildHeaders.get(fmt.Sprintf("hash-%d", entries))
		Expect([]any{old, kept, latest.String()}).To(Equal([]any{false, true, "hack"}))
		cache.reset()
		_, kept = cache.buildHeaders.get(fmt.Sprintf("hash-%d", entries))
		Expect(kept).To(BeFalse())
	})
})
