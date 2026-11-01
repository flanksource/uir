package uir_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	"github.com/flanksource/uir"
)

var _ = Describe("Location.Pretty", func() {
	DescribeTable("renders the file name and line numbers, not pointer addresses",
		func(location uir.Location, expected string) {
			Expect(location.Pretty().String()).To(Equal(expected))
		},
		Entry("a line range", uir.Location{Path: "pkg/store.go", StartLine: lo.ToPtr(12), EndLine: lo.ToPtr(18)}, "store.go:L12-18"),
		Entry("a single line", uir.Location{Path: "pkg/store.go", StartLine: lo.ToPtr(7), EndLine: lo.ToPtr(7)}, "store.go:L7"),
		Entry("a start line without an end", uir.Location{Path: "pkg/store.go", StartLine: lo.ToPtr(3)}, "store.go:L3"),
	)
})
