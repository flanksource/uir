package storage_test

import (
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// wideFuncs is more than one IN batch of symbols in one package, so a lookup of all of them takes two
// queries whose results must join in handle order.
const wideFuncs = 300

var _ = Describe("defined symbols", func() {
	DescribeTable("restrict a snapshot's defined-symbol set to the handles asked about",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDB(ctx, options())
			fixture := newModuleFixture(database, "/workspace/service")
			rows, err := publishSymbols(ctx, database, fmtPackage, storeType, saveMethod, loadFunc, runFunc)
			Expect(err).ToNot(HaveOccurred())
			fmtHandle, store, save, load, run := rows[0].Handle, rows[1].Handle, rows[2].Handle, rows[3].Handle, rows[4].Handle
			wide := make([]storage.Symbol, wideFuncs)
			for i := range wide {
				wide[i] = handleSymbol(serviceModule, serviceModule+"/wide", "func", fmt.Sprintf("f%03d", i), "internal")
			}
			wide, err = publishSymbols(ctx, database, wide...)
			Expect(err).ToNot(HaveOccurred())

			initial := publishedSnapshot(fixture.location, nil, "initial", 0, fixture.now)
			edited := publishedSnapshot(fixture.location, &initial.ID, "edited", 0, fixture.now)
			createAll(database, &initial, &edited)
			deltas := []storage.SymbolDelta{setSymbol(initial, store, 1, 1), setSymbol(initial, save, 2, 2), setSymbol(initial, load, 3, 3),
				deleteSymbol(edited, load), setSymbol(edited, run, 4, 4)}
			var wideHandles, evenHandles []int64
			for i, row := range wide {
				wideHandles = append(wideHandles, row.Handle)
				deltas = append(deltas, setSymbol(initial, row.Handle, int64(i), int64(i)))
				if i%2 == 1 {
					deltas = append(deltas, deleteSymbol(edited, row.Handle))
				} else {
					evenHandles = append(evenHandles, row.Handle)
				}
			}
			Expect(database.CreateInBatches(deltas, 100).Error).To(Succeed())

			asked := []int64{run, fmtHandle, load, store, save, store}
			for snapshot, want := range map[uuid.UUID][]int64{initial.ID: {store, save, load}, edited.ID: {store, save, run}} {
				defined, err := storage.DefinedSymbols(ctx, database, snapshot, asked)
				Expect(err).ToNot(HaveOccurred())
				Expect(defined).To(Equal(sortedHandles(want)), "an IN lookup of a few handles in snapshot %s", snapshot)
			}
			defined, err := storage.DefinedSymbols(ctx, database, edited.ID, append(slices.Clone(wideHandles), store))
			Expect(err).ToNot(HaveOccurred())
			Expect(defined).To(Equal(sortedHandles(append(slices.Clone(evenHandles), store))), "a lookup in two IN batches")
			withoutTen := slices.Delete(slices.Clone(wideHandles), 10, 11)
			defined, err = storage.DefinedSymbols(ctx, database, edited.ID, withoutTen)
			Expect(err).ToNot(HaveOccurred())
			Expect(defined).To(Equal(slices.Delete(slices.Clone(evenHandles), 5, 6)), "f010 is defined but was not asked about")

			defined, err = storage.DefinedSymbols(ctx, database, edited.ID, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(defined).To(BeEmpty())
			missing := uuid.New()
			_, err = storage.DefinedSymbols(ctx, database, missing, []int64{store})
			Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("snapshot %s does not exist", missing))))
			Expect(database.Model(&storage.ModuleSnapshot{}).Where("id = ?", initial.ID).Update("base_snapshot_id", edited.ID).Error).To(Succeed())
			_, err = storage.DefinedSymbols(ctx, database, edited.ID, []int64{store})
			Expect(err).To(MatchError(ContainSubstring("exceeds 100000 base links")))
		},
		Entry("SQLite", sqliteOptions("defined-symbols.db")),
		Entry("PostgreSQL", postgresOptions("uir_defined_symbols")),
	)
})

func sortedHandles(handles []int64) []int64 {
	sorted := slices.Clone(handles)
	slices.Sort(sorted)
	return sorted
}
