package storage_test

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	chainLinks = 1000
	chainPaths = 50
	insertRows = 100
)

type statementCounter struct{ statements []string }

func (counter *statementCounter) LogMode(logger.LogLevel) logger.Interface { return counter }
func (counter *statementCounter) Info(context.Context, string, ...any)     {}
func (counter *statementCounter) Warn(context.Context, string, ...any)     {}
func (counter *statementCounter) Error(context.Context, string, ...any)    {}
func (counter *statementCounter) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	statement, _ := sql()
	counter.statements = append(counter.statements, statement)
}

// countedEffectiveSources resolves a snapshot and returns the SQL statements the resolution issued.
func countedEffectiveSources(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID) (map[string]storage.SourceRevision, []string) {
	GinkgoHelper()
	counter := &statementCounter{}
	sources, err := storage.EffectiveSources(ctx, database.Session(&gorm.Session{Logger: counter}), snapshotID)
	Expect(err).ToNot(HaveOccurred())
	return sources, counter.statements
}

func chainPath(index int) string { return fmt.Sprintf("file-%02d.go", index%chainPaths) }

func sqliteOptions(file string) func() storage.DBOptions {
	return func() storage.DBOptions {
		return storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), file)}
	}
}

func postgresOptions(name string) func() storage.DBOptions {
	return func() storage.DBOptions {
		return storage.DBOptions{DSN: dbtest.ForGinkgo(dbtest.Options{Name: name}).DSN(), Schema: name}
	}
}

type moduleFixture struct {
	root     storage.ModuleRoot
	location storage.ModuleLocation
	now      time.Time
}

func newModuleFixture(database *gorm.DB, canonicalPath string) moduleFixture {
	GinkgoHelper()
	now := time.Now().UTC()
	root := storage.ModuleRoot{ID: uuid.New(), RootKey: "example.org/service", Name: "service", CreatedAt: now}
	location := storage.ModuleLocation{ID: uuid.New(), RootID: root.ID, CanonicalPath: canonicalPath, Kind: "git", CreatedAt: now}
	createAll(database, &root, &location)
	return moduleFixture{root: root, location: location, now: now}
}

func setDelta(snapshot storage.ModuleSnapshot, revision *storage.SourceRevision) storage.SourceDelta {
	return storage.SourceDelta{SnapshotID: snapshot.ID, RootID: snapshot.RootID, PathKey: revision.PathKey, RevisionID: &revision.ID, Operation: storage.SourceSet}
}

func deleteDelta(snapshot storage.ModuleSnapshot, path string) storage.SourceDelta {
	return storage.SourceDelta{SnapshotID: snapshot.ID, RootID: snapshot.RootID, PathKey: path, Operation: storage.SourceDelete}
}

// linkedChain publishes chainLinks snapshots where link i sets chainPath(i), link 0 also sets early.go,
// link 5 deletes file-10.go (set again from link 10), and link 990 deletes file-00.go (last set at 950).
func linkedChain(database *gorm.DB, fixture moduleFixture) ([]storage.ModuleSnapshot, []storage.SourceRevision, storage.SourceRevision) {
	GinkgoHelper()
	snapshots := make([]storage.ModuleSnapshot, chainLinks)
	revisions := make([]storage.SourceRevision, chainLinks)
	deltas := make([]storage.SourceDelta, 0, chainLinks+3)
	var base *uuid.UUID
	for link := range snapshots {
		snapshots[link] = publishedSnapshot(fixture.location, base, fmt.Sprintf("link-%d", link), 0, fixture.now)
		base = &snapshots[link].ID
		revisions[link] = sourceRevision(fixture.root, chainPath(link), fmt.Sprintf("content of link %d", link))
		deltas = append(deltas, setDelta(snapshots[link], &revisions[link]))
	}
	early := sourceRevision(fixture.root, "early.go", "early")
	deltas = append(deltas, setDelta(snapshots[0], &early), deleteDelta(snapshots[5], chainPath(10)), deleteDelta(snapshots[990], chainPath(0)))
	Expect(database.CreateInBatches(snapshots, insertRows).Error).To(Succeed())
	Expect(database.CreateInBatches(append(revisions, early), insertRows).Error).To(Succeed())
	Expect(database.CreateInBatches(deltas, insertRows).Error).To(Succeed())
	return snapshots, revisions, early
}

