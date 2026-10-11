package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
)

func sampleSource(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("source sample requires a nonempty source set")
	}
	for _, path := range paths {
		if path != "generation" && path != "manifest.json" {
			return path, nil
		}
	}
	return paths[0], nil
}

func verifyRetainedSources(ctx context.Context, manifest runManifest) (int, error) {
	seen := map[string]bool{}
	var verified int
	for _, entry := range manifest.Cases {
		if seen[entry.Database] {
			continue
		}
		seen[entry.Database] = true
		count, err := verifyDatabaseSources(ctx, entry.Database)
		if err != nil {
			return verified, err
		}
		verified += count
	}
	return verified, nil
}

func verifyDatabaseSources(ctx context.Context, path string) (verified int, returnErr error) {
	database, err := storage.OpenReadOnly(ctx, storage.DBOptions{DSN: path})
	if err != nil {
		return 0, err
	}
	defer func() { returnErr = errors.Join(returnErr, closeDB(database)) }()
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return 0, err
	}
	var snapshots []storage.ModuleSnapshot
	if err := database.WithContext(ctx).Select("id").Find(&snapshots).Error; err != nil {
		return 0, err
	}
	if len(snapshots) == 0 {
		return 0, fmt.Errorf("%s has no retained snapshots", path)
	}
	for _, snapshot := range snapshots {
		if err := verifySource(ctx, database, pipeline, snapshot.ID.String()); err != nil {
			return verified, err
		}
		verified++
	}
	return verified, nil
}
