package indexer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// useModuleProxy makes the go command resolve modules from the file proxy at directory only.
func useModuleProxy(directory string) {
	GinkgoT().Setenv("GOPROXY", "file://"+directory)
	GinkgoT().Setenv("GOSUMDB", "off")
	GinkgoT().Setenv("GOWORK", "off")
}

// downloadModules records the go.sum of the module at directory from the proxy.
func downloadModules(ctx context.Context, directory string) {
	GinkgoHelper()
	command := exec.CommandContext(ctx, "go", "mod", "download", "all")
	command.Dir = directory
	output, err := command.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(output))
}

// writeTaggedModule publishes module v1.0.0 to the proxy and writes a checkout of it whose commit is
// tagged v1.0.0; requirement is appended to its go.mod.
func writeTaggedModule(ctx context.Context, proxy, checkout, module, requirement string) {
	GinkgoHelper()
	manifest := "module " + module + "\n\ngo 1.26\n" + requirement
	writeModuleProxy(proxy, module, "v1.0.0", manifest)
	Expect(os.MkdirAll(checkout, 0o755)).To(Succeed())
	writeFile(filepath.Join(checkout, "go.mod"), manifest)
	writeFile(filepath.Join(checkout, "source.go"), "package source\n\nfunc Value() int { return 1 }\n")
	if requirement != "" {
		downloadModules(ctx, checkout)
	}
	gitOutput(ctx, checkout, "init")
	commitPaths(ctx, checkout, "test: tagged module", ".")
	gitOutput(ctx, checkout, "tag", "v1.0.0")
}

// writeVersionedConsumer writes a module that requires module v1.0.0 from the proxy without importing it.
func writeVersionedConsumer(ctx context.Context, directory, name, module string) {
	GinkgoHelper()
	writeModule(directory, "example.org/"+name, "\nrequire "+module+" v1.0.0\n", "package "+name+"\n\nfunc Run() int { return 1 }\n")
	downloadModules(ctx, directory)
}

