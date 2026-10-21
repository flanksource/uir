package indexer

import (
	"context"
	"os"
	"path/filepath"

	rpchttp "github.com/flanksource/clicky/rpc/http"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type checkpointTestRequest struct {
	engine  *Indexer
	options ModuleOptions
}

func checkpointTestRun(ctx context.Context, request checkpointTestRequest) ([]ModuleResult, *rpchttp.Timings) {
	GinkgoHelper()
	run, err := StartModulesTask(ctx, request.engine, []ModuleOptions{request.options})
	Expect(err).NotTo(HaveOccurred())
	waiting, timings := rpchttp.WithTimings(ctx)
	results, err := run.Wait(waiting)
	Expect(err).NotTo(HaveOccurred())
	return results, timings
}

var _ = Describe("Index checkpoints", func() {
	It("reuses versioned dependencies after their temporary worktrees are removed", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		base := canonicalTempDir()
		proxy, pricing, shop := filepath.Join(base, "proxy"), filepath.Join(base, "pricing"), filepath.Join(base, "shop")
		useModuleProxy(proxy)
		writeTaggedModule(ctx, proxy, pricing, "example.org/pricing", "")
		writeVersionedConsumer(ctx, shop, "shop", "example.org/pricing")
		gitOutput(ctx, shop, "init")
		commitPaths(ctx, shop, "initial", ".")
		engine, err := New(database)
		Expect(err).NotTo(HaveOccurred())
		indexOnce(ctx, engine, pricing)
		request := checkpointTestRequest{engine, ModuleOptions{Path: shop, Reason: storage.ReasonReindex}}
		first, _ := checkpointTestRun(ctx, request)
		again, timings := checkpointTestRun(ctx, request)
		Expect(again).To(ConsistOf(And(HaveField("SnapshotID", first[0].SnapshotID), HaveField("Unchanged", true))))
		calls, _ := timings.Counter("fingerprint-reuse", "calls")
		Expect(calls).To(Equal(int64(1)))
	})

	DescribeTable("skips discovery and dependencies across indexers for unchanged dirty checkout inputs", func(ctx SpecContext, options func() storage.DBOptions) {
		database := openIndexerDB(ctx, options())
		workspace := canonicalTempDir()
		writeTwoPackageModule(workspace)
		gitOutput(ctx, workspace, "init")
		commitPaths(ctx, workspace, "initial", "go.mod", "cart.go", "checkout.go", "tax/tax.go")
		writeFile(filepath.Join(workspace, "untracked.go"), "package shop\nfunc Untracked() {}\n")
		var first []ModuleResult
		for iteration := range 3 {
			engine, err := New(database)
			Expect(err).NotTo(HaveOccurred())
			run, err := StartModulesTask(ctx, engine, []ModuleOptions{{Path: workspace, IncludeTests: true, Reason: storage.ReasonReindex}})
			Expect(err).NotTo(HaveOccurred())
			waiting, timings := rpchttp.WithTimings(ctx)
			results, err := run.Wait(waiting)
			Expect(err).NotTo(HaveOccurred())
			if iteration == 0 {
				first = results
			} else {
				Expect(results).To(ConsistOf(And(HaveField("SnapshotID", first[0].SnapshotID), HaveField("Unchanged", true))))
				for _, phase := range []string{"discover", "dependencies", "typecheck", "verify-inputs"} {
					calls, _ := timings.Counter(phase, "calls")
					Expect(calls).To(BeZero(), phase)
				}
			}
		}
	}, Entry("SQLite", indexerSQLiteOptions), Entry("PostgreSQL", indexerPostgresOptions))

	DescribeTable("invalidates a cached request when inputs change", func(ctx SpecContext, change string) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		writeCheckouts(ctx, workspace, map[string][2]string{
			"lib": {"", "package lib\nfunc Base() int { return 1 }\n"},
			"app": {"\nrequire example.org/lib v0.0.0\n", "package app\nimport \"example.org/lib\"\nfunc Value() int { return lib.Base() }\n"},
		})
		engine, err := New(database)
		Expect(err).NotTo(HaveOccurred())
		app, library := filepath.Join(workspace, "app"), filepath.Join(workspace, "lib")
		request := checkpointTestRequest{engine, ModuleOptions{Path: app, Reason: storage.ReasonReindex}}
		checkpointTestRun(ctx, request)
		_, warm := checkpointTestRun(ctx, request)
		calls, _ := warm.Counter("fingerprint-reuse", "calls")
		Expect(calls).To(Equal(int64(1)))
		switch change {
		case "untracked":
			writeFile(filepath.Join(app, "new.go"), "package app\nfunc Added() {}\n")
		case "dependency":
			writeFile(filepath.Join(library, "main.go"), "package lib\nfunc Base() int { return 2 }\n")
		case "workspace":
			file := filepath.Join(workspace, "go.work")
			content, err := os.ReadFile(file)
			Expect(err).NotTo(HaveOccurred())
			writeFile(file, string(content)+"\n// changed workspace\n")
		case "HEAD":
			writeFile(filepath.Join(app, "README.md"), "metadata\n")
			commitPaths(ctx, app, "metadata", "README.md")
		case "force":
			request.options.Force = true
		case "tests":
			request.options.IncludeTests = true
		case "snapshot":
			_, err := engine.IndexModules(ctx, ModuleOptions{Path: app, ExactLocation: app, IncludeTests: true, Reason: storage.ReasonReindex})
			Expect(err).NotTo(HaveOccurred())
		}
		_, changed := checkpointTestRun(ctx, request)
		calls, _ = changed.Counter("discover", "calls")
		Expect(calls).To(BeNumerically(">", 0), "changed inputs must take the normal indexing path")
	}, Entry("untracked addition", "untracked"), Entry("dependency source", "dependency"), Entry("ignored workspace manifest", "workspace"),
		Entry("HEAD commit", "HEAD"), Entry("force", "force"), Entry("test selection", "tests"), Entry("saved snapshot head", "snapshot"))
})
