package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	clickytask "github.com/flanksource/clicky/task"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/taskruns"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/tools/go/packages"
)

// runModules starts a module-index run and waits for its outcome.
func runModules(ctx context.Context, engine *Indexer, options ModuleOptions) ([]ModuleResult, error) {
	run, err := StartModulesTask(ctx, engine, []ModuleOptions{options})
	if err != nil {
		return nil, err
	}
	return run.Wait(ctx)
}

// runTasks returns the run's task snapshots by name, and its group snapshot.
func runTasks(id string) (map[string]clickytask.TaskSnapshot, clickytask.TaskSnapshot) {
	GinkgoHelper()
	tasks := map[string]clickytask.TaskSnapshot{}
	var group clickytask.TaskSnapshot
	for _, snapshot := range clickytask.SnapshotByID(id) {
		if snapshot.Type == "group" {
			group = snapshot
			continue
		}
		tasks[snapshot.Name] = snapshot
	}
	Expect(group.GroupID).To(Equal(id))
	return tasks, group
}

// writeModule writes a one-file module at directory.
func writeModule(directory, module, manifest, source string) {
	GinkgoHelper()
	Expect(os.MkdirAll(directory, 0o755)).To(Succeed())
	writeFile(filepath.Join(directory, "go.mod"), "module "+module+"\n\ngo 1.26\n"+manifest)
	writeFile(filepath.Join(directory, "main.go"), source)
}

