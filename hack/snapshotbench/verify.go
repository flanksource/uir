//go:build hack

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

func validateObservation(result observation) error {
	if len(result.Modules) == 0 || len(result.Snapshots) != len(result.Modules) {
		return fmt.Errorf("%s: incomplete module/snapshot results", result.Case.ID)
	}
	for i, module := range result.Modules {
		snapshot := result.Snapshots[i]
		if module.Error != "" || module.SnapshotID != snapshot.ID.String() || snapshot.GitCommit != result.Case.Commit {
			return fmt.Errorf("%s: module %q has wrong snapshot/commit or error %q", result.Case.ID, module.RootKey, module.Error)
		}
		if result.Case.Path == "head" || result.Case.Kind == "initial" {
			if result.After.Heads[module.RootKey].Snapshot != module.SnapshotID {
				return fmt.Errorf("%s: %q did not become its location head", result.Case.ID, module.RootKey)
			}
		}
		if result.Case.Kind == "noop" || result.Case.Kind == "repeat" {
			if !module.Unchanged {
				return fmt.Errorf("%s: %q did not reuse its snapshot", result.Case.ID, module.RootKey)
			}
		}
	}
	if result.Case.Path == "historical" && result.Case.Kind != "initial" && !reflect.DeepEqual(result.Before.Heads, result.After.Heads) {
		return fmt.Errorf("%s: historical operation moved heads", result.Case.ID)
	}
	if result.Case.Kind == "noop" || result.Case.Kind == "repeat" {
		for table, count := range result.Delta {
			if count != 0 {
				return fmt.Errorf("%s: reuse changed %s by %d rows", result.Case.ID, table, count)
			}
		}
	}
	if result.SourceVerified == 0 {
		return fmt.Errorf("%s: no source round-trip was verified", result.Case.ID)
	}
	return nil
}

func verifyRun(directory string) error {
	var manifest runManifest
	if err := readJSON(filepath.Join(directory, "manifest.json"), &manifest); err != nil {
		return err
	}
	if err := validateReplayMatrix(manifest); err != nil {
		return err
	}
	for _, name := range []string{"report.md", "observations.json", "observations.csv"} {
		path := filepath.Join(directory, name)
		if size, err := fileSize(path); err != nil || size == 0 {
			return fmt.Errorf("missing replay artifact %s (size %d): %v", path, size, err)
		}
	}
	for _, measurement := range manifest.Cases {
		var result observation
		if err := readJSON(filepath.Join(measurement.Directory, "result.json"), &result); err != nil {
			return err
		}
		if !result.OK {
			return fmt.Errorf("%s failed: %s", measurement.ID, result.Error)
		}
		if result.Case != measurement {
			return fmt.Errorf("%s result descriptor differs from the replay manifest", measurement.ID)
		}
		if err := validateObservation(result); err != nil {
			return err
		}
		evidence, err := loadFixtureEvidence(filepath.Join(measurement.Directory, "gavel.json"))
		if err != nil {
			return err
		}
		if evidence.SQL == nil || evidence.SQL.QueryCount == 0 {
			return fmt.Errorf("%s has no captured SQL evidence", measurement.ID)
		}
		for _, artifact := range []string{result.CPU, result.Heap, result.Trace, evidence.SQL.Path,
			filepath.Join(measurement.Directory, "phases.json"), filepath.Join(measurement.Directory, "cpu-top.txt"), filepath.Join(measurement.Directory, "heap-top.txt")} {
			size, err := fileSize(artifact)
			if err != nil || size == 0 {
				return fmt.Errorf("%s missing profile %s (size %d): %v", measurement.ID, artifact, size, err)
			}
		}
	}
	retained, err := verifyRetainedSources(context.Background(), manifest)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "cases": len(manifest.Cases), "transitions": manifest.Transitions, "retained_source_samples": retained})
}
