package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

type analysisInputs struct {
	Packages map[string]string
	Digests  map[string]string
	Files    map[string]string
}

func (indexer *Indexer) analysisBase(ctx context.Context, root discoveredRoot) (storage.AnalysisHead, storage.ModuleSnapshot, error) {
	var head storage.AnalysisHead
	var snapshot storage.ModuleSnapshot
	err := indexer.database.WithContext(ctx).Table("analysis_heads AS head").Select("head.*").
		Joins("JOIN locations AS location ON location.id = head.location_id").
		Joins("JOIN modules AS module ON module.id = location.root_id").
		Where("module.root_key = ? AND location.canonical_path = ? AND head.mode = ? AND head.configuration_hash = ?", root.RootKey, root.LocalPath, root.AnalysisMode, root.ConfigurationHash).Take(&head).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return head, snapshot, nil
	}
	if err != nil {
		return head, snapshot, err
	}
	err = indexer.database.WithContext(ctx).Where("id = ?", head.SnapshotID).Take(&snapshot).Error
	return head, snapshot, err
}

func analysisCurrent(_ context.Context, root discoveredRoot, head storage.AnalysisHead, snapshot storage.ModuleSnapshot) (bool, error) {
	if head.Version == 0 || snapshot.ContentSetHash != root.ContentSetHash || snapshot.ConfigurationHash != root.ConfigurationHash || snapshot.Revision != root.Revision {
		return false, nil
	}
	var inputs analysisInputs
	if err := json.Unmarshal(head.Inputs, &inputs); err != nil {
		return false, fmt.Errorf("decode analysis inputs: %w", err)
	}
	if inputs.Packages == nil || inputs.Digests == nil || inputs.Files == nil {
		return false, errors.New("invalid analysis input manifest")
	}
	return inputs.current()
}

func (inputs analysisInputs) current() (bool, error) {
	for path, expected := range inputs.Files {
		content, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			if expected == "missing" {
				continue
			}
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if hashBytes(content) != expected {
			return false, nil
		}
	}
	for directory, module := range inputs.Packages {
		digest, err := packageSourceDigest(module, directory, nil)
		if err != nil {
			return false, err
		}
		if digest != inputs.Digests[directory] {
			return false, nil
		}
	}
	return true, nil
}

func analysisManifests(root discoveredRoot) (analysisInputs, error) {
	inputs := analysisInputs{Packages: map[string]string{}, Digests: map[string]string{}, Files: map[string]string{}}
	if root.AnalysisMode == storage.AnalysisSyntax {
		return inputs, nil
	}
	modules, err := diskModules(root)
	if err != nil {
		return inputs, err
	}
	modules[root.RootKey] = root.LocalPath
	for _, directory := range modules {
		for _, name := range []string{"go.mod", "go.sum"} {
			path := filepath.Join(directory, name)
			content, err := os.ReadFile(path)
			if errors.Is(err, os.ErrNotExist) && name == "go.sum" {
				inputs.Files[path] = "missing"
				continue
			}
			if err != nil {
				return inputs, err
			}
			inputs.Files[path] = hashBytes(content)
		}
	}
	return inputs, nil
}

func storeAnalysisHead(ctx context.Context, tx *gorm.DB, previous storage.AnalysisHead, snapshot storage.ModuleSnapshot, inputs analysisInputs) error {
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return err
	}
	head := storage.AnalysisHead{LocationID: snapshot.LocationID, Mode: snapshot.AnalysisMode, ConfigurationHash: snapshot.ConfigurationHash, SnapshotID: snapshot.ID, Version: previous.Version + 1, Inputs: encoded}
	if previous.Version == 0 {
		err := tx.WithContext(ctx).Create(&head).Error
		if storage.IsUniqueViolation(err) {
			return fmt.Errorf("analysis head was published concurrently: %w", ErrIndexInputsChanged)
		}
		return err
	}
	updated := tx.WithContext(ctx).Model(&storage.AnalysisHead{}).Where("location_id = ? AND mode = ? AND configuration_hash = ? AND version = ?", head.LocationID, head.Mode, head.ConfigurationHash, previous.Version).
		Updates(map[string]any{"snapshot_id": head.SnapshotID, "version": head.Version, "inputs": head.Inputs})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrIndexInputsChanged
	}
	return nil
}
