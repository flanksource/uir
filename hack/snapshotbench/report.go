//go:build hack

package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func saveReport(manifest runManifest) (returnErr error) {
	var observations []observation
	var incomplete []string
	for _, entry := range manifest.Cases {
		var result observation
		if err := readJSON(filepath.Join(entry.Directory, "result.json"), &result); err != nil {
			incomplete = append(incomplete, entry.ID+": "+err.Error())
			continue
		}
		if result.OK {
			if manifest.ReusedGoRun == "" || entry.Corpus != "go" {
				if err := analyseProfiles(context.Background(), result); err != nil {
					returnErr = errors.Join(returnErr, err)
				}
			}
			evidence, err := loadFixtureEvidence(filepath.Join(entry.Directory, "gavel.json"))
			if err != nil {
				returnErr = errors.Join(returnErr, err)
			} else {
				result.Evidence = &evidence
			}
			if err := readJSON(filepath.Join(entry.Directory, "phases.json"), &result.Phases); err != nil {
				returnErr = errors.Join(returnErr, err)
			}
		}
		observations = append(observations, result)
	}
	returnErr = errors.Join(returnErr, writeJSON(filepath.Join(manifest.Directory, "observations.json"), observations))
	returnErr = errors.Join(returnErr, writeCSV(manifest.Directory, observations))
	var report strings.Builder
	fmt.Fprintf(&report, "# Incremental snapshot profile\n\nRun: `%s`. Started %s. One observation per case, warm standard Go caches, fixed producer executable.\n\n", manifest.Directory, manifest.StartedAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Fprintf(&report, "Toolchain: `%s`. Cache settings:\n\n```text\n%s\n```\n\n", manifest.GoVersion, manifest.Cache)
	if manifest.ReusedGoRun != "" {
		fmt.Fprintf(&report, "Go observations retain their original descriptors, commands, profiles and executed runner from `%s`. Its manifest and build inputs provide the Go provenance; `go-runner` and `go-gavel` record those original binary hashes. Rules observations use this run's frozen OIPA producer and runner. No earlier rules observations are included.\n\n", manifest.ReusedGoRun)
	}
	report.WriteString("[Manifest](manifest.json) · [JSON](observations.json) · [CSV](observations.csv). Each case directory retains its descriptor, Gavel fixture/log/JSON, source checks, CPU and heap profiles, execution trace and phase totals. Raw Gavel benchmark and resource reports are under `.gavel/`. Process sampling may miss short commands; zero samples mean unmeasured.\n\n")
	report.WriteString("Native Go index time excludes the separately timed database open. OIPA index time includes command startup, archive preparation, its own database open, lowering, publication and cleanup; inspect the OIPA trace regions to separate these costs. SQL includes indexing and the worker's storage/source checks. Resource samples cover each fixture process tree. Runs share standard warm caches; concurrent activity and profiling overhead affect timings. Source checksum files describe the working trees at launch; binary hashes and build info identify the exact executed producers.\n\n")
	report.WriteString("Native Go checkout replay retains all published snapshots. OIPA automatically prunes superseded current snapshots and preserves historical snapshots. Row deltas are net counts after pruning, so zero growth can include publication and deletion. Parsed/reused counters describe the producer's reported publication results; native Go historical reuse can return zero counters after typechecking and extraction. Use trace regions and SQL evidence to identify that work.\n\n")
	report.WriteString("| Corpus | Path | Case | Commit | Checkout ms | Open ms | Index ms | Parsed | Reused | Snapshots Δ | Documents Δ | DB bytes |\n| --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, result := range observations {
		parsed, reused := fileCounts(result)
		link, err := filepath.Rel(manifest.Directory, result.Case.Directory)
		if err != nil {
			return errors.Join(returnErr, err)
		}
		fmt.Fprintf(&report, "| %s | %s | [%s](%s/result.json) | %.12s | %.2f | %.2f | %.2f | %d | %d | %d | %d | %d |\n", result.Case.Corpus, result.Case.Path, result.Case.Kind, filepath.ToSlash(link), result.Case.Commit, result.Case.CheckoutMS, result.OpenMS, result.IndexMS, parsed, reused, result.Delta["snapshots"], result.Delta["documents"], result.After.Bytes+result.After.WAL)
	}
	report.WriteString("\n## Slowest indexing observations\n\n")
	sort.Slice(observations, func(i, j int) bool { return observations[i].IndexMS > observations[j].IndexMS })
	for _, result := range observations[:min(5, len(observations))] {
		fmt.Fprintf(&report, "- %s/%s/%s %.12s: %.2f ms. Inspect `%s/phases.json` and `cpu-top.txt`.\n", result.Case.Corpus, result.Case.Path, result.Case.Kind, result.Case.Commit, result.IndexMS, result.Case.Directory)
	}
	if len(incomplete) > 0 {
		fmt.Fprintf(&report, "\nIncomplete cases:\n\n%s\n", strings.Join(incomplete, "\n"))
	}
	writeProfileSummary(&report, observations)
	return errors.Join(returnErr, os.WriteFile(filepath.Join(manifest.Directory, "report.md"), []byte(report.String()), 0o644))
}

func fileCounts(result observation) (parsed, reused int) {
	for _, module := range result.Modules {
		parsed, reused = parsed+module.ParsedFiles, reused+module.ReusedFiles
	}
	return parsed, reused
}

func writeCSV(directory string, observations []observation) (returnErr error) {
	file, err := os.Create(filepath.Join(directory, "observations.csv"))
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"corpus", "path", "case", "commit", "checkout_ms", "open_ms", "index_ms", "parsed", "reused", "snapshot_delta", "document_delta", "db_bytes", "wal_bytes", "ok"}); err != nil {
		return err
	}
	for _, result := range observations {
		parsed, reused := fileCounts(result)
		if err := writer.Write([]string{result.Case.Corpus, result.Case.Path, result.Case.Kind, result.Case.Commit, fmt.Sprint(result.Case.CheckoutMS), fmt.Sprint(result.OpenMS), fmt.Sprint(result.IndexMS), fmt.Sprint(parsed), fmt.Sprint(reused), fmt.Sprint(result.Delta["snapshots"]), fmt.Sprint(result.Delta["documents"]), fmt.Sprint(result.After.Bytes), fmt.Sprint(result.After.WAL), fmt.Sprint(result.OK)}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}
