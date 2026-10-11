package indexer

import (
	"context"
	"time"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func historicalFixture(ctx context.Context, database *gorm.DB, publication Publication) ModuleResult {
	GinkgoHelper()
	kinds, err := storage.LoadSymbolKinds(ctx, database)
	Expect(err).ToNot(HaveOccurred())
	extraction, err := publication.validatedExtraction(kinds, time.Now().UTC())
	Expect(err).ToNot(HaveOccurred())
	var result ModuleResult
	Expect(database.Transaction(func(tx *gorm.DB) error {
		var publishErr error
		result, _, publishErr = indexModule(ctx, tx, extraction, nil, ModuleOptions{Reason: storage.ReasonHistorical})
		return publishErr
	})).To(Succeed())
	return result
}

var _ = Describe("historical external publication", func() {
	It("preserves the current head and reuses a matching historical snapshot", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		current := publish(ctx, database, prodPublication("current", "200"))
		old := historicalFixture(ctx, database, prodPublication("old", "100"))
		Expect(old.HeadVersion).To(Equal(current.HeadVersion))
		var head storage.ModuleLocationHead
		Expect(database.First(&head).Error).To(Succeed())
		Expect(head.SnapshotID.String()).To(Equal(current.SnapshotID))
		again := historicalFixture(ctx, database, prodPublication("old", "100"))
		Expect(again.SnapshotID).To(Equal(old.SnapshotID))
		Expect(again.Unchanged).To(BeTrue())
		Expect(rowCounts(database, "snapshots")).To(Equal([]int64{2}))
	})

	It("publishes and browses a historical-only location without a head", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		for _, kind := range externalKinds {
			_, err := storage.RegisterKind(ctx, database, kind.Name, kind.Category)
			Expect(err).ToNot(HaveOccurred())
		}
		result := historicalFixture(ctx, database, prodPublication("old", "100"))
		Expect(result.HeadVersion).To(BeZero())
		Expect(rowCounts(database, "location_heads")).To(Equal([]int64{0}))
		locations, err := storage.ModuleLocations(ctx, database, prodRoot)
		Expect(err).ToNot(HaveOccurred())
		Expect(locations).To(HaveLen(1))
		rows, total, err := storage.ModuleSnapshots(ctx, database, storage.ModuleSnapshotListOptions{RootKey: prodRoot, Location: prodURI})
		Expect(err).ToNot(HaveOccurred())
		Expect(total).To(Equal(int64(1)))
		Expect(rows[0].Kind).To(Equal(storage.SnapshotKindHistorical))
		Expect(rows[0].Head).To(BeFalse())
	})
})
