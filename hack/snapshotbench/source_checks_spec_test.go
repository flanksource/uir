//go:build hack

package main

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("snapshot replay evidence source samples", func() {
	DescribeTable("samples a real source before generated catalog metadata", func(paths []string, expected string) {
		selected, err := sampleSource(paths)
		Expect(err).ToNot(HaveOccurred())
		Expect(selected).To(Equal(expected))
	},
		Entry("rules", []string{"generation", "rules/pc/example/XMLData.xml"}, "rules/pc/example/XMLData.xml"),
		Entry("manifest", []string{"manifest.json", "rules/example.xml"}, "rules/example.xml"),
		Entry("Go", []string{"cmd/main.go", "main.go"}, "cmd/main.go"),
		Entry("generated-only catalog", []string{"manifest.json"}, "manifest.json"),
	)

	It("refuses to claim a round-trip for an empty source set", func() {
		_, err := sampleSource(nil)
		Expect(err).To(HaveOccurred())
	})
})
