package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type checkpointContextKey struct{}

type indexCheckpoint struct {
	mu       sync.Mutex
	key      string
	inputs   checkpointInputs
	eligible bool
	database *gorm.DB
}

type checkpointInputs struct {
	Repositories map[string]string
	Files        map[string]string
	Catalog      string
}

func (indexer *Indexer) prepareCheckpoint(ctx context.Context, requests []ModuleOptions) (*indexCheckpoint, []ModuleResult, error) {
	key, err := checkpointKey(ctx, requests)
	if err != nil || key == "" {
		return nil, nil, err
	}
	checkpoint := &indexCheckpoint{key: key, eligible: true, database: indexer.database,
		inputs: checkpointInputs{Repositories: map[string]string{}, Files: map[string]string{}}}
	var stored storage.IndexCheckpoint
	err = indexer.database.WithContext(ctx).Where("key = ?", key).Take(&stored).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return checkpoint, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load index checkpoint: %w", err)
	}
	var inputs checkpointInputs
	var results []ModuleResult
	var heads map[string]string
	for _, value := range []struct {
		data   []byte
		target any
	}{{stored.Inputs, &inputs}, {stored.Results, &results}, {stored.Heads, &heads}} {
		if err := json.Unmarshal(value.data, value.target); err != nil {
			return nil, nil, fmt.Errorf("decode index checkpoint: %w", err)
		}
	}
	if len(inputs.Repositories) == 0 || len(results) == 0 || len(heads) == 0 || stored.Fingerprint == "" {
		return nil, nil, errors.New("invalid empty index checkpoint")
	}
	current, err := checkpoint.refresh(ctx, inputs)
	if err != nil {
		return nil, nil, err
	}
	if current != stored.Fingerprint {
		return checkpoint, nil, nil
	}
	valid, err := checkpoint.headsCurrent(ctx, heads)
	if err != nil || !valid {
		return checkpoint, nil, err
	}
	for i := range results {
		results[i].Unchanged, results[i].ParsedFiles, results[i].ReusedFiles = true, 0, results[i].Files
	}
	return checkpoint, results, nil
}

func checkpointKey(ctx context.Context, requests []ModuleOptions) (string, error) {
	var values []string
	for _, request := range requests {
		if request.Force || !headReasons[request.Reason] {
			return "", nil
		}
		path, err := filepath.Abs(request.Path)
		if err != nil {
			return "", err
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
		environment, err := readGoEnvironment(ctx, path)
		if err != nil {
			return "", err
		}
		values = append(values, fmt.Sprintf("%s\x00%s\x00%t\x00%s\x00%s\x00%s", path, request.ExactLocation, request.ExistingOnly, request.Reason,
			configurationHash(request.IncludeTests, environment.Variant), environment.WorkFile))
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return hashBytes(append([]byte("index-checkpoint-v1\x00"), encoded...)), nil
}

func (checkpoint *indexCheckpoint) save(ctx context.Context, memo *runMemo, results []ModuleResult) error {
	checkpoint.mu.Lock()
	defer checkpoint.mu.Unlock()
	if !checkpoint.eligible || len(checkpoint.inputs.Repositories) == 0 {
		return nil
	}
	before, err := checkpointDigest(checkpoint.inputs)
	if err != nil {
		return err
	}
	current, err := checkpoint.refresh(ctx, checkpoint.inputs)
	if err != nil || current != before {
		return err
	}
	heads := map[string]string{}
	memo.mu.Lock()
	for _, result := range memo.modules {
		heads[result.Location] = result.SnapshotID
	}
	memo.mu.Unlock()
	var snapshots []storage.ModuleSnapshot
	var ids []string
	for _, id := range heads {
		ids = append(ids, id)
	}
	if err := checkpoint.database.WithContext(ctx).Where("id IN ?", ids).Find(&snapshots).Error; err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		if !snapshot.CoverageSettled() {
			return nil
		}
	}
	valid, err := checkpoint.headsCurrent(ctx, heads)
	if err != nil || !valid {
		return err
	}
	checkpoint.inputs.Catalog, err = checkpoint.catalog(ctx)
	if err != nil {
		return err
	}
	record := storage.IndexCheckpoint{Key: checkpoint.key}
	for _, value := range []struct {
		target *storage.JSON
		value  any
	}{{&record.Inputs, checkpoint.inputs}, {&record.Results, results}, {&record.Heads, heads}} {
		encoded, err := json.Marshal(value.value)
		if err != nil {
			return err
		}
		*value.target = encoded
	}
	record.Fingerprint, err = checkpointDigest(checkpoint.inputs)
	if err != nil {
		return err
	}
	return checkpoint.database.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, UpdateAll: true}).Create(&record).Error
}

func checkpointDigest(inputs checkpointInputs) (string, error) {
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return "", err
	}
	return hashBytes(encoded), nil
}

func (checkpoint *indexCheckpoint) headsCurrent(ctx context.Context, expected map[string]string) (bool, error) {
	var paths []string
	for path := range expected {
		paths = append(paths, path)
	}
	var rows []struct{ CanonicalPath, SnapshotID string }
	if err := checkpoint.database.WithContext(ctx).Table("location_heads AS head").
		Select("location.canonical_path, head.snapshot_id").Joins("JOIN locations AS location ON location.id = head.location_id").
		Where("location.canonical_path IN ?", paths).Scan(&rows).Error; err != nil {
		return false, err
	}
	if len(rows) != len(expected) {
		return false, nil
	}
	for _, row := range rows {
		if expected[row.CanonicalPath] != row.SnapshotID {
			return false, nil
		}
	}
	return true, nil
}

func (checkpoint *indexCheckpoint) catalog(ctx context.Context) (string, error) {
	var locations []storage.ModuleLocation
	if err := checkpoint.database.WithContext(ctx).Order("id").Find(&locations).Error; err != nil {
		return "", err
	}
	var siblings []storage.SnapshotSiblingShape
	if err := checkpoint.database.WithContext(ctx).Table("snapshot_sibling_shapes AS shape").Select("shape.*").
		Joins("JOIN location_heads AS head ON head.snapshot_id = shape.snapshot_id").
		Order("shape.snapshot_id, shape.import_path").Scan(&siblings).Error; err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		Locations []storage.ModuleLocation
		Siblings  []storage.SnapshotSiblingShape
	}{locations, siblings})
	if err != nil {
		return "", err
	}
	return hashBytes(encoded), nil
}
