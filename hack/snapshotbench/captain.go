//go:build hack

package main

import (
	"sort"

	"github.com/flanksource/uir/storage"
)

type packageChange struct {
	Path  string `json:"path"`
	Files int    `json:"files"`
	Kind  string `json:"kind"`
}

type packageChangeSummary struct {
	ChangedPackages int             `json:"changed_packages"`
	ChangedFiles    int             `json:"changed_files"`
	RemovedPackages int             `json:"removed_packages"`
	Rows            []packageChange `json:"rows"`
}

type captainSequence struct {
	First, Second, Path string
}

func captainMeasurements(options captainSequence) []measurement {
	base := measurement{Corpus: "captain", Producer: "go", Path: options.Path, RootKey: "github.com/flanksource/captain"}
	firstKind := "revision"
	if options.Path == "head" {
		firstKind = "initial"
	}
	return []measurement{
		{ID: "00-first", Corpus: base.Corpus, Producer: base.Producer, Path: base.Path, Kind: firstKind, Commit: options.First, RootKey: base.RootKey},
		{ID: "01-second", Corpus: base.Corpus, Producer: base.Producer, Path: base.Path, Kind: "revision", Commit: options.Second, RootKey: base.RootKey},
		{ID: "02-repeat", Corpus: base.Corpus, Producer: base.Producer, Path: base.Path, Kind: "repeat", Commit: options.Second, RootKey: base.RootKey},
	}
}

func comparePackageCoverage(before, after map[string]storage.PackageCoverage) packageChangeSummary {
	summary := packageChangeSummary{Rows: []packageChange{}}
	for path, current := range after {
		previous, found := before[path]
		if found && previous.InputHash == current.InputHash {
			continue
		}
		kind := "changed"
		if !found {
			kind = "added"
		}
		summary.Rows = append(summary.Rows, packageChange{Path: path, Files: current.FileCount, Kind: kind})
		summary.ChangedPackages++
		summary.ChangedFiles += current.FileCount
	}
	for path, previous := range before {
		if _, found := after[path]; !found {
			summary.Rows = append(summary.Rows, packageChange{Path: path, Files: previous.FileCount, Kind: "removed"})
			summary.RemovedPackages++
		}
	}
	sort.Slice(summary.Rows, func(i, j int) bool { return summary.Rows[i].Path < summary.Rows[j].Path })
	return summary
}
