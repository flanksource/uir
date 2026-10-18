package main

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"time"

	"gorm.io/gorm"
)

type captainRuntime struct {
	AllocatedBytes uint64  `json:"allocated_bytes"`
	HeapBefore     uint64  `json:"heap_before"`
	HeapAfter      uint64  `json:"heap_after"`
	GCCycles       uint32  `json:"gc_cycles"`
	GCPauseMS      float64 `json:"gc_pause_ms"`
}

type captainCaseResult struct {
	observation
	Runtime captainRuntime `json:"runtime"`
	SQL     sqlProfile     `json:"sql"`
}

func measureCaptainCase(ctx context.Context, database *gorm.DB, entry measurement, profile bool) (measured captainCaseResult, returnErr error) {
	measured.observation = observation{Case: entry}
	result := &measured.observation
	if profile {
		result.Trace = filepath.Join(entry.Directory, "index.trace")
	}
	defer func() {
		result.OK = returnErr == nil
		if returnErr != nil {
			result.Error = returnErr.Error()
		}
	}()
	var err error
	if result.Before, err = inspectDatabase(ctx, database, entry.Database); err != nil {
		return measured, err
	}
	indexDB := database
	var sql *sqlRecorder
	if profile {
		sql = newSQLRecorder(database.Logger)
		indexDB = database.Session(&gorm.Session{Logger: sql})
	}
	stop := func() error { return nil }
	if profile {
		stop, err = startProfiles(result, true, false)
		if err != nil {
			return measured, err
		}
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	result.Modules, err = indexGo(ctx, indexDB, entry)
	result.IndexMS = milliseconds(started)
	stopErr := stop()
	runtime.ReadMemStats(&after)
	measured.Runtime = captainRuntime{
		AllocatedBytes: after.TotalAlloc - before.TotalAlloc,
		HeapBefore:     before.HeapAlloc, HeapAfter: after.HeapAlloc,
		GCCycles:  after.NumGC - before.NumGC,
		GCPauseMS: float64(after.PauseTotalNs-before.PauseTotalNs) / float64(time.Millisecond),
	}
	if sql != nil {
		measured.SQL = sql.snapshot()
	}
	if err != nil || stopErr != nil {
		return measured, errors.Join(err, stopErr)
	}
	result.Command = []string{"native", "indexer", entry.Kind, entry.Commit}
	if result.After, err = inspectDatabase(ctx, database, entry.Database); err != nil {
		return measured, err
	}
	result.Delta = make(map[string]int64, len(result.After.Rows))
	for table, count := range result.After.Rows {
		result.Delta[table] = count - result.Before.Rows[table]
	}
	if err := verifyPublished(ctx, entry, result); err != nil {
		return measured, err
	}
	if err := validateObservation(*result); err != nil {
		return measured, err
	}
	if profile {
		if err := analyseProfiles(ctx, *result); err != nil {
			return measured, err
		}
		result.Phases, err = tracePhases(result.Trace)
		if err != nil {
			return measured, err
		}
	}
	return measured, nil
}