var _ = Describe("effective sources", func() {
	DescribeTable("resolves a 1,000-link base chain with one chain query",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			snapshots, revisions, early := linkedChain(database, newModuleFixture(database, "/workspace/service"))

			head, headStatements := countedEffectiveSources(ctx, database, snapshots[chainLinks-1].ID)
			want := map[string]storage.SourceRevision{"early.go": early}
			for path := 1; path < chainPaths; path++ {
				want[chainPath(path)] = revisions[chainLinks-chainPaths+path]
			}
			Expect(head).To(Equal(want), "newest set per path wins, file-00.go is tombstoned at link 990, and the link-5 tombstone of file-10.go is overridden")

			shallow, shallowStatements := countedEffectiveSources(ctx, database, snapshots[9].ID)
			want = map[string]storage.SourceRevision{"early.go": early}
			for link := range 10 {
				want[chainPath(link)] = revisions[link]
			}
			Expect(shallow).To(Equal(want), "up to link 9, file-10.go only has the link-5 tombstone")
			Expect(headStatements).To(HaveLen(2), "one chain query plus one revision batch for %d paths: %v", len(head), headStatements)
			Expect(shallowStatements).To(HaveLen(len(headStatements)), "statement count must not grow with chain length")
		},
		Entry("SQLite", sqliteOptions("chain.db")),
		Entry("PostgreSQL", postgresOptions("uir_chain")),
	)

	DescribeTable("resolves a branch that bases on another location's head",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			fixture := newModuleFixture(database, "/workspace/service")
			worktree := storage.ModuleLocation{ID: uuid.New(), RootID: fixture.root.ID, CanonicalPath: "/workspace/service-feature", Kind: "git", CreatedAt: fixture.now}
			first := publishedSnapshot(fixture.location, nil, "main-1", 0, fixture.now)
			second := publishedSnapshot(fixture.location, &first.ID, "main-2", 0, fixture.now)
			alpha, beta := sourceRevision(fixture.root, "alpha.go", "alpha"), sourceRevision(fixture.root, "beta.go", "beta v1")
			betaV2, gamma := sourceRevision(fixture.root, "beta.go", "beta v2"), sourceRevision(fixture.root, "gamma.go", "gamma")
			createAll(database, &worktree, &first, &second, &alpha, &beta, &betaV2, &gamma,
				&storage.ModuleLocationHead{RootID: fixture.root.ID, LocationID: fixture.location.ID, SnapshotID: second.ID, Version: 2})
			var head storage.ModuleLocationHead
			Expect(database.Where("location_id = ?", fixture.location.ID).First(&head).Error).To(Succeed())
			branch := publishedSnapshot(worktree, &head.SnapshotID, "feature-1", 0, fixture.now)
			createAll(database, &branch)
			for _, delta := range []storage.SourceDelta{
				setDelta(first, &alpha), setDelta(first, &beta), setDelta(second, &betaV2), setDelta(branch, &gamma),
			} {
				createAll(database, &delta)
			}

			sources, err := storage.EffectiveSources(ctx, database, branch.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(sources).To(Equal(map[string]storage.SourceRevision{"alpha.go": alpha, "beta.go": betaV2, "gamma.go": gamma}))
			sources, err = storage.EffectiveSources(ctx, database, second.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(sources).To(Equal(map[string]storage.SourceRevision{"alpha.go": alpha, "beta.go": betaV2}), "the branch's files stay out of the head it bases on")
		},
		Entry("SQLite", sqliteOptions("branch.db")),
		Entry("PostgreSQL", postgresOptions("uir_branch")),
	)

	DescribeTable("fails loudly on a base cycle and on a missing snapshot",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			fixture := newModuleFixture(database, "/workspace/service")
			first := publishedSnapshot(fixture.location, nil, "first", 0, fixture.now)
			second := publishedSnapshot(fixture.location, &first.ID, "second", 0, fixture.now)
			alpha := sourceRevision(fixture.root, "alpha.go", "alpha")
			delta := setDelta(first, &alpha)
			createAll(database, &first, &second, &alpha, &delta)
			Expect(database.Model(&storage.ModuleSnapshot{}).Where("id = ?", first.ID).Update("base_snapshot_id", second.ID).Error).To(Succeed())

			_, err := storage.EffectiveSources(ctx, database, second.ID)
			Expect(err).To(MatchError(ContainSubstring("exceeds 100000 base links")))
			missing := uuid.New()
			_, err = storage.EffectiveSources(ctx, database, missing)
			Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("snapshot %s does not exist", missing))))
		},
		Entry("SQLite", sqliteOptions("cycle.db")),
		Entry("PostgreSQL", postgresOptions("uir_cycle")),
	)
})
