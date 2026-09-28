package storage_test

import (
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// lockWaiters counts the sessions of this database blocked on a lock.
func lockWaiters(database *gorm.DB) int64 {
	var waiting int64
	Expect(database.Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'").
		Scan(&waiting).Error).To(Succeed())
	return waiting
}

var _ = Describe("allocation conflicts on PostgreSQL", func() {
	It("surfaces a lost registry and handle race as ErrAllocationConflict and wins on retry", func(ctx SpecContext) {
		database := openDB(ctx, postgresOptions("uir_allocation_race")())
		held := []storage.Symbol{handleSymbol(serviceModule, serviceModule+"/a", "func", "First", "exported")}
		holder := database.WithContext(ctx).Begin()
		Expect(holder.Error).ToNot(HaveOccurred())
		committed := false
		DeferCleanup(func() {
			if !committed {
				Expect(holder.Rollback().Error).To(Succeed())
			}
		})
		Expect(storage.AssignSymbolHandles(ctx, holder, held)).To(Succeed())
		Expect(storage.UpsertSymbols(ctx, holder, held, 8)).To(Succeed())

		var attempts []error
		var racer []storage.Symbol
		done := make(chan error, 1)
		go func() {
			defer GinkgoRecover()
			done <- storage.RetryAllocationConflicts(ctx, database, func(tx *gorm.DB) error {
				racer = []storage.Symbol{handleSymbol(serviceModule, serviceModule+"/a", "func", "Second", "exported")}
				err := storage.AssignSymbolHandles(ctx, tx, racer)
				if err == nil {
					err = storage.UpsertSymbols(ctx, tx, racer, 8)
				}
				attempts = append(attempts, err)
				return err
			})
		}()
		Eventually(func() int64 { return lockWaiters(database) }).WithContext(ctx).Should(BeNumerically(">=", 1),
			"the racer cannot see the uncommitted module number and blocks on its unique key")
		Expect(holder.Commit().Error).To(Succeed())
		committed = true

		Eventually(done).WithContext(ctx).Should(Receive(Succeed()))
		Expect(attempts).To(HaveLen(2))
		Expect(attempts[0]).To(MatchError(storage.ErrAllocationConflict))
		Expect(attempts[0]).To(MatchError(ContainSubstring("register 1 symbol modules")))
		Expect(attempts[1]).ToNot(HaveOccurred())
		Expect(racer[0].Handle).To(Equal(held[0].Handle+1), "the retry sees the committed registry and takes the next local")
	})

	It("does not retry an error that is not a lost race", func(ctx SpecContext) {
		database := openDB(ctx, postgresOptions("uir_allocation_other")())
		calls := 0
		err := storage.RetryAllocationConflicts(ctx, database, func(tx *gorm.DB) error {
			calls++
			return storage.AssignSymbolHandles(ctx, tx, []storage.Symbol{handleSymbol(serviceModule, serviceModule+"/a", "label", "loop", "internal")})
		})
		Expect(err).To(MatchError(ContainSubstring(`symbol kind "label"`)))
		Expect(err).ToNot(MatchError(storage.ErrAllocationConflict))
		Expect(calls).To(Equal(1))
	})
})
