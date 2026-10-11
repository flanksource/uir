package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func benchmarkFiles(directory string) (map[string]bool, error) {
	paths, err := filepath.Glob(filepath.Join(directory, ".gavel", "benchmarks", "fixtures", "run-*.json"))
	result := make(map[string]bool, len(paths))
	for _, path := range paths {
		result[path] = true
	}
	return result, err
}

func captureBenchmark(directory, output string, previous map[string]bool) error {
	current, err := benchmarkFiles(directory)
	if err != nil {
		return err
	}
	var artifacts []string
	for path := range current {
		if !previous[path] {
			artifacts = append(artifacts, path)
		}
	}
	if len(artifacts) != 1 {
		return fmt.Errorf("one Gavel report required, got %d", len(artifacts))
	}
	body, err := os.ReadFile(artifacts[0])
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, "gavel.json"), body, 0o644)
}

type fixtureEvidence struct {
	Name     string            `json:"name"`
	Passed   *bool             `json:"passed"`
	Children []fixtureEvidence `json:"children"`
	Process  *processEvidence  `json:"fixture_profile"`
	SQL      *sqlEvidence      `json:"sql_profile"`
}

type sqlEvidence struct {
	Path           string  `json:"path"`
	QueryCount     int     `json:"query_count"`
	SlowQueryCount int     `json:"slow_query_count"`
	TotalMS        float64 `json:"total_duration_ms"`
	MaxMS          float64 `json:"max_query_ms"`
}

type processEvidence struct {
	Scope   string        `json:"scope"`
	Samples int           `json:"sample_count"`
	PeakCPU float64       `json:"peak_cpu_percent"`
	PeakRSS uint64        `json:"peak_rss_bytes"`
	DiskIO  *diskEvidence `json:"disk_io,omitempty"`
}

type diskEvidence struct {
	Read    uint64 `json:"disk_read_bytes"`
	Write   uint64 `json:"disk_write_bytes"`
	Samples int    `json:"sample_count"`
	Missing int    `json:"missing_processes"`
}

func loadFixtureEvidence(path string) (fixtureEvidence, error) {
	var report struct {
		Tests []fixtureEvidence `json:"tests"`
	}
	if err := readJSON(path, &report); err != nil {
		return fixtureEvidence{}, err
	}
	var matches []fixtureEvidence
	var visit func([]fixtureEvidence)
	visit = func(nodes []fixtureEvidence) {
		for _, node := range nodes {
			if node.Passed != nil {
				matches = append(matches, node)
			}
			visit(node.Children)
		}
	}
	visit(report.Tests)
	if len(matches) != 1 {
		return fixtureEvidence{}, fmt.Errorf("expected one fixture verdict, got %d", len(matches))
	}
	if !*matches[0].Passed {
		return fixtureEvidence{}, errors.New("gavel fixture failed")
	}
	return matches[0], nil
}
