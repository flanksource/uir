package indexer

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func matchingHistoricalSnapshot(ctx context.Context, database *gorm.DB, location storage.ModuleLocation, extraction moduleExtraction) (storage.ModuleSnapshot, bool, error) {
	root := extraction.root
	var snapshot storage.ModuleSnapshot
	selection := database.WithContext(ctx).Where(
		"root_id = ? AND location_id = ? AND revision = ? AND git_commit = ? AND content_set_hash = ? AND configuration_hash = ? AND context_hash = ? AND coverage = ? AND dependency_set_hash = ? AND reason = ?",
		location.RootID, location.ID, root.Revision, root.GitCommit, root.ContentSetHash, root.ConfigurationHash,
		extraction.contextHash, extraction.coverage, dependencyHash(root.Dependencies), storage.ReasonHistorical,
	)
	if extraction.requestedBase != uuid.Nil {
		selection = selection.Where("base_snapshot_id = ?", extraction.requestedBase)
	}
	err := selection.Order("ordinal DESC").Take(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return snapshot, false, nil
	}
	if err != nil {
		return snapshot, false, fmt.Errorf("find historical publication of %q: %w", root.RootKey, err)
	}
	return snapshot, true, nil
}
