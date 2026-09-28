package storage_test

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func setSymbol(snapshot storage.ModuleSnapshot, handle, shape, body int64) storage.SymbolDelta {
	return storage.SymbolDelta{SnapshotOrdinal: snapshot.Ordinal, RootOrdinal: fixtureRootOrdinal, SymbolHandle: handle, Operation: storage.SourceSet, ShapeFP: &shape, BodyFP: &body}
}

func deleteSymbol(snapshot storage.ModuleSnapshot, handle int64) storage.SymbolDelta {
	return storage.SymbolDelta{SnapshotOrdinal: snapshot.Ordinal, RootOrdinal: fixtureRootOrdinal, SymbolHandle: handle, Operation: storage.SourceDelete}
}

func effective(handle, shape, body int64) storage.EffectiveSymbol {
	return storage.EffectiveSymbol{Handle: handle, ShapeFP: &shape, BodyFP: &body}
}

var _ = Describe("effective symbols", func() {
	DescribeTable("take the newest delta per handle along the base chain and drop tombstones",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			fixture := newModuleFixture(database, "/workspace/service")
			rows, err := publishSymbols(ctx, database, storeType, saveMethod, loadFunc, runFunc)
			Expect(err).ToNot(HaveOccurred())
			store, save, load, run := rows[0].Handle, rows[1].Handle, rows[2].Handle, rows[3].Handle
			initial := publishedSnapshot(fixture.location, nil, "initial", 0, fixture.now)
			edited := publishedSnapshot(fixture.location, &initial.ID, "edited", 0, fixture.now)
			added := publishedSnapshot(fixture.location, &edited.ID, "added", 0, fixture.now)
			readded := publishedSnapshot(fixture.location, &added.ID, "readded", 0, fixture.now)
			createAll(database, &initial, &edited, &added, &readded)
			for _, delta := range []storage.SymbolDelta{
				setSymbol(initial, store, 1, 1), setSymbol(initial, save, 2, 2), setSymbol(initial, load, 3, 3),
				setSymbol(edited, save, 2, 5), deleteSymbol(edited, load),
				setSymbol(added, run, 4, 4),
				deleteSymbol(readded, store), setSymbol(readded, load, 3, 9),
			} {
				createAll(database, &delta)
			}

			for snapshot, want := range map[string][]storage.EffectiveSymbol{
				"initial": {effective(store, 1, 1), effective(save, 2, 2), effective(load, 3, 3)},
				"edited":  {effective(store, 1, 1), effective(save, 2, 5)},
				"added":   {effective(store, 1, 1), effective(save, 2, 5), effective(run, 4, 4)},
				"readded": {effective(save, 2, 5), effective(load, 3, 9), effective(run, 4, 4)},
			} {
				id := map[string]uuid.UUID{"initial": initial.ID, "edited": edited.ID, "added": added.ID, "readded": readded.ID}[snapshot]
				symbols, err := storage.EffectiveSymbols(ctx, database, id)
				Expect(err).ToNot(HaveOccurred())
				Expect(symbols).To(Equal(sortedEffective(want)), snapshot)
			}

			empty := publishedSnapshot(fixture.location, nil, "empty", 0, fixture.now)
			createAll(database, &empty)
			symbols, err := storage.EffectiveSymbols(ctx, database, empty.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(symbols).To(BeEmpty())
			missing := uuid.New()
			_, err = storage.EffectiveSymbols(ctx, database, missing)
			Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("snapshot %s does not exist", missing))))
			Expect(database.Model(&storage.ModuleSnapshot{}).Where("id = ?", initial.ID).Update("base_snapshot_id", readded.ID).Error).To(Succeed())
			_, err = storage.EffectiveSymbols(ctx, database, readded.ID)
			Expect(err).To(MatchError(ContainSubstring("exceeds 100000 base links")))
		},
		Entry("SQLite", sqliteOptions("effective-symbols.db")),
		Entry("PostgreSQL", postgresOptions("uir_effective_symbols")),
	)

	DescribeTable("reject a delta row whose fingerprints disagree with its operation or whose ordinals are unknown",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			fixture := newModuleFixture(database, "/workspace/service")
			rows, err := publishSymbols(ctx, database, storeType)
			Expect(err).ToNot(HaveOccurred())
			snapshot := publishedSnapshot(fixture.location, nil, "initial", 0, fixture.now)
			createAll(database, &snapshot)
			shapeOnly := storage.SymbolDelta{SnapshotOrdinal: snapshot.Ordinal, RootOrdinal: fixtureRootOrdinal, SymbolHandle: rows[0].Handle, Operation: storage.SourceSet, ShapeFP: new(int64)}
			Expect(database.Create(&shapeOnly).Error).To(HaveOccurred(), "a set carries both fingerprints")
			tombstone := deleteSymbol(snapshot, rows[0].Handle)
			tombstone.BodyFP = new(int64)
			Expect(database.Create(&tombstone).Error).To(HaveOccurred(), "a delete carries neither")
			unknownSnapshot := deleteSymbol(snapshot, rows[0].Handle)
			unknownSnapshot.SnapshotOrdinal = snapshot.Ordinal + 1
			Expect(database.Create(&unknownSnapshot).Error).To(HaveOccurred(), "a delta references an existing snapshot ordinal")
			unknownRoot := deleteSymbol(snapshot, rows[0].Handle)
			unknownRoot.RootOrdinal = fixtureRootOrdinal + 1
			Expect(database.Create(&unknownRoot).Error).To(HaveOccurred(), "a delta references an existing root ordinal")

			createAll(database, new(deleteSymbol(snapshot, rows[0].Handle)))
			Expect(database.Delete(&storage.ModuleSnapshot{}, "id = ?", snapshot.ID).Error).To(Succeed())
			var remaining int64
			Expect(database.Model(&storage.SymbolDelta{}).Count(&remaining).Error).To(Succeed())
			Expect(remaining).To(BeZero(), "deltas cascade with their snapshot")
		},
		Entry("SQLite", sqliteOptions("delta-checks.db")),
		Entry("PostgreSQL", postgresOptions("uir_delta_checks")),
	)

	DescribeTable("allocate dense snapshot ordinals from one past the largest",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			fixture := newModuleFixture(database, "/workspace/service")
			first := publishedSnapshot(fixture.location, nil, "first", 0, fixture.now)
			second := publishedSnapshot(fixture.location, &first.ID, "second", 0, fixture.now)
			first.Ordinal, second.Ordinal = 0, 0
			Expect(storage.CreateSnapshot(ctx, database, &first)).To(Succeed())
			Expect(storage.CreateSnapshot(ctx, database, &second)).To(Succeed())
			Expect([]int64{first.Ordinal, second.Ordinal}).To(Equal([]int64{1, 2}))

			preset := publishedSnapshot(fixture.location, &second.ID, "preset", 0, fixture.now)
			Expect(storage.CreateSnapshot(ctx, database, &preset)).To(MatchError(ContainSubstring("assigns the snapshot ordinal itself")))
		},
		Entry("SQLite", sqliteOptions("snapshot-ordinals.db")),
		Entry("PostgreSQL", postgresOptions("uir_snapshot_ordinals")),
	)
})

func sortedEffective(symbols []storage.EffectiveSymbol) []storage.EffectiveSymbol {
	sorted := slices.Clone(symbols)
	slices.SortFunc(sorted, func(left, right storage.EffectiveSymbol) int { return cmp.Compare(left.Handle, right.Handle) })
	return sorted
}
