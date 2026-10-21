//go:build hack

package main

import (
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

type impactComparison struct {
	Case          string             `json:"case"`
	Mode          string             `json:"mode"`
	BeforeMS      impactDistribution `json:"before_ms"`
	AfterMS       impactDistribution `json:"after_ms"`
	ChangePercent float64            `json:"change_percent"`
	BeforeMiB     impactDistribution `json:"before_allocated_mib"`
	AfterMiB      impactDistribution `json:"after_allocated_mib"`
	BeforeKiB     impactDistribution `json:"before_response_kib"`
	AfterKiB      impactDistribution `json:"after_response_kib"`
	GraphSHA      string             `json:"current_graph_sha256,omitempty"`
	Nodes         int                `json:"nodes,omitempty"`
	Edges         int                `json:"edges,omitempty"`
}

func reportImpact(args []string) error {
	flags := flag.NewFlagSet("impact-report", flag.ContinueOnError)
	before := flags.String("before", "", "comma-separated before run directories")
	after := flags.String("after", "", "comma-separated after run directories")
	out := flags.String("out", "", "new absolute report directory under .tmp")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *before == "" || *after == "" || !filepath.IsAbs(*out) {
		return errors.New("impact-report requires --before, --after and an absolute --out")
	}
	old, err := loadImpactRuns(strings.Split(*before, ","), "before")
	if err != nil {
		return err
	}
	new, err := loadImpactRuns(strings.Split(*after, ","), "after")
	if err != nil {
		return err
	}
	if err := matchingImpactRuns(append(slices.Clone(old), new...)); err != nil {
		return err
	}
	rows, err := compareImpactRuns(old, new)
	if err != nil {
		return err
	}
	if err := os.Mkdir(*out, 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(*out, "summary.json"), rows); err != nil {
		return err
	}
	return writeImpactMarkdown(*out, append(slices.Clone(old), new...), rows)
}

func loadImpactRuns(directories []string, label string) ([]impactRun, error) {
	runs := make([]impactRun, len(directories))
	for index, directory := range directories {
		if err := readJSON(filepath.Join(directory, "results.json"), &runs[index]); err != nil {
			return nil, err
		}
		run := runs[index]
		cohorts := 1
		if run.Options.Only == "graph" {
			cohorts = 0
		}
		if run.Options.Only != "prune" {
			cohorts += len(impactSelectors) * 2
		}
		if !run.Complete || run.Error != "" || run.Options.Label != label || len(run.Observations) != cohorts*run.Options.Samples {
			return nil, fmt.Errorf("%s is an incomplete or mislabeled impact run", directory)
		}
	}
	return runs, nil
}

func matchingImpactRuns(runs []impactRun) error {
	first := runs[0]
	producers := map[string]string{}
	for _, run := range runs {
		if run.CorpusSHA != first.CorpusSHA || run.From.ID != first.From.ID || run.To.ID != first.To.ID ||
			run.GoVersion != first.GoVersion || run.Platform != first.Platform || run.GOMAXPROCS != first.GOMAXPROCS ||
			run.Options.Depth != first.Options.Depth || run.Options.Limit != first.Options.Limit || run.Options.Only != first.Options.Only {
			return fmt.Errorf("%s has different corpus, snapshots, runtime or graph bounds", run.Options.Output)
		}
		if previous := producers[run.Options.Label]; previous != "" && previous != run.ProducerSHA {
			return fmt.Errorf("%s uses a different %s executable", run.Options.Output, run.Options.Label)
		}
		producers[run.Options.Label] = run.ProducerSHA
	}
	if producers["before"] == producers["after"] {
		return errors.New("before and after are the same executable")
	}
	return nil
}

func impactCohorts(runs []impactRun) map[string][]impactObservation {
	cohorts := map[string][]impactObservation{}
	for _, run := range runs {
		for _, observed := range run.Observations {
			key := observed.Case + "\t" + observed.Mode
			cohorts[key] = append(cohorts[key], observed)
		}
	}
	return cohorts
}

func compareImpactRuns(before, after []impactRun) ([]impactComparison, error) {
	old, new := impactCohorts(before), impactCohorts(after)
	if !slices.Equal(slices.Sorted(maps.Keys(old)), slices.Sorted(maps.Keys(new))) {
		return nil, errors.New("before and after have different cohorts")
	}
	var rows []impactComparison
	for _, key := range slices.Sorted(maps.Keys(old)) {
		row, err := compareImpactCohort(old[key], new[key])
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func compareImpactCohort(before, after []impactObservation) (impactComparison, error) {
	first := before[0]
	for _, observed := range append(slices.Clone(before), after...) {
		if observed.ElapsedNS <= 0 || observed.Unreadable != 0 || observed.CurrentGraphSHA != first.CurrentGraphSHA ||
			observed.Nodes != first.Nodes || observed.Edges != first.Edges || observed.Pruned != nil && !observed.StatePreserved {
			return impactComparison{}, fmt.Errorf("%s/%s has invalid timing, unreadable source or unequal effective state", first.Case, first.Mode)
		}
	}
	row := impactComparison{Case: first.Case, Mode: first.Mode, GraphSHA: first.CurrentGraphSHA, Nodes: first.Nodes, Edges: first.Edges}
	for _, side := range []struct {
		samples      []impactObservation
		ms, mib, kib *impactDistribution
	}{{before, &row.BeforeMS, &row.BeforeMiB, &row.BeforeKiB}, {after, &row.AfterMS, &row.AfterMiB, &row.AfterKiB}} {
		var ms, mib, kib []float64
		for _, observed := range side.samples {
			ms = append(ms, float64(observed.ElapsedNS)/1e6)
			mib = append(mib, float64(observed.AllocatedBytes)/(1<<20))
			kib = append(kib, float64(observed.ResponseBytes)/1024)
		}
		var err error
		if *side.ms, err = impactSummary(ms); err != nil {
			return row, err
		}
		if *side.mib, err = impactSummary(mib); err != nil {
			return row, err
		}
		if *side.kib, err = impactSummary(kib); err != nil {
			return row, err
		}
	}
	row.ChangePercent = (row.AfterMS.Median/row.BeforeMS.Median - 1) * 100
	return row, nil
}

func writeImpactMarkdown(directory string, runs []impactRun, rows []impactComparison) error {
	var report strings.Builder
	fmt.Fprintf(&report, "# SQLite correctness-change performance impact\n\nCorpus SHA-256: `%s`. Runtime: `%s`, `%s`, GOMAXPROCS=%d.\n\n", runs[0].CorpusSHA, runs[0].GoVersion, runs[0].Platform, runs[0].GOMAXPROCS)
	report.WriteString("Samples are unprofiled and serial. Graph timings include pipeline creation in cold-pipeline cohorts, the query and JSON serialization; OS caches are warm in both cohorts. Warm pipelines receive one untimed warmup. Pruning includes its transaction and response serialization, with a fresh database copy and a warm metadata sweep before each sample. Copying, migration, correctness sweeps, allocation-counter reads and process startup are outside the timers. Each run performs an unrecorded pruning warmup.\n\n")
	report.WriteString("| Case | Mode | Samples before/after | Median ms before | Median ms after | Change | Allocated MiB before/after | Response KiB before/after |\n| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, row := range rows {
		fmt.Fprintf(&report, "| %s | %s | %d/%d | %.3f | %.3f | %+.1f%% | %.2f / %.2f | %.2f / %.2f |\n", row.Case, row.Mode,
			row.BeforeMS.Count, row.AfterMS.Count, row.BeforeMS.Median, row.AfterMS.Median, row.ChangePercent,
			row.BeforeMiB.Median, row.AfterMiB.Median, row.BeforeKiB.Median, row.AfterKiB.Median)
	}
	report.WriteString("\nThe current graph digest, node count and edge count match across every before/after sample. Each prune preserves the effective document/source bindings, symbol fingerprints and implements facts. Statistics preservation is reported separately because correcting those statistics changes the intended result.\n\n| Run | Producer SHA-256 | Stats preserved |\n| --- | --- | ---: |\n")
	for _, run := range runs {
		preserved, total := 0, 0
		for _, observed := range run.Observations {
			if observed.Pruned != nil {
				total++
				if reflect.DeepEqual(observed.StatsBefore, observed.StatsAfter) {
					preserved++
				}
			}
		}
		fmt.Fprintf(&report, "| [%s](%s/results.json) | `%s` | %d/%d |\n", filepath.Base(run.Options.Output), run.Options.Output, run.ProducerSHA, preserved, total)
	}
	report.WriteString("\n`summary.json` contains min, median, nearest-rank p95, maximum, allocation and response-size distributions. Raw samples, snapshot identities and bounds are in each run's `results.json`. These descriptive percentiles are not the plan's ordinary-commit p95 acceptance gate. No indexing latency was measured. PostgreSQL was excluded.\n")
	return os.WriteFile(filepath.Join(directory, "report.md"), []byte(report.String()), 0o644)
}
