package indexer

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("typed load configuration", func() {
	DescribeTable("keeps -mod=readonly whatever else the variant adds",
		func(tags []string, modFile string, expected []string) {
			directory := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module example.org/flags\n\ngo 1.22\n"), 0o600)).To(Succeed())
			root := discoveredRoot{LocalPath: directory, ModFile: modFile, Variant: buildVariant{GOOS: "linux", GOARCH: "amd64", CGOEnabled: "0", BuildTags: tags}}
			config, _, err := typedLoadConfig(context.Background(), root, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.BuildFlags).To(Equal(expected))
		},
		Entry("no tags", nil, "", []string{"-mod=readonly"}),
		Entry("build tags", []string{"integration", "e2e"}, "", []string{"-mod=readonly", "-tags=integration,e2e"}),
		Entry("build tags and a mod file", []string{"integration"}, "alt.mod", []string{"-mod=readonly", "-tags=integration", "-modfile=alt.mod"}),
	)
})
