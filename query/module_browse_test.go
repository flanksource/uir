package query_test

import (
	"os"
	"path/filepath"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("module browsing", func() {
	DescribeTable("reads immutable source and node projections", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		workspace := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.org/browse\n\ngo 1.26\n"), 0o644)).To(Succeed())
		path := filepath.Join(workspace, "browse.go")
		Expect(os.WriteFile(path, []byte("package browse\n\nfunc Target() {}\nfunc Caller() { Target() }\n"), 0o644)).To(Succeed())
		engine, err := indexer.New(database)
		Expect(err).To(Succeed())
		published, err := engine.IndexModules(ctx, indexer.ModuleOptions{Path: workspace})
		Expect(err).To(Succeed())
		pipeline, err := query.NewPipeline(database)
		Expect(err).To(Succeed())
		result, err := pipeline.BrowseModules(ctx, query.ModuleScopeOptions{RootKey: "example.org/browse", SnapshotID: published[0].SnapshotID})
		Expect(err).To(Succeed())
		Expect(result.Sources).To(HaveLen(1))
		Expect(result.Sources[0].Path).To(Equal("browse.go"))
		Expect(result.Sources[0].ContentHash).ToNot(BeEmpty())
		Expect(result.Nodes).To(ContainElement(And(HaveField("Symbol", ContainSubstring("Target")), HaveField("Path", "browse.go"))))
		Expect(result.Nodes).To(ContainElement(HaveField("Calls", HaveLen(1))))
		content, err := pipeline.ReadModuleSource(ctx, published[0].SnapshotID, "browse.go")
		Expect(err).To(Succeed())
		Expect(content.Content).To(ContainSubstring("func Target()"))
		Expect(content.Origin).To(Equal("local"))

		Expect(os.Remove(path)).To(Succeed())
		_, err = engine.IndexModules(ctx, indexer.ModuleOptions{Path: workspace})
		Expect(err).To(Succeed())
		historical, err := pipeline.BrowseModules(ctx, query.ModuleScopeOptions{SnapshotID: published[0].SnapshotID})
		Expect(err).To(Succeed())
		Expect(historical).To(Equal(result))
		_, err = pipeline.ReadModuleSource(ctx, published[0].SnapshotID, "browse.go")
		Expect(err).To(MatchError(ContainSubstring("no pinned Git revision")))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
})
