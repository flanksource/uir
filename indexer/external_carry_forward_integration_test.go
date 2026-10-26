package indexer

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const (
	effectiveDocumentsFragment = "d.document_ordinal, d.operation"
	effectiveSourcesFragment   = "d.revision_id, d.operation"
)

// statementCounter counts the statements whose SQL contains a fragment, for every fragment it watches.
type statementCounter map[string]*atomic.Int64

func countStatements(database *gorm.DB, fragments ...string) statementCounter {
	GinkgoHelper()
	counter := statementCounter{}
	for _, fragment := range fragments {
		counter[fragment] = &atomic.Int64{}
	}
	count := func(statement *gorm.DB) {
		for fragment, seen := range counter {
			if strings.Contains(statement.Statement.SQL.String(), fragment) {
				seen.Add(1)
			}
		}
	}
	name := "test:count:" + uuid.NewString()
	Expect(database.Callback().Row().After("gorm:row").Register(name, count)).To(Succeed())
	Expect(database.Callback().Query().After("gorm:query").Register(name, count)).To(Succeed())
	return counter
}

func (counter statementCounter) counts() map[string]int64 {
	counts := map[string]int64{}
	for fragment, seen := range counter {
		counts[fragment] = seen.Load()
	}
	return counts
}

func historicalProd(revision, calc, commit, base string) Publication {
	publication := prodPublication(revision, calc)
	publication.Options = PublishOptions{Historical: true, GitCommit: strings.Repeat(commit, 40), Producer: "catalog-v1", BaseSnapshotID: base}
	return publication
}

func emptyDelta(revision, commit, base string) Publication {
	publication := historicalProd(revision, "100", commit, base)
	publication.Documents, publication.Delta = nil, &PublicationDelta{}
	return publication
}

// publishedState is everything a reader sees of one snapshot, without the fields that identify the
// publication itself: its id, ordinal, revision, commit, and times.
type publishedState struct {
	Snapshot       storage.ModuleSnapshot
	Sources        map[string]storage.SourceRevision
	Active         map[string]storage.ActiveDocument
	Coverage       []storage.PackageCoverage
	Rows           map[string]int64
	ImplementPairs []int64
	Writers        []query.ModuleMatch
}

func readPublished(ctx context.Context, database *gorm.DB, snapshotID string) publishedState {
	GinkgoHelper()
	id := uuid.MustParse(snapshotID)
	var state publishedState
	Expect(database.Where("id = ?", id).Take(&state.Snapshot).Error).To(Succeed())
	ordinal := state.Snapshot.Ordinal
	var err error
	state.Sources, err = storage.EffectiveSources(ctx, database, storage.EffectiveSourcesOptions{SnapshotID: id})
	Expect(err).NotTo(HaveOccurred())
	state.Active, err = storage.ActiveDocuments(ctx, database, id, storage.ActiveDocumentOptions{Content: true})
	Expect(err).NotTo(HaveOccurred())
	Expect(database.Where("snapshot_id = ?", id).Order("package_path").Find(&state.Coverage).Error).To(Succeed())
	for i := range state.Coverage {
		state.Coverage[i].SnapshotID = uuid.Nil
	}
	state.Rows = map[string]int64{}
	for table, column := range map[string]string{"source_deltas": "snapshot_id", "document_deltas": "snapshot_ordinal", "symbol_deltas": "snapshot_ordinal", "implements_deltas": "snapshot_ordinal"} {
		var count int64
		key := any(ordinal)
		if column == "snapshot_id" {
			key = id
		}
		Expect(database.Table(table).Where(column+" = ?", key).Count(&count).Error).To(Succeed(), table)
		state.Rows[table] = count
	}
	Expect(database.Table("implements_facts").Where("snapshot_ordinal = ?", ordinal).Pluck("pair_count", &state.ImplementPairs).Error).To(Succeed())
	pipeline, err := query.NewPipeline(database)
	Expect(err).NotTo(HaveOccurred())
	writers, err := pipeline.RunModules(ctx, "field:Amount <", query.ModuleScopeOptions{SnapshotID: snapshotID})
	Expect(err).NotTo(HaveOccurred())
	state.Writers = writers.Matches
	for i := range state.Writers {
		Expect(state.Writers[i].SnapshotID).To(Equal(snapshotID))
		state.Writers[i].SnapshotID = ""
	}
	state.Snapshot.ID, state.Snapshot.Ordinal, state.Snapshot.Revision, state.Snapshot.GitCommit = uuid.Nil, 0, "", ""
	state.Snapshot.StartedAt, state.Snapshot.CompletedAt, state.Snapshot.IndexStartedAt, state.Snapshot.LastModifiedAt = time.Time{}, time.Time{}, nil, nil
	return state
}