var _ = Describe("dependency-aware head reuse", func() {
	DescribeTable("reuses the head of a root with a local dependency until the dependency gets a new snapshot",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := canonicalTempDir()
			app, library := filepath.Join(workspace, "app"), filepath.Join(workspace, "library")
			writeModule(library, "example.org/library", "", "package library\n\nfunc Price() int { return 1 }\n")
			writeModule(app, "example.org/app", "\nrequire example.org/library v0.0.0\n\nreplace example.org/library => ../library\n", "package app\n\nfunc Total() int { return 1 }\n")
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			loads := countLoads(engine)
			first := indexOnce(ctx, engine, app)
			Expect(*loads).To(Equal(2))

			again := indexOnce(ctx, engine, app)
			Expect(outcomeOf(again)).To(Equal(triggerOutcome{Unchanged: true, HeadVersion: 1, ReusedFiles: 1}))
			Expect(again.SnapshotID).To(Equal(first.SnapshotID))
			Expect(*loads).To(Equal(2), "an unchanged root whose dependency set hash is unchanged is not type-checked")

			writeFile(filepath.Join(library, "main.go"), "package library\n\nfunc Price() int { return 2 }\n")
			changed := indexOnce(ctx, engine, app)
			Expect(changed).To(And(HaveField("Unchanged", false), HaveField("HeadVersion", int64(2))))
			Expect(*loads).To(Equal(4), "library and then app are type-checked")
			before, after := loadSnapshot(database, first.SnapshotID), loadSnapshot(database, changed.SnapshotID)
			Expect([]string{after.ContentSetHash, after.ConfigurationHash}).To(Equal([]string{before.ContentSetHash, before.ConfigurationHash}))
			Expect(after.DependencySetHash).ToNot(Equal(before.DependencySetHash))
			head, found, err := loadHead(ctx, database, discoveredRoot{RootKey: "example.org/library", LocalPath: library})
			Expect(err).ToNot(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(dependencyTarget(ctx, engine, changed.SnapshotID, "example.org/library")).To(Equal(head.ID))
			Expect(countRows(database, &storage.SourceDelta{}, "snapshot_id = ?", changed.SnapshotID)).To(BeZero())
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	DescribeTable("reuses the head of a root with a versioned dependency until the version's tag moves",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			base := canonicalTempDir()
			proxy, pricing, shop := filepath.Join(base, "proxy"), filepath.Join(base, "pricing"), filepath.Join(base, "shop")
			useModuleProxy(proxy)
			writeTaggedModule(ctx, proxy, pricing, "example.org/pricing", "")
			writeVersionedConsumer(ctx, shop, "shop", "example.org/pricing")
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			loads := countLoads(engine)
			indexOnce(ctx, engine, pricing)
			first := indexOnce(ctx, engine, shop)
			version := dependencyTarget(ctx, engine, first.SnapshotID, "example.org/pricing")
			indexed := *loads

			again := indexOnce(ctx, engine, shop)
			Expect(again).To(And(HaveField("Unchanged", true), HaveField("SnapshotID", first.SnapshotID)))
			Expect(*loads).To(Equal(indexed), "an unchanged root whose versioned dependency resolves to the same snapshot is not type-checked")

			writeFile(filepath.Join(pricing, "source.go"), "package source\n\nfunc Value() int { return 2 }\n")
			commitPaths(ctx, pricing, "test: move tag", "source.go")
			gitOutput(ctx, pricing, "tag", "-f", "v1.0.0")
			moved := indexOnce(ctx, engine, shop)
			Expect(moved).To(And(HaveField("Unchanged", false), HaveField("HeadVersion", int64(2))))
			Expect(dependencyTarget(ctx, engine, moved.SnapshotID, "example.org/pricing")).ToNot(Equal(version))
			Expect(loadSnapshot(database, moved.SnapshotID).DependencySetHash).ToNot(Equal(loadSnapshot(database, first.SnapshotID).DependencySetHash))
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", indexerPostgresOptions),
	)

	It("resolves each version tag of a run's versioned closures once", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		base := canonicalTempDir()
		proxy, shops := filepath.Join(base, "proxy"), filepath.Join(base, "shops")
		useModuleProxy(proxy)
		writeTaggedModule(ctx, proxy, filepath.Join(base, "pricing"), "example.org/pricing", "")
		writeTaggedModule(ctx, proxy, filepath.Join(base, "catalog"), "example.org/catalog", "\nrequire example.org/pricing v1.0.0\n")
		writeVersionedConsumer(ctx, filepath.Join(shops, "direct"), "direct", "example.org/pricing")
		writeVersionedConsumer(ctx, filepath.Join(shops, "transitive"), "transitive", "example.org/catalog")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		indexOnce(ctx, engine, filepath.Join(base, "pricing"))
		indexOnce(ctx, engine, filepath.Join(base, "catalog"))
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: shops, Reason: storage.ReasonAdd})
		Expect(err).ToNot(HaveOccurred())

		var mu sync.Mutex
		resolved := map[string]int{}
		memo := newRunMemo()
		memo.versionCommit = func(ctx context.Context, checkout, version string) (string, error) {
			mu.Lock()
			resolved[filepath.Base(checkout)+"@"+version]++
			mu.Unlock()
			return versionCommit(ctx, checkout, version)
		}
		loads := countLoads(engine)
		results, err := engine.IndexModules(withRunMemo(ctx, memo), ModuleOptions{Path: shops, Reason: storage.ReasonAdd})

		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveEach(HaveField("Unchanged", true)))
		Expect(*loads).To(BeZero())
		Expect(resolved).To(Equal(map[string]int{"pricing@v1.0.0": 1, "catalog@v1.0.0": 1}),
			"pricing is in the closure of both shops' dependencies, and its tag is resolved once")
	})
})
