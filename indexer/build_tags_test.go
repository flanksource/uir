package indexer

import (
	"path/filepath"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Build tag normalization", func() {
	It("retains selected tags when isolating historical analysis", func(ctx SpecContext) {
		root := canonicalTempDir()
		writeFile(filepath.Join(root, "go.mod"), "module example.org/historical\n\ngo 1.26\n")
		isolated, err := isolateRoot(ctx, discoveredRoot{LocalPath: root, AnalysisMode: storage.AnalysisTyped, Variant: buildVariant{BuildTags: []string{"feature"}}}, false, canonicalTempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(isolated.Variant.BuildTags).To(Equal([]string{"feature"}))
		Expect(isolated.Variant.GoWorkOff).To(BeTrue())
	})
	It("canonicalizes tag sets without mutating caller input", func() {
		tags := []string{"integration", "feature", "integration"}
		normalized, err := normalizeBuildTags(tags)
		Expect(err).NotTo(HaveOccurred())
		Expect(normalized).To(Equal([]string{"feature", "integration"}))
		Expect(tags).To(Equal([]string{"integration", "feature", "integration"}))
	})
	DescribeTable("rejects non-tag expressions", func(tag string) {
		_, err := normalizeBuildTags([]string{tag})
		Expect(err).To(MatchError(ContainSubstring("--tags")))
	}, Entry("empty", ""), Entry("negation", "!feature"), Entry("expression", "feature && integration"))
})
