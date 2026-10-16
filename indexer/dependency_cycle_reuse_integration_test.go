package indexer

import (
	"os/exec"
	"path/filepath"

	rpchttp "github.com/flanksource/clicky/rpc/http"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("dependency cycle reuse", func() {
	It("type-checks each module of a cyclic local graph once per run, and an unchanged graph not at all", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		modules := []struct{ name, requires string }{
			{"first", "require (\n\texample.org/second v0.0.0\n\texample.org/shared v0.0.0\n)\n"},
			{"second", "require example.org/first v0.0.0\n"},
			{"shared", ""},
		}
		work := "go 1.26\n\nuse (\n"
		for _, module := range modules {
			work += "\t./" + module.name + "\n"
			writeModule(filepath.Join(workspace, module.name), "example.org/"+module.name, "\n"+module.requires, "package "+module.name+"\n\nfunc Value() int { return 1 }\n")
		}
		writeFile(filepath.Join(workspace, "go.work"), work+")\n")
		for _, args := range [][]string{{"init"}, {"add", "."}, {"-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "-m", "test: cyclic graph"}} {
			output, err := exec.CommandContext(ctx, "git", append([]string{"-C", workspace}, args...)...).CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(output))
		}
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		requests := []ModuleOptions{
			{Path: filepath.Join(workspace, "first"), Reason: storage.ReasonAdd},
			{Path: filepath.Join(workspace, "second"), Reason: storage.ReasonAdd},
		}
		typechecks := func() int64 {
			GinkgoHelper()
			run, err := StartModulesTask(ctx, engine, requests)
			Expect(err).ToNot(HaveOccurred())
			waiting, timings := rpchttp.WithTimings(ctx)
			_, err = run.Wait(waiting)
			Expect(err).ToNot(HaveOccurred())
			calls, _ := timings.Counter("typecheck", "calls")
			return calls
		}

		Expect(typechecks()).To(Equal(int64(len(modules))), "the second target reuses the first target's graph from the run memo")
		Expect(typechecks()).To(BeZero(), "every member's head is reusable without type-checking")
	})

	Context("with a nested module in a cycle whose enclosing module is not in the graph", func() {
		var workspace, examples, entity string
		BeforeEach(func() {
			workspace = canonicalTempDir()
			examples, entity = filepath.Join(workspace, "examples"), filepath.Join(workspace, "examples", "entity")
			writeModule(examples, "example.org/examples", "", "package examples\n")
			writeModule(entity, "example.org/entity", "\nrequire example.org/other v0.0.0\n\nreplace example.org/other => ../../other\n", "package entity\n")
			writeModule(filepath.Join(workspace, "other"), "example.org/other", "\nrequire example.org/entity v0.0.0\n\nreplace example.org/entity => ../examples/entity\n", "package other\n")
		})

		It("links the nested module to its registered enclosing module", func(ctx SpecContext) {
			database := openIndexerDB(ctx, indexerSQLiteOptions())
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			_, err = engine.IndexModules(ctx, ModuleOptions{Path: workspace, ExactLocation: examples, Reason: storage.ReasonAdd})
			Expect(err).ToNot(HaveOccurred())
			_, err = engine.IndexModules(ctx, ModuleOptions{Path: workspace, ExactLocation: entity, Reason: storage.ReasonAdd})
			Expect(err).ToNot(HaveOccurred())
			var parent, child storage.ModuleLocation
			Expect(database.Where("canonical_path = ?", examples).Take(&parent).Error).To(Succeed())
			Expect(database.Where("canonical_path = ?", entity).Take(&child).Error).To(Succeed())
			Expect(child.ParentLocationID).To(Equal(&parent.ID))
		})

		It("indexes the enclosing module first when a run lists the nested module before it", func(ctx SpecContext) {
			database := openIndexerDB(ctx, indexerSQLiteOptions())
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			run, err := StartModulesTask(ctx, engine, []ModuleOptions{
				{Path: workspace, ExactLocation: entity, Reason: storage.ReasonAdd},
				{Path: workspace, ExactLocation: examples, Reason: storage.ReasonAdd},
			})
			Expect(err).ToNot(HaveOccurred())
			_, err = run.Wait(ctx)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
