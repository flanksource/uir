package query_test

import (
	"os"
	"path/filepath"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("module queries", func() {
	DescribeTable("resolves call references from source projections without losing history", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		workspace := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.org/calls\n\ngo 1.26\n"), 0o644)).To(Succeed())
		source := filepath.Join(workspace, "calls.go")
		Expect(os.WriteFile(source, []byte("package calls\n\nfunc Target() {}\nfunc Caller() { Target(); Missing() }\n"), 0o644)).To(Succeed())
		engine, err := indexer.New(database)
		Expect(err).ToNot(HaveOccurred())
		indexed, err := engine.IndexModules(ctx, indexer.ModuleOptions{Path: workspace})
		Expect(err).ToNot(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		callers, err := pipeline.RunModules(ctx, `callers of node where method = "Target"`, query.ModuleScopeOptions{RootKey: "example.org/calls"})
		Expect(err).ToNot(HaveOccurred())
		Expect(callers.Matches).To(HaveLen(1))
		Expect(callers.Matches[0].Identifier.Method).To(Equal("Caller"))
		callees, err := pipeline.RunModules(ctx, `callees of node where method = "Caller"`, query.ModuleScopeOptions{RootKey: "example.org/calls"})
		Expect(err).ToNot(HaveOccurred())
		Expect(callees.Matches).To(HaveLen(1))
		Expect(callees.Matches[0].Identifier.Method).To(Equal("Target"))
		unresolved, err := pipeline.RunModules(ctx, "unresolved calls", query.ModuleScopeOptions{RootKey: "example.org/calls"})
		Expect(err).ToNot(HaveOccurred())
		Expect(unresolved.Matches).To(HaveLen(1))
		Expect(unresolved.Matches[0].Identifier.Method).To(Equal("Missing"))
		Expect(os.Remove(source)).To(Succeed())
		_, err = engine.IndexModules(ctx, indexer.ModuleOptions{Path: workspace})
		Expect(err).ToNot(HaveOccurred())
		current, err := pipeline.RunModules(ctx, `nodes where method = "Target"`, query.ModuleScopeOptions{RootKey: "example.org/calls"})
		Expect(err).ToNot(HaveOccurred())
		Expect(current.Matches).To(BeEmpty())
		historical, err := pipeline.RunModules(ctx, `nodes where method = "Target"`, query.ModuleScopeOptions{SnapshotID: indexed[0].SnapshotID})
		Expect(err).ToNot(HaveOccurred())
		Expect(historical.Matches).To(HaveLen(1))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))

	DescribeTable("uses the primary head by default and exposes checkout candidates for calls", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		workspace := GinkgoT().TempDir()
		primary := filepath.Join(workspace, "primary")
		branch := filepath.Join(workspace, "branch")
		for _, path := range []string{primary, branch} {
			Expect(os.MkdirAll(path, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(path, "go.mod"), []byte("module example.org/service\n\ngo 1.26\n"), 0o644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(path, "service.go"), []byte("package service\n\nfunc Run() {}\n"), 0o644)).To(Succeed())
		}
		engine, err := indexer.New(database)
		Expect(err).ToNot(HaveOccurred())
		_, err = engine.IndexModules(ctx, indexer.ModuleOptions{Path: primary})
		Expect(err).ToNot(HaveOccurred())
		_, err = engine.IndexModules(ctx, indexer.ModuleOptions{Path: branch})
		Expect(err).ToNot(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		result, err := pipeline.RunModules(ctx, `nodes where method = "Run"`, query.ModuleScopeOptions{RootKey: "example.org/service"})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Matches).To(HaveLen(1))
		canonicalPrimary, err := filepath.EvalSymlinks(primary)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Matches[0].Location).To(Equal(canonicalPrimary))
		result, err = pipeline.RunModules(ctx, `callers of node where method = "Run"`, query.ModuleScopeOptions{RootKey: "example.org/service"})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Matches).To(HaveLen(2))
		for _, match := range result.Matches {
			Expect(match.Kind).To(Equal("candidate"))
		}
		result, err = pipeline.RunModules(ctx, `nodes where method = "Run"`, query.ModuleScopeOptions{RootKey: "example.org/service", Location: branch})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Matches).To(HaveLen(1))
		canonicalBranch, err := filepath.EvalSymlinks(branch)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Matches[0].Location).To(Equal(canonicalBranch))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))

	It("rejects a nil query database", func(ctx SpecContext) {
		pipeline := &query.Pipeline{}
		_, err := pipeline.RunModules(ctx, "nodes", query.ModuleScopeOptions{})
		Expect(err).To(MatchError("UIR query database is required"))
	})
})
