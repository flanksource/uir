package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

func executeCase(ctx context.Context, descriptor string) (returnErr error) {
	var entry measurement
	if err := readJSON(descriptor, &entry); err != nil {
		return err
	}
	result := observation{Case: entry, Trace: filepath.Join(entry.Directory, "index.trace")}
	defer func() {
		result.OK = returnErr == nil
		if returnErr != nil {
			result.Error = returnErr.Error()
		}
		returnErr = errors.Join(returnErr, writeJSON(filepath.Join(entry.Directory, "result.json"), result), json.NewEncoder(os.Stdout).Encode(result))
	}()
	stop, err := startProfiles(&result, entry.Producer == "go")
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, stop()) }()
	started := time.Now()
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: entry.Database})
	if err != nil {
		return err
	}
	result.OpenMS = milliseconds(started)
	defer func() { returnErr = errors.Join(returnErr, closeDB(database)) }()
	result.Before, err = inspectDatabase(ctx, database, entry.Database)
	if err != nil {
		return err
	}
	started = time.Now()
	if entry.Producer == "go" {
		result.Modules, err = indexGo(ctx, database, entry)
		result.Command = []string{"native", "indexer", entry.Kind, entry.Commit}
	} else {
		result.Modules, result.Command, result.Coverage, err = indexOIPA(ctx, entry, result)
	}
	result.IndexMS = milliseconds(started)
	if err != nil {
		return err
	}
	result.After, err = inspectDatabase(ctx, database, entry.Database)
	if err != nil {
		return err
	}
	result.Delta = make(map[string]int64, len(result.After.Rows))
	for table, count := range result.After.Rows {
		result.Delta[table] = count - result.Before.Rows[table]
	}
	if err := verifyPublished(ctx, entry, &result); err != nil {
		return err
	}
	return validateObservation(result)
}

func indexGo(ctx context.Context, database *gorm.DB, entry measurement) ([]indexer.ModuleResult, error) {
	engine, err := indexer.New(database)
	if err != nil {
		return nil, err
	}
	if entry.Path == "historical" && entry.Kind != "initial" && entry.Kind != "noop" {
		result, err := engine.IndexRevision(ctx, indexer.RevisionOptions{RootKey: entry.RootKey, Checkout: entry.Checkout, Commit: entry.Commit, Reason: storage.ReasonHistorical, ScratchDirectory: filepath.Join(entry.Directory, "worktrees")})
		return []indexer.ModuleResult{result}, err
	}
	reason := storage.ReasonReindex
	if entry.Kind == "initial" {
		reason = storage.ReasonAdd
	}
	return engine.IndexModules(ctx, indexer.ModuleOptions{Path: entry.Checkout, ExactLocation: entry.Checkout, Reason: reason})
}

func indexOIPA(ctx context.Context, entry measurement, result observation) ([]indexer.ModuleResult, []string, *producerCoverage, error) {
	args := []string{"--no-progress", "--json", "--cpuprofile", result.CPU, "--go-trace", result.Trace,
		"symbols", "index", "--dir", entry.Checkout, "--revision", entry.Commit, "--dsn", entry.Database, "--memprofile", result.Heap}
	if entry.Path == "historical" && entry.Kind != "initial" && entry.Kind != "noop" {
		args = append(args, "--historical")
	}
	command := exec.CommandContext(ctx, entry.OIPA, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if writeErr := os.WriteFile(filepath.Join(entry.Directory, "producer.log"), stderr.Bytes(), 0o644); writeErr != nil {
		return nil, args, nil, errors.Join(err, writeErr)
	}
	if err != nil {
		return nil, args, nil, fmt.Errorf("OIPA producer: %w: %s", err, stderr.String())
	}
	var published struct {
		Modules  []indexer.ModuleResult `json:"modules"`
		Coverage *producerCoverage      `json:"coverage"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &published); err != nil {
		return nil, args, nil, fmt.Errorf("decode OIPA publication: %w: %s", err, stdout.String())
	}
	return published.Modules, append([]string{entry.OIPA}, args...), published.Coverage, nil
}

func milliseconds(started time.Time) float64 {
	return float64(time.Since(started).Microseconds()) / 1000
}

func closeDB(database *gorm.DB) error {
	connection, err := database.DB()
	if err != nil {
		return err
	}
	return connection.Close()
}