var _ = Describe("module index tasks", func() {
	It("indexes again, automatically, when a source changes while the index runs", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		writeTwoPackageModule(workspace)
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		var loads atomic.Int32
		engine.loadPackages = func(config *packages.Config, patterns ...string) ([]*packages.Package, error) {
			loaded, err := packages.Load(config, patterns...)
			if loads.Add(1) == 1 {
				writeFile(filepath.Join(workspace, "checkout.go"), "package shop\n\nfunc Checkout(c *Cart) int { return 2 * len(c.Items) }\n")
			}
			return loaded, err
		}

		results, err := runModules(ctx, engine, ModuleOptions{Path: workspace, Reason: storage.ReasonAdd})

		Expect(err).ToNot(HaveOccurred())
		Expect(loads.Load()).To(Equal(int32(2)), "the first extraction saw the old source, so the index ran again")
		published := loadSnapshot(database, results[0].SnapshotID)
		Expect(published.TaskRunID).ToNot(BeNil(), "a task-run publication records its run")
		Expect(indexOnce(ctx, engine, workspace).Unchanged).To(BeTrue(), "the published snapshot is of the changed source")
	})

	It("fails an index once, whatever its error says, instead of retrying it by message", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		writeTwoPackageModule(workspace)
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		var loads atomic.Int32
		engine.loadPackages = func(*packages.Config, ...string) ([]*packages.Package, error) {
			loads.Add(1)
			return nil, errors.New("dial tcp 127.0.0.1:443: connection refused: timeout")
		}

		_, err = runModules(ctx, engine, ModuleOptions{Path: workspace, Reason: storage.ReasonAdd})

		Expect(err).To(MatchError(ContainSubstring("connection refused")))
		Expect(loads.Load()).To(Equal(int32(1)))
	})

	It("indexes each module as its own task, reports every outcome, and keeps the modules that succeeded", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		good, broken := filepath.Join(workspace, "good"), filepath.Join(workspace, "broken")
		writeModule(good, "example.org/good", "", "package good\n\nfunc Run() {}\n")
		writeModule(broken, "example.org/broken", "", "package broken\n\nfunc Run() {}\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		engine.loadPackages = func(config *packages.Config, patterns ...string) ([]*packages.Package, error) {
			if config.Dir == broken {
				return nil, errors.New("go list failed for the broken module")
			}
			return packages.Load(config, patterns...)
		}

		run, err := StartModulesTask(ctx, engine, []ModuleOptions{{Path: workspace, Reason: storage.ReasonAdd}})
		Expect(err).ToNot(HaveOccurred())
		results, err := run.Wait(ctx)

		Expect(err).To(MatchError(And(ContainSubstring("1 of 2 modules failed"), ContainSubstring("go list failed for the broken module"))))
		Expect(results).To(ConsistOf(
			And(HaveField("RootKey", "example.org/broken"), HaveField("Location", broken), HaveField("Error", ContainSubstring("go list failed"))),
			And(HaveField("RootKey", "example.org/good"), HaveField("Location", good), HaveField("Error", ""), HaveField("SnapshotID", Not(BeEmpty()))),
		))
		Expect(countRows(database, &storage.ModuleLocationHead{}, "1 = 1")).To(Equal(int64(1)), "the module that succeeded stays published")
		tasks, group := runTasks(run.ID())
		Expect(group.Status).To(Equal(string(clickytask.StatusFailed)))
		Expect(tasks).To(And(HaveKeyWithValue("example.org/good", HaveField("Status", string(clickytask.StatusSuccess))),
			HaveKeyWithValue("example.org/broken", HaveField("Status", string(clickytask.StatusFailed)))))
		var good0 ModuleResult
		for _, result := range results {
			if result.RootKey == "example.org/good" {
				good0 = result
			}
		}
		Expect(group.Details).To(Equal(taskruns.RunDetails{SnapshotIDs: []string{good0.SnapshotID}}), "the run records the snapshots it published")
	})

	It("shows each local dependency it indexes on the way as a step of the run", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		app, library := filepath.Join(workspace, "app"), filepath.Join(workspace, "library")
		writeModule(library, "example.org/library", "", "package library\n\nfunc Price() int { return 1 }\n")
		writeModule(app, "example.org/app", "\nrequire example.org/library v0.0.0\n\nreplace example.org/library => ../library\n",
			"package app\n\nimport \"example.org/library\"\n\nfunc Total() int { return library.Price() }\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())

		run, err := StartModulesTask(ctx, engine, []ModuleOptions{{Path: app, Reason: storage.ReasonAdd}})
		Expect(err).ToNot(HaveOccurred())
		results, err := run.Wait(ctx)

		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(ConsistOf(HaveField("RootKey", "example.org/app")))
		tasks, group := runTasks(run.ID())
		Expect(tasks).To(HaveKeyWithValue("local dependency "+library, HaveField("Status", string(clickytask.StatusSuccess))))
		Expect(group.Details).To(HaveField("SnapshotIDs", HaveLen(2)), "the dependency's snapshot was published under the run too")
	})

	It("shares one run between concurrent identical requests", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		writeTwoPackageModule(workspace)
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		release := make(chan struct{})
		var loads atomic.Int32
		engine.loadPackages = func(config *packages.Config, patterns ...string) ([]*packages.Package, error) {
			loads.Add(1)
			<-release
			return packages.Load(config, patterns...)
		}
		options := ModuleOptions{Path: workspace, Reason: storage.ReasonAdd}

		first, err := StartModulesTask(ctx, engine, []ModuleOptions{options})
		Expect(err).ToNot(HaveOccurred())
		Eventually(loads.Load).Should(Equal(int32(1)))
		second, err := StartModulesTask(ctx, engine, []ModuleOptions{options})
		Expect(err).ToNot(HaveOccurred())
		reindex, err := StartModulesTask(ctx, engine, []ModuleOptions{{Path: workspace, Reason: storage.ReasonReindex, ExistingOnly: true, Force: true}})
		close(release)

		Expect(second.ID()).To(Equal(first.ID()))
		Expect(err).To(MatchError(ContainSubstring("is not registered")), "a different request is a different run, validated on its own")
		Expect(reindex).To(BeNil())
		firstResults, err := first.Wait(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(second.Wait(ctx)).To(Equal(firstResults))
		Expect(loads.Load()).To(Equal(int32(1)))
	})

	It("settles concurrent first-time indexes of one checkout on one head", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerPostgresOptions())
		workspace := canonicalTempDir()
		writeTwoPackageModule(workspace)
		var arrived atomic.Int32
		bothExtracted := make(chan struct{})
		results := make([][]ModuleResult, 2)
		errs := make([]error, 2)
		var group sync.WaitGroup
		for index := range results {
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			engine.loadPackages = func(config *packages.Config, patterns ...string) ([]*packages.Package, error) {
				loaded, err := packages.Load(config, patterns...)
				if count := arrived.Add(1); count == 2 {
					close(bothExtracted)
				} else if count < 2 {
					<-bothExtracted
				}
				return loaded, err
			}
			group.Go(func() {
				results[index], errs[index] = engine.indexTarget(ctx, ModuleOptions{Path: workspace, ExactLocation: workspace, Reason: storage.ReasonAdd}, GinkgoWriter.Printf)
			})
		}
		group.Wait()

		Expect(errs).To(HaveEach(BeNil()))
		Expect(results[0][0].SnapshotID).To(Equal(results[1][0].SnapshotID))
		Expect(countRows(database, &storage.ModuleLocationHead{}, "1 = 1")).To(Equal(int64(1)))
		Expect(countRows(database, &storage.ModuleSnapshot{}, "1 = 1")).To(Equal(int64(1)))
	})

	It("reindexes every registered checkout without a head and reports each one's outcome", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		present, removed := filepath.Join(workspace, "present"), filepath.Join(workspace, "removed")
		writeModule(present, "example.org/present", "", "package present\n")
		writeModule(removed, "example.org/removed", "", "package removed\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		_, err = runModules(ctx, engine, ModuleOptions{Path: workspace, Reason: storage.ReasonAdd})
		Expect(err).ToNot(HaveOccurred())
		Expect(database.Exec("DELETE FROM location_heads").Error).To(Succeed())
		Expect(os.RemoveAll(removed)).To(Succeed())

		run, err := StartMissingModulesTask(ctx, engine, false)
		Expect(err).ToNot(HaveOccurred())
		results, err := run.Wait(ctx)

		Expect(err).To(MatchError(ContainSubstring("1 of 2 modules failed")))
		Expect(results).To(ConsistOf(
			And(HaveField("RootKey", "example.org/present"), HaveField("SnapshotID", Not(BeEmpty()))),
			And(HaveField("RootKey", "example.org/removed"), HaveField("Location", removed), HaveField("Error", Not(BeEmpty()))),
		))
	})
})
