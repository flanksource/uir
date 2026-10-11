package indexer

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

func matchingHistoricalSnapshot(ctx context.Context, database *gorm.DB, location storage.ModuleLocation, extraction moduleExtraction) (storage.ModuleSnapshot, bool, error) {
	root := extraction.root
	var snapshot storage.ModuleSnapshot
	err := database.WithContext(ctx).Where(
		"root_id = ? AND location_id = ? AND revision = ? AND git_commit = ? AND content_set_hash = ? AND configuration_hash = ? AND context_hash = ? AND coverage = ? AND dependency_set_hash = ? AND reason = ?",
		location.RootID, location.ID, root.Revision, root.GitCommit, root.ContentSetHash, root.ConfigurationHash,
		extraction.contextHash, extraction.coverage, dependencyHash(root.Dependencies), storage.ReasonHistorical,
	).Order("ordinal DESC").Take(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return snapshot, false, nil
	}
	if err != nil {
		return snapshot, false, fmt.Errorf("find historical publication of %q: %w", root.RootKey, err)
	}
	return snapshot, true, nil
}
