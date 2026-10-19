package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func impactPruning(ctx context.Context, run *impactRun, seed string) error {
	for iteration := -1; iteration < run.Options.Samples; iteration++ {
		observed, err := impactPruneSample(ctx, run, seed)
		if err != nil {
			return err
		}
		if iteration >= 0 {
			observed.Case, observed.Mode, observed.Iteration = "prune", "warm-metadata", iteration
			if err := recordImpact(run, observed); err != nil {
				return err
			}
		}
	}
	return nil
}

func impactPruneSample(ctx context.Context, run *impactRun, seed string) (observed impactObservation, returnErr error) {
	directory, err := os.MkdirTemp(run.Options.Output, "prune-")
	if err != nil {
		return observed, err
	}
	path := filepath.Join(directory, "uir.db")
	if _, err := copyImpactDatabase(seed, path); err != nil {
		return observed, err
	}
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: path})
	if err != nil {
		return observed, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, closeDB(database))
		if returnErr == nil {
			returnErr = os.RemoveAll(directory)
		}
	}()
	before, err := impactState(ctx, database, run.To.ID)
	if err != nil {
		return observed, err
	}
	statsBefore, err := impactStats(ctx, database, run.To.ID)
	if err != nil {
		return observed, err
	}
	var pruned storage.PruneResult
	observed, err = timeImpact(func() ([]byte, error) {
		var err error
		pruned, err = storage.PruneHistory(ctx, database, run.Options.Root, storage.HistoryPruneOptions{Keep: 1})
		if err != nil {
			return nil, err
		}
		return json.Marshal(pruned)
	})
	if err != nil {
		return observed, err
	}
	return validateImpactPrune(ctx, database, run.To.ID, before, statsBefore, pruned, observed)
}

func validateImpactPrune(ctx context.Context, database *gorm.DB, snapshot uuid.UUID, before string, statsBefore storage.SnapshotStats, pruned storage.PruneResult, observed impactObservation) (impactObservation, error) {
	after, err := impactState(ctx, database, snapshot)
	if err != nil {
		return observed, err
	}
	if before != after || pruned.Rebased != 1 || pruned.Snapshots != 1 {
		return observed, fmt.Errorf("prune changed effective state or did not rebase exactly one survivor: before=%s after=%s pruned=%+v", before, after, pruned)
	}
	statsAfter, err := impactStats(ctx, database, snapshot)
	observed.Pruned, observed.StatsBefore, observed.StatsAfter, observed.StatePreserved = &pruned, &statsBefore, &statsAfter, true
	return observed, err
}

func impactState(ctx context.Context, database *gorm.DB, snapshot uuid.UUID) (string, error) {
	documents, err := storage.ActiveDocuments(ctx, database, snapshot, storage.ActiveDocumentOptions{})
	if err != nil {
		return "", err
	}
	symbols, err := storage.EffectiveSymbols(ctx, database, snapshot)
	if err != nil {
		return "", err
	}
	implements, err := storage.EffectiveImplements(ctx, database, snapshot, storage.ImplementsFilter{})
	if err != nil {
		return "", err
	}
	sort.Slice(implements, func(i, j int) bool {
		return implements[i].Type < implements[j].Type || implements[i].Type == implements[j].Type && implements[i].Interface < implements[j].Interface
	})
	body, err := json.Marshal(struct {
		Documents  map[string]storage.ActiveDocument
		Symbols    []storage.EffectiveSymbol
		Implements []storage.ImplementsPair
	}{documents, symbols, implements})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:]), nil
}

func impactStats(ctx context.Context, database *gorm.DB, id uuid.UUID) (storage.SnapshotStats, error) {
	var snapshot storage.ModuleSnapshot
	if err := database.WithContext(ctx).Where("id = ?", id).Take(&snapshot).Error; err != nil {
		return storage.SnapshotStats{}, err
	}
	return snapshot.Stats()
}
