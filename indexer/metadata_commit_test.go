package indexer

import (
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("extraction reuse identity", func() {
	DescribeTable("invalidates identical sources when stored context changes", func(change func(*storage.ModuleSnapshot)) {
		root := discoveredRoot{ContentSetHash: hashBytes([]byte("sources and manifests")), ConfigurationHash: hashBytes([]byte("build policy"))}
		dependencies := dependencyHash(root.Dependencies)
		base := storage.ModuleSnapshot{ID: uuid.New(), ContentSetHash: root.ContentSetHash, ConfigurationHash: root.ConfigurationHash, DependencySetHash: &dependencies, Coverage: storage.CoverageIndexed}
		Expect(sameExtractionInputs(base, root)).To(BeTrue())
		change(&base)
		Expect(sameExtractionInputs(base, root)).To(BeFalse())
	},
		Entry("dependency target changes", func(base *storage.ModuleSnapshot) {
			changed := hashBytes([]byte("another selected dependency snapshot"))
			base.DependencySetHash = &changed
		}),
		Entry("dependency selection was not recorded", func(base *storage.ModuleSnapshot) { base.DependencySetHash = nil }),
		Entry("producer or build configuration changes", func(base *storage.ModuleSnapshot) { base.ConfigurationHash = hashBytes([]byte("new build policy")) }),
		Entry("no base snapshot", func(base *storage.ModuleSnapshot) { base.ID = uuid.Nil }),
	)
})
