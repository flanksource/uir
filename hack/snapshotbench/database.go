//go:build hack

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var measuredTables = []string{"snapshots", "documents", "source_revisions", "source_blobs", "source_deltas", "symbol_deltas", "package_coverage", "symbol_postings", "symbols", "locations", "location_heads"}

func inspectDatabase(ctx context.Context, database *gorm.DB, path string) (databaseState, error) {
	state := databaseState{Rows: map[string]int64{}, Heads: map[string]head{}}
	for _, table := range measuredTables {
		var count int64
		if err := database.WithContext(ctx).Table(table).Count(&count).Error; err != nil {
			return state, err
		}
		state.Rows[table] = count
	}
	var heads []struct {
		RootKey, SnapshotID string
		Version             int64
	}
	if err := database.WithContext(ctx).Table("location_heads AS head").Select("root.root_key, head.snapshot_id, head.version").
		Joins("JOIN modules AS root ON root.id = head.root_id").Find(&heads).Error; err != nil {
		return state, err
	}
	for _, row := range heads {
		state.Heads[row.RootKey] = head{Snapshot: row.SnapshotID, Version: row.Version}
	}
	var err error
	state.Bytes, err = fileSize(path)
	if err != nil {
		return state, err
	}
	state.WAL, err = fileSize(path + "-wal")
	return state, err
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func verifyPublished(ctx context.Context, entry measurement, result *observation) (returnErr error) {
	database, err := storage.OpenReadOnly(ctx, storage.DBOptions{DSN: entry.Database})
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, closeDB(database)) }()
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return err
	}
	for _, module := range result.Modules {
		rows, _, err := storage.ModuleSnapshots(ctx, database, storage.ModuleSnapshotListOptions{RootKey: module.RootKey, Location: module.Location, Limit: 1000})
		if err != nil {
			return err
		}
		found := false
		for _, row := range rows {
			if row.ID.String() == module.SnapshotID {
				result.Snapshots = append(result.Snapshots, row)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("published snapshot %s is missing after reopening", module.SnapshotID)
		}
		if err := verifySource(ctx, database, pipeline, module.SnapshotID); err != nil {
			return err
		}
		result.SourceVerified++
	}
	return nil
}

func verifySource(ctx context.Context, database *gorm.DB, pipeline *query.Pipeline, snapshot string) error {
	id, err := uuid.Parse(snapshot)
	if err != nil {
		return err
	}
	sources, err := storage.EffectiveSources(ctx, database, storage.EffectiveSourcesOptions{SnapshotID: id})
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(sources))
	for path := range sources {
		if err := validateSourcePath(path); err != nil {
			return fmt.Errorf("snapshot %s: %w", snapshot, err)
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	sample, err := sampleSource(paths)
	if err != nil {
		return fmt.Errorf("snapshot %s: %w", snapshot, err)
	}
	source, err := pipeline.ReadModuleSource(ctx, snapshot, sample)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(source.Content))
	if hex.EncodeToString(digest[:]) != sources[sample].ContentHash {
		return fmt.Errorf("snapshot %s source %s failed its SHA-256 round-trip", snapshot, sample)
	}
	return nil
}
