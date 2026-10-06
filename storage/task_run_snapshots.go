package storage

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TaskRunSnapshotIDs lists the snapshots published under a task run, in publication order.
func TaskRunSnapshotIDs(ctx context.Context, database *gorm.DB, runID string) ([]string, error) {
	var ids []uuid.UUID
	if err := database.WithContext(ctx).Model(&ModuleSnapshot{}).Where("task_run_id = ?", runID).Order("ordinal").Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("list snapshots of task run %s: %w", runID, err)
	}
	published := make([]string, 0, len(ids))
	for _, id := range ids {
		published = append(published, id.String())
	}
	return published, nil
}