func expectSamePublished(actual, expected publishedState) {
	GinkgoHelper()
	Expect(actual.Snapshot).To(Equal(expected.Snapshot), "snapshot row")
	Expect(actual.Sources).To(Equal(expected.Sources), "effective sources")
	Expect(actual.Active).To(Equal(expected.Active), "active documents")
	Expect(actual.Coverage).To(Equal(expected.Coverage), "package coverage")
	Expect(actual.Rows).To(Equal(expected.Rows), "delta rows")
	Expect(actual.ImplementPairs).To(Equal(expected.ImplementPairs), "implements facts")
	Expect(actual.Writers).To(Equal(expected.Writers), "query matches")
}

var _ = Describe("delta publication cost", func() {
	It("reads the base's active documents once for a delta that replaces a package", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		base := publish(ctx, database, historicalProd("first", "100", "a", ""))
		counter := countStatements(database, effectiveDocumentsFragment)
		next := historicalProd("next", "200", "b", base.SnapshotID)
		next.Documents, next.Delta = next.Documents[1:], &PublicationDelta{}
		publish(ctx, database, next)
		Expect(counter.counts()).To(Equal(map[string]int64{effectiveDocumentsFragment: 1}))
	})

	It("carries an empty delta forward without resolving the base's sources or documents", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		base := publish(ctx, database, historicalProd("first", "100", "a", ""))
		counter := countStatements(database, effectiveDocumentsFragment, effectiveSourcesFragment)
		carried := publish(ctx, database, emptyDelta("second", "b", base.SnapshotID))
		Expect(counter.counts()).To(Equal(map[string]int64{effectiveDocumentsFragment: 0, effectiveSourcesFragment: 0}))
		Expect(carried.Unchanged).To(BeFalse())
		Expect(carried.SnapshotID).NotTo(Equal(base.SnapshotID))
	})
})

var _ = Describe("empty delta carry-forward", func() {
	DescribeTable("publishes a snapshot readers cannot tell from a full re-publish of the base's documents", func(ctx SpecContext, open func(context.Context) *gorm.DB) {
		database := open(ctx)
		base := publish(ctx, database, historicalProd("first", "100", "a", ""))
		carried := publish(ctx, database, emptyDelta("second", "b", base.SnapshotID))
		full := publish(ctx, database, historicalProd("third", "100", "c", base.SnapshotID))
		Expect(carried).To(Equal(ModuleResult{RootKey: full.RootKey, Location: full.Location, SnapshotID: carried.SnapshotID,
			Files: full.Files, ParsedFiles: full.ParsedFiles, ReusedFiles: full.ReusedFiles, Warnings: full.Warnings}))
		expectSamePublished(readPublished(ctx, database, carried.SnapshotID), readPublished(ctx, database, full.SnapshotID))

		again := publish(ctx, database, emptyDelta("second", "b", base.SnapshotID))
		Expect([]any{again.SnapshotID, again.Unchanged, again.Files}).To(Equal([]any{carried.SnapshotID, true, full.Files}))

		onCarried := historicalProd("fourth", "300", "d", carried.SnapshotID)
		onCarried.Documents, onCarried.Delta = onCarried.Documents[1:], &PublicationDelta{}
		onFull := historicalProd("fifth", "300", "e", full.SnapshotID)
		onFull.Documents, onFull.Delta = onFull.Documents[1:], &PublicationDelta{}
		afterCarried, afterFull := readPublished(ctx, database, publish(ctx, database, onCarried).SnapshotID), readPublished(ctx, database, publish(ctx, database, onFull).SnapshotID)
		Expect([]*uuid.UUID{afterCarried.Snapshot.BaseSnapshotID, afterFull.Snapshot.BaseSnapshotID}).To(Equal([]*uuid.UUID{
			new(uuid.MustParse(carried.SnapshotID)), new(uuid.MustParse(full.SnapshotID))}))
		afterCarried.Snapshot.BaseSnapshotID, afterFull.Snapshot.BaseSnapshotID = nil, nil
		expectSamePublished(afterCarried, afterFull)
	},
		Entry("SQLite", openIndexerSQLite),
		Entry("PostgreSQL", openIndexerPostgres),
	)

	It("keeps serving the base's documents once pruning deletes the base", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		base := publish(ctx, database, historicalProd("first", "100", "a", ""))
		carried := publish(ctx, database, emptyDelta("second", "b", base.SnapshotID))
		before := readPublished(ctx, database, carried.SnapshotID)
		pruned, err := storage.PruneHistory(ctx, database, prodRoot, storage.HistoryPruneOptions{Keep: 1})
		Expect(err).NotTo(HaveOccurred())
		Expect(pruned.Kept).To(Equal(1))
		after := readPublished(ctx, database, carried.SnapshotID)
		Expect(after.Snapshot.BaseSnapshotID).To(BeNil())
		Expect([]any{after.Sources, after.Active, after.Coverage, after.Writers}).To(Equal([]any{before.Sources, before.Active, before.Coverage, before.Writers}))
	})
})
