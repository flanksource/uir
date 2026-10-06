package indexer

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/tools/go/packages"
)

// directoryLoads counts an engine's go/packages loads by the base name of the module directory loaded.
type directoryLoads struct {
	mu     sync.Mutex
	counts map[string]int
}

// countLoadsByDirectory makes the engine count its loads per module directory, calling before, when
// set, with the directory and its load count before each load runs.
func countLoadsByDirectory(engine *Indexer, before func(directory string, count int)) *directoryLoads {
	loads := &directoryLoads{counts: map[string]int{}}
	engine.loadPackages = func(config *packages.Config, patterns ...string) ([]*packages.Package, error) {
		loads.mu.Lock()
		loads.counts[filepath.Base(config.Dir)]++
		count := loads.counts[filepath.Base(config.Dir)]
		loads.mu.Unlock()
		if before != nil {
			before(config.Dir, count)
		}
		return packages.Load(config, patterns...)
	}
	return loads
}

func (loads *directoryLoads) snapshot() map[string]int {
	loads.mu.Lock()
	defer loads.mu.Unlock()
	counts := make(map[string]int, len(loads.counts))
	for directory, count := range loads.counts {
		counts[directory] = count
	}
	return counts
}

// sharedLibrary is two apps that require example.org/library, and through it example.org/base, by
// local replaces without importing them, and lib/, a go.work of library and base in which library
// imports base. Importing a workspace sibling makes library type-checked on every index, so how often
// a run loads it is how often the run indexes it.
type sharedLibrary struct {
	app1, app2, library string
}

func writeSharedLibrary(workspace string) sharedLibrary {
	GinkgoHelper()
	fixture := sharedLibrary{app1: filepath.Join(workspace, "app1"), app2: filepath.Join(workspace, "app2"), library: filepath.Join(workspace, "lib", "library")}
	writeModule(filepath.Join(workspace, "lib", "base"), "example.org/base", "", "package base\n\nfunc Base() int { return 1 }\n")
	writeModule(fixture.library, "example.org/library", "\nrequire example.org/base v0.0.0\n",
		"package library\n\nimport \"example.org/base\"\n\nfunc Price() int { return base.Base() }\n")
	writeFile(filepath.Join(workspace, "lib", "go.work"), "go 1.26\n\nuse (\n\t./base\n\t./library\n)\n")
	requirements := "\nrequire (\n\texample.org/base v0.0.0\n\texample.org/library v0.0.0\n)\n\n" +
		"replace example.org/base => ../lib/base\n\nreplace example.org/library => ../lib/library\n"
	for _, app := range []string{fixture.app1, fixture.app2} {
		name := filepath.Base(app)
		writeModule(app, "example.org/"+name, requirements, "package "+name+"\n\nfunc Run() int { return 1 }\n")
	}
	return fixture
}

// runTargets runs one module-index run over every path and returns its results by root key.
func runTargets(ctx context.Context, engine *Indexer, paths ...string) map[string]ModuleResult {
	GinkgoHelper()
	requests := make([]ModuleOptions, 0, len(paths))
	for _, path := range paths {
		requests = append(requests, ModuleOptions{Path: path, Reason: storage.ReasonAdd})
	}
	run, err := StartModulesTask(ctx, engine, requests)
	Expect(err).ToNot(HaveOccurred())
	results, err := run.Wait(ctx)
	Expect(err).ToNot(HaveOccurred())
	byRoot := map[string]ModuleResult{}
	for _, result := range results {
		byRoot[result.RootKey] = result
	}
	Expect(byRoot).To(HaveLen(len(paths)))
	return byRoot
}

// dependencyTarget is the snapshot the edge of snapshot to modulePath targets.
func dependencyTarget(ctx context.Context, engine *Indexer, snapshot, modulePath string) uuid.UUID {
	GinkgoHelper()
	dependencies, err := storage.ModuleDependencies(ctx, engine.database, uuid.MustParse(snapshot))
	Expect(err).ToNot(HaveOccurred())
	for _, edge := range dependencies.Items {
		if edge.ModulePath == modulePath {
			Expect(edge.TargetSnapshotID).ToNot(BeNil(), "%+v", edge)
			return *edge.TargetSnapshotID
		}
	}
	Fail("snapshot " + snapshot + " has no dependency on " + modulePath)
	return uuid.Nil
}

var _ = Describe("module index run memo", func() {
	DescribeTable("type-checks a local dependency that two targets share once per run",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			fixture := writeSharedLibrary(canonicalTempDir())
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			loads := countLoadsByDirectory(engine, nil)

			first := runTargets(ctx, engine, fixture.app1, fixture.app2)
			Expect(loads.snapshot()).To(Equal(map[string]int{"base": 1, "library": 1, "app1": 1, "app2": 1}))
			Expect(dependencyTarget(ctx, engine, first["example.org/app1"].SnapshotID, "example.org/library")).
				To(Equal(dependencyTarget(ctx, engine, first["example.org/app2"].SnapshotID, "example.org/library")))

			again := runTargets(ctx, engine, fixture.app1, fixture.app2)
			Expect(loads.snapshot()).To(Equal(map[string]int{"base": 1, "library": 2, "app1": 1, "app2": 1}),
				"an unchanged run type-checks library, which imports a sibling, once, and reuses every other head")
			for _, root := range []string{"example.org/app1", "example.org/app2"} {
				Expect(again[root]).To(And(HaveField("Unchanged", true), HaveField("SnapshotID", first[root].SnapshotID)))
			}
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	It("indexes a remembered local dependency again when its sources change and the target retries", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		fixture := writeSharedLibrary(canonicalTempDir())
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		source := filepath.Join(fixture.library, "main.go")
		loads := countLoadsByDirectory(engine, func(directory string, count int) {
			if filepath.Base(directory) == "app2" && count == 1 {
				writeFile(source, "package library\n\nimport \"example.org/base\"\n\nfunc Price() int { return 2 * base.Base() }\n")
			}
		})

		results := runTargets(ctx, engine, fixture.app1, fixture.app2)

		Expect(loads.snapshot()).To(Equal(map[string]int{"base": 1, "library": 2, "app1": 1, "app2": 2}),
			"the retry forgot the run's library snapshot, so it indexed the changed library before app2 again")
		head, found, err := loadHead(ctx, database, discoveredRoot{RootKey: "example.org/library", LocalPath: fixture.library})
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(dependencyTarget(ctx, engine, results["example.org/app2"].SnapshotID, "example.org/library")).To(Equal(head.ID))
		Expect(dependencyTarget(ctx, engine, results["example.org/app1"].SnapshotID, "example.org/library")).ToNot(Equal(head.ID))
	})
})
