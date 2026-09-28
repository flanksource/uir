package storage

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// ErrAllocationConflict marks a publication that lost a race for a surrogate number (a module or
// package registry number, a symbol handle, or a module or document ordinal) to a concurrent
// publisher. Every allocation reads the current maximum inside the publication transaction and relies
// on the unique keys to reject a duplicate, so the loser's transaction is rolled back whole and may be
// retried from the start; RetryAllocationConflicts does that.
var ErrAllocationConflict = errors.New("lost a surrogate allocation race to a concurrent publisher")

// AllocationAttempts bounds how often RetryAllocationConflicts runs a publication.
const AllocationAttempts = 3

// retryableStates are the PostgreSQL SQLSTATEs of losing an allocation race: a unique violation, a
// deadlock between two allocators, and a serialization failure. SQLite serializes writers and reports
// none of them.
var retryableStates = map[string]bool{"23505": true, "40P01": true, "40001": true}

// allocationConflict wraps err with subject, marking it ErrAllocationConflict when the database
// reports a lost race.
func allocationConflict(err error, subject string) error {
	var coded interface{ SQLState() string }
	if errors.As(err, &coded) && retryableStates[coded.SQLState()] {
		return fmt.Errorf("%s: %w: %w", subject, ErrAllocationConflict, err)
	}
	return fmt.Errorf("%s: %w", subject, err)
}

// RetryAllocationConflicts runs publish in a transaction, and again in a fresh one when it lost an
// allocation race, at most AllocationAttempts times. Any other error is returned at once.
func RetryAllocationConflicts(ctx context.Context, database *gorm.DB, publish func(*gorm.DB) error) error {
	var err error
	for attempt := 1; attempt <= AllocationAttempts; attempt++ {
		err = database.WithContext(ctx).Transaction(publish)
		if !errors.Is(err, ErrAllocationConflict) {
			return err
		}
	}
	return fmt.Errorf("publication lost %d consecutive allocation races: %w", AllocationAttempts, err)
}
