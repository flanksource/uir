package symboldiff

import (
	"context"
	"path/filepath"
	"time"

	clickytask "github.com/flanksource/clicky/task"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// twoCommits returns a repository whose head (to) is indexed and whose parent (from) is not.
func twoCommits(ctx context.Context, database *gorm.DB) (*repository, string, string, string) {
	GinkgoHelper()
	repo := newRepository()
	repo.write(map[string]*string{"go.mod": text("module " + shopRoot + "\n\ngo 1.26\n"), "cart.go": text(cartBefore)})
	from := repo.commit("Add cart")
	repo.write(map[string]*string{"cart.go": text(cartAfter)})
	to := repo.commit("Change cart")
	return repo, from, to, repo.index(ctx, database)
}

func snapshotOf(database *gorm.DB, id string) storage.ModuleSnapshot {
	GinkgoHelper()
	var snapshot storage.ModuleSnapshot
	Expect(database.Where("id = ?", uuid.MustParse(id)).Take(&snapshot).Error).To(Succeed())
	return snapshot
}

// indexRevision indexes one commit as a module-index run and returns its snapshot id.
func indexRevision(ctx context.Context, database *gorm.DB, repo *repository, commit string) string {
	GinkgoHelper()
	engine, err := indexer.New(database)
	Expect(err).ToNot(HaveOccurred())
	run, err := indexer.StartRevisionTask(ctx, engine, indexer.RevisionOptions{RootKey: shopRoot, Checkout: repo.path, Commit: commit, Reason: storage.ReasonHistorical})
	Expect(err).ToNot(HaveOccurred())
	result, err := run.Wait(ctx)
	Expect(err).ToNot(HaveOccurred())
	return result.SnapshotID
}

// shopIndexRuns is the commit label of every module-index run of shopRoot, by run id. Fixture
// commits have fixed dates, so other specs' runs share these labels and only a delta is meaningful.
func shopIndexRuns() map[string]string {
	runs := map[string]string{}
	for _, run := range clickytask.Runs(clickytask.RunFilter{Kind: indexer.ModuleIndexKind, Labels: map[string]string{"root": shopRoot}}) {
		runs[run.ID] = run.Labels["commit"]
	}
	return runs
}

// startedRuns is the commit label of every run in after that is not in before.
func startedRuns(before, after map[string]string) []string {
	var commits []string
	for id, commit := range after {
		if _, existed := before[id]; !existed {
			commits = append(commits, commit)
		}
	}
	return commits
}

var _ = Describe("auto-indexed diffs", func() {
	It("starts no module-index run for commits that already have reusable snapshots", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		repo, from, to, _ := twoCommits(ctx, database)
		indexed := []string{indexRevision(ctx, database, repo, from), indexRevision(ctx, database, repo, to)}
		before := shopIndexRuns()

		result, err := Diff(ctx, database, Options{RootKey: shopRoot, From: from, To: to, Visibility: VisibilityAll, AutoIndex: true, TaskContext: ctx})

		Expect(err).ToNot(HaveOccurred())
		Expect([]string{result.From.SnapshotID, result.To.SnapshotID}).To(Equal(indexed))
		Expect(startedRuns(before, shopIndexRuns())).To(BeEmpty())
	})

	It("starts exactly one module-index run, for the side without a reusable snapshot", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		repo, from, to, _ := twoCommits(ctx, database)
		indexed := indexRevision(ctx, database, repo, to)
		before := shopIndexRuns()

		result, err := Diff(ctx, database, Options{RootKey: shopRoot, From: from, To: to, Visibility: VisibilityAll, AutoIndex: true, TaskContext: ctx})

		Expect(err).ToNot(HaveOccurred())
		Expect(result.To.SnapshotID).To(Equal(indexed))
		Expect(startedRuns(before, shopIndexRuns())).To(Equal([]string{from}))
	})

	It("publishes one snapshot of a commit another run is indexing, recorded under that run", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		repo, from, to, _ := twoCommits(ctx, database)
		engine, err := indexer.New(database)
		Expect(err).ToNot(HaveOccurred())
		running, err := indexer.StartRevisionTask(ctx, engine, indexer.RevisionOptions{
			RootKey: shopRoot, Checkout: repo.path, Commit: from, Reason: storage.ReasonHistorical,
		})
		Expect(err).ToNot(HaveOccurred())

		result, err := Diff(ctx, database, Options{RootKey: shopRoot, From: from, To: to, Visibility: VisibilityAll, AutoIndex: true, TaskContext: ctx})

		Expect(err).ToNot(HaveOccurred())
		indexed := snapshotOf(database, result.From.SnapshotID)
		Expect(indexed.TaskRunID).To(HaveValue(Equal(running.ID())), "the diff waited on the run that was already indexing its commit")
		Expect(indexed.Reason).To(Equal(storage.ReasonHistorical))
		var commitSnapshots int64
		Expect(database.Model(&storage.ModuleSnapshot{}).Where("git_commit = ?", from).Count(&commitSnapshots).Error).To(Succeed())
		Expect(commitSnapshots).To(Equal(int64(1)))
	})

	It("stops waiting when the request ends, while the index it started keeps running", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		_, from, to, _ := twoCommits(ctx, database)
		request, cancel := context.WithCancel(ctx)
		go func() {
			defer GinkgoRecover()
			defer cancel()
			running := clickytask.RunFilter{Kind: indexer.ModuleIndexKind, Status: string(clickytask.StatusRunning), Labels: map[string]string{"commit": from}}
			Eventually(func() []clickytask.RunMeta { return clickytask.Runs(running) }, 30*time.Second, time.Millisecond).ShouldNot(BeEmpty())
		}()

		_, err := Diff(request, database, Options{RootKey: shopRoot, From: from, To: to, Visibility: VisibilityAll, AutoIndex: true, TaskContext: ctx})

		Expect(err).To(MatchError(context.Canceled))
		Eventually(func() int64 {
			var count int64
			Expect(database.Model(&storage.ModuleSnapshot{}).Where("git_commit = ? AND reason = ?", from, storage.ReasonHistorical).Count(&count).Error).To(Succeed())
			return count
		}, 30*time.Second).Should(Equal(int64(1)), "the run outlives the request that started it")
	})

	It("refuses to auto-index without a context for the run", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		_, from, to, _ := twoCommits(ctx, database)

		_, err := Diff(ctx, database, Options{RootKey: shopRoot, From: from, To: to, Visibility: VisibilityAll, AutoIndex: true})

		Expect(err).To(MatchError(ContainSubstring("auto-index needs a task context")))
	})

	It("indexes from the checkout the diff names", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		repo, from, to, _ := twoCommits(ctx, database)
		clone, err := filepath.EvalSymlinks(GinkgoT().TempDir())
		Expect(err).ToNot(HaveOccurred())
		repo.git("clone", "--quiet", repo.path, clone)
		(&repository{path: clone}).index(ctx, database)
		var location storage.ModuleLocation
		Expect(database.Where("canonical_path = ?", clone).Take(&location).Error).To(Succeed())

		result, err := Diff(ctx, database, Options{RootKey: shopRoot, From: from, To: to, Visibility: VisibilityAll, AutoIndex: true, TaskContext: ctx, Location: clone})
		Expect(err).ToNot(HaveOccurred())
		_, err = Diff(ctx, database, Options{RootKey: shopRoot, From: from, To: to, Visibility: VisibilityAll, Location: filepath.Join(clone, "missing")})

		Expect(snapshotOf(database, result.From.SnapshotID).LocationID).To(Equal(location.ID))
		Expect(err).To(MatchError(ContainSubstring("is not registered for root")))
	})

	It("marks the commits that have a snapshot in the listed checkout", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		repo, from, to, head := twoCommits(ctx, database)

		history, err := ListHistory(ctx, database, shopRoot, repo.path, 20)

		Expect(err).ToNot(HaveOccurred())
		Expect(history.Commits).To(HaveLen(2))
		Expect([]string{history.Commits[0].Commit, history.Commits[0].SnapshotID, history.Commits[0].SnapshotReason}).To(Equal([]string{to, head, "add"}))
		Expect(history.Commits[0].SnapshotCompletedAt).ToNot(BeNil())
		Expect([]any{history.Commits[1].Commit, history.Commits[1].SnapshotID, history.Commits[1].SnapshotCompletedAt}).To(Equal([]any{from, "", (*time.Time)(nil)}))
	})
})
