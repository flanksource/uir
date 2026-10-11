package indexer

import (
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("historical snapshot retention", func() {
	It("protects historical imports during automatic pruning and permits explicit pruning", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		publish(ctx, database, prodPublication("initial", "100"))
		old := historicalFixture(ctx, database, prodPublication("old", "50"))
		publish(ctx, database, prodPublication("current", "200"))
		pruned, err := storage.PruneHistory(ctx, database, prodRoot, storage.HistoryPruneOptions{Keep: 1, PreserveHistorical: true})
		Expect(err).ToNot(HaveOccurred())
		Expect(pruned.Kept).To(Equal(2))
		var retained storage.ModuleSnapshot
		Expect(database.Where("id = ?", old.SnapshotID).First(&retained).Error).To(Succeed())
		pruned, err = storage.PruneHistory(ctx, database, prodRoot, storage.HistoryPruneOptions{Keep: 1})
		Expect(err).ToNot(HaveOccurred())
		Expect(pruned.Kept).To(Equal(1))
		Expect(rowCounts(database, "snapshots")).To(Equal([]int64{1}))
	})
})
