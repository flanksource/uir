package indexer

import (
	"path/filepath"

	rpchttp "github.com/flanksource/clicky/rpc/http"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("dependency read memo", func() {
	It("reads each module's dependencies once per run, and again in the next run after they change", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		fixture := writeSharedLibrary(workspace)
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		reads := func() int64 {
			GinkgoHelper()
			run, err := StartModulesTask(ctx, engine, []ModuleOptions{
				{Path: fixture.app1, Reason: storage.ReasonAdd}, {Path: fixture.app2, Reason: storage.ReasonAdd},
			})
			Expect(err).ToNot(HaveOccurred())
			waiting, timings := rpchttp.WithTimings(ctx)
			_, err = run.Wait(waiting)
			Expect(err).ToNot(HaveOccurred())
			count, _ := timings.Counter("dependencies", "reads")
			return count
		}
		modules := []string{"app1", "app2", "library", "base"}

		Expect(reads()).To(Equal(int64(len(modules))), "library and base are shared by both targets' graphs and local-dependency steps")

		extra := filepath.Join(workspace, "lib", "extra")
		writeModule(extra, "example.org/extra", "", "package extra\n")
		writeFile(filepath.Join(workspace, "lib", "go.work"), "go 1.26\n\nuse (\n\t./base\n\t./extra\n\t./library\n)\n")
		writeFile(filepath.Join(fixture.library, "go.mod"), "module example.org/library\n\ngo 1.26\n\nrequire (\n\texample.org/base v0.0.0\n\texample.org/extra v0.0.0\n)\n")

		Expect(reads()).To(Equal(int64(len(modules)+1)), "the next run reads every module again, and extra once")
		library, found, err := loadHead(ctx, database, discoveredRoot{RootKey: "example.org/library", LocalPath: fixture.library})
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		head, found, err := loadHead(ctx, database, discoveredRoot{RootKey: "example.org/extra", LocalPath: extra})
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(dependencyTarget(ctx, engine, library.ID.String(), "example.org/extra")).To(Equal(head.ID))
	})
})
