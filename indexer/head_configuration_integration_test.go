package indexer

import (
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("head configuration", func() {
	DescribeTable("reads whether a checkout's head was indexed with its tests",
		func(ctx SpecContext, includeTests bool) {
			database := openIndexerDB(ctx, indexerSQLiteOptions())
			workspace := canonicalTempDir()
			writeTwoPackageModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			_, err = engine.IndexModules(ctx, ModuleOptions{Path: workspace, IncludeTests: includeTests, Reason: storage.ReasonAdd})
			Expect(err).ToNot(HaveOccurred())

			Expect(engine.HeadIncludesTests(ctx, workspace)).To(Equal(includeTests))
		},
		Entry("with tests", true),
		Entry("without tests", false),
	)

	It("fails for a checkout without a head", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		writeTwoPackageModule(workspace)
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())

		_, err = engine.HeadIncludesTests(ctx, workspace)

		Expect(err).To(MatchError(ContainSubstring("no indexed head")))
	})
})
