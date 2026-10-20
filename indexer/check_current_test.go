package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	rpchttp "github.com/flanksource/clicky/rpc/http"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

var _ = Describe("checking an indexed checkout", func() {
	It("returns the current snapshot and rejects changed source and manifests", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		root := GinkgoT().TempDir()
		module := filepath.Join(root, "go.mod")
		source := filepath.Join(root, "main.go")
		writeFile(module, "module example.org/current\n\ngo 1.26\n")
		writeFile(source, "package current\n\nfunc Run() {}\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		indexed, err := engine.IndexModules(ctx, ModuleOptions{Path: root, IncludeTests: true, Reason: storage.ReasonAdd})
		Expect(err).ToNot(HaveOccurred())
		current, err := engine.CheckCurrent(ctx, root, CheckOptions{IncludeTests: true})
		Expect(err).ToNot(HaveOccurred())
		Expect(current.SnapshotID).To(Equal(indexed[0].SnapshotID))
		writeFile(source, "package current\n\nfunc Run() { Run() }\n")
		_, err = engine.CheckCurrent(ctx, root, CheckOptions{IncludeTests: true})
		Expect(err).To(MatchError(ContainSubstring("stale")))
		writeFile(source, "package current\n\nfunc Run() {}\n")
		Expect(os.WriteFile(module, []byte("module example.org/current\n\ngo 1.26\n\n// changed\n"), 0o644)).To(Succeed())
		_, err = engine.CheckCurrent(ctx, root, CheckOptions{IncludeTests: true})
		Expect(err).To(MatchError(ContainSubstring("stale")))
	})

	It("requires an indexed head with complete coverage", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		root := GinkgoT().TempDir()
		writeFile(filepath.Join(root, "go.mod"), "module example.org/coverage\n\ngo 1.26\n")
		writeFile(filepath.Join(root, "main.go"), "package coverage\n\nvar _ = missing\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		_, err = engine.CheckCurrent(ctx, root, CheckOptions{IncludeTests: true})
		Expect(err).To(MatchError(ContainSubstring("no indexed head")))
		_, err = engine.IndexModules(ctx, ModuleOptions{Path: root, IncludeTests: true, Reason: storage.ReasonAdd})
		Expect(err).ToNot(HaveOccurred())
		_, err = engine.CheckCurrent(ctx, root, CheckOptions{IncludeTests: true})
		Expect(err).To(MatchError(ContainSubstring("incomplete")))
	})

	It("accepts a head whose coverage is excluded", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		root := GinkgoT().TempDir()
		writeFile(filepath.Join(root, "go.mod"), "module example.org/excluded\n\ngo 1.26\n")
		writeFile(filepath.Join(root, "main.go"), "package excluded\n\nfunc Run() {}\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		indexed, err := engine.IndexModules(ctx, ModuleOptions{Path: root, IncludeTests: true, Reason: storage.ReasonAdd})
		Expect(err).ToNot(HaveOccurred())
		Expect(database.Model(&storage.ModuleSnapshot{}).Where("id = ?", indexed[0].SnapshotID).Update("coverage", storage.CoverageExcluded).Error).To(Succeed())
		current, err := engine.CheckCurrent(ctx, root, CheckOptions{IncludeTests: true})
		Expect(err).ToNot(HaveOccurred())
		Expect(current.SnapshotID).To(Equal(indexed[0].SnapshotID))
	})
})

// checkTypechecks runs CheckCurrent on the module at path and returns its result, how often it
// type-checked a module, and its error.
func checkTypechecks(ctx context.Context, engine *Indexer, path string) (ModuleResult, int64, error) {
	GinkgoHelper()
	timed, timings := rpchttp.WithTimings(ctx)
	result, err := engine.CheckCurrent(timed, path, CheckOptions{IncludeTests: false})
	calls, _ := timings.Counter("typecheck", "calls")
	return result, calls, err
}

var _ = Describe("checking an indexed checkout that imports a workspace sibling", func() {
	var database *gorm.DB
	var engine *Indexer
	var workspace, app, lib string
	var appHead string
	writeLibrary := func(ctx context.Context, rate, helper, message string) {
		writeFile(filepath.Join(lib, "main.go"), fmt.Sprintf(siblingLibrary, rate, helper))
		commitCheckout(ctx, lib, message)
	}

	BeforeEach(func(ctx SpecContext) {
		database = openIndexerDB(ctx, indexerSQLiteOptions())
		workspace = canonicalTempDir()
		writeCheckouts(ctx, workspace, map[string][2]string{"lib": {"", fmt.Sprintf(siblingLibrary, "2", "10")}, "app": {"", siblingApp}})
		app, lib = filepath.Join(workspace, "app"), filepath.Join(workspace, "lib")
		var err error
		engine, err = New(database)
		Expect(err).ToNot(HaveOccurred())
		Expect(runTypechecks(ctx, engine, lib, app)).To(Equal(int64(2)))
		results, err := engine.IndexModules(ctx, ModuleOptions{Path: app, Reason: storage.ReasonAdd})
		Expect(err).ToNot(HaveOccurred())
		appHead = results[0].SnapshotID
	})

	It("confirms the head through the sibling's recorded shape without type-checking or publishing", func(ctx SpecContext) {
		snapshots := countRows(database, &storage.ModuleSnapshot{}, "1 = 1")
		current, calls, err := checkTypechecks(ctx, engine, app)
		Expect(err).ToNot(HaveOccurred())
		Expect(current.SnapshotID).To(Equal(appHead))
		Expect(calls).To(BeZero())

		writeLibrary(ctx, "2", "11", "test: change an unexported helper")
		Expect(runTypechecks(ctx, engine, lib)).To(Equal(int64(1)))
		current, calls, err = checkTypechecks(ctx, engine, app)
		Expect(err).ToNot(HaveOccurred())
		Expect(current.SnapshotID).To(Equal(appHead))
		Expect(calls).To(BeZero(), "lib's new head presents the shape app recorded")
		Expect(countRows(database, &storage.ModuleSnapshot{}, "1 = 1")).To(Equal(snapshots+1), "only lib's index published")
	})

	It("reports stale after the sibling's exported API changes, publishing nothing", func(ctx SpecContext) {
		writeLibrary(ctx, "3", "10", "test: change an exported constant")
		snapshots := countRows(database, &storage.ModuleSnapshot{}, "1 = 1")
		_, _, err := checkTypechecks(ctx, engine, app)
		Expect(err).To(MatchError(ContainSubstring("local dependency context changed")))
		Expect(runTypechecks(ctx, engine, lib)).To(Equal(int64(1)))
		snapshots++
		_, _, err = checkTypechecks(ctx, engine, app)
		Expect(err).To(MatchError(ContainSubstring("local dependency context changed")))
		Expect(countRows(database, &storage.ModuleSnapshot{}, "1 = 1")).To(Equal(snapshots))
	})

	It("type-checks a head that recorded no sibling shapes and confirms it by its context hash", func(ctx SpecContext) {
		Expect(database.Where("1 = 1").Delete(&storage.SnapshotSiblingShape{}).Error).To(Succeed())
		current, calls, err := checkTypechecks(ctx, engine, app)
		Expect(err).ToNot(HaveOccurred())
		Expect(current.SnapshotID).To(Equal(appHead))
		Expect(calls).To(Equal(int64(1)))
	})
})
