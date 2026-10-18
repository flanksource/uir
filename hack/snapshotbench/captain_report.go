package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

func reportCaptain(ctx context.Context, database *gorm.DB, run captainRun) error {
	if len(run.Cases) != 3 {
		return fmt.Errorf("captain report requires three observations, got %d", len(run.Cases))
	}
	first, err := captainCoverage(ctx, database, run.Cases[0].Modules[0].SnapshotID)
	if err != nil {
		return err
	}
	second, err := captainCoverage(ctx, database, run.Cases[1].Modules[0].SnapshotID)
	if err != nil {
		return err
	}
	changes := comparePackageCoverage(first, second)
	if err := writeJSON(filepath.Join(run.Directory, "package-changes.json"), changes); err != nil {
		return err
	}
	changedSources, err := commandOutput(ctx, "git", "-C", run.Checkouts["captain"], "diff", "--name-only", run.First, run.Second, "--", "*.go")
	if err != nil {
		return err
	}
	var productionChanges int
	for _, path := range strings.Split(changedSources, "\n") {
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			productionChanges++
		}
	}
	var report strings.Builder
	fmt.Fprintf(&report, "# Captain historical index profile\n\nRun: `%s` · profiled: `%t` · UIR executable SHA-256: `%s`.\n\n", run.Directory, run.Profiled, run.ProducerSHA)
	report.WriteString("The database was empty apart from four registered checkouts. Requests ran in one process, in order. Source verification and report queries ran after each timed index request.\n\n")
	if run.Profiled {
		report.WriteString("CPU and trace profiles cover indexing only. Heap profiles do not force a GC between requests.\n\n")
	}
	report.WriteString("| Request | Commit | Index ms | Parsed | Reused | Unchanged | Allocated MB | GC cycles | GC pause ms | SQL count | SQL ms |\n| --- | --- | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, observed := range run.Cases {
		module := observed.Modules[0]
		fmt.Fprintf(&report, "| %s | %.12s | %.1f | %d | %d | %t | %.1f | %d | %.1f | %d | %.1f |\n", observed.Case.ID, observed.Case.Commit, observed.IndexMS, module.ParsedFiles, module.ReusedFiles, module.Unchanged, float64(observed.Runtime.AllocatedBytes)/1e6, observed.Runtime.GCCycles, observed.Runtime.GCPauseMS, observed.SQL.Count, observed.SQL.TotalMS)
	}
	fmt.Fprintf(&report, "\nBetween the commits, %d non-test Go files changed. Package input hashes changed in %d packages covering %d files; %d packages were removed. Full details: [package changes](package-changes.json).\n", productionChanges, changes.ChangedPackages, changes.ChangedFiles, changes.RemovedPackages)
	for _, observed := range run.Cases {
		fmt.Fprintf(&report, "\n## %s\n\n", observed.Case.ID)
		if run.Profiled {
			fmt.Fprintf(&report, "[CPU profile](%s/cpu.pprof) · [heap profile](%s/mem.pprof) · [trace](%s/index.trace) · [phase totals](%s/phases.json) · [CPU top](%s/cpu-top.txt).\n\n", observed.Case.ID, observed.Case.ID, observed.Case.ID, observed.Case.ID, observed.Case.ID)
			report.WriteString("| Phase | Calls | Inclusive ms | Exclusive ms |\n| --- | ---: | ---: | ---: |\n")
			for _, name := range []string{"checkout", "discover", "versioned-deps", "typecheck", "extract", "render-documents", "symbol-facts", "reuse-check", "publish", "documents", "symbol-deltas"} {
				if phase, found := observed.Phases[name]; found {
					fmt.Fprintf(&report, "| %s | %d | %.1f | %.1f |\n", name, phase.Count, phase.InclusiveMS, phase.ExclusiveMS)
				}
			}
			report.WriteString("\n| SQL operation | Calls | Total ms | Max ms |\n| --- | ---: | ---: | ---: |\n")
			for i, group := range observed.SQL.Groups {
				if i >= 12 {
					break
				}
				fmt.Fprintf(&report, "| %s | %d | %.1f | %.1f |\n", group.Label, group.Count, group.TotalMS, group.MaxMS)
			}
		}
	}
	return os.WriteFile(filepath.Join(run.Directory, "report.md"), []byte(report.String()), 0o644)
}

func captainCoverage(ctx context.Context, database *gorm.DB, snapshotID string) (map[string]storage.PackageCoverage, error) {
	var rows []storage.PackageCoverage
	if err := database.WithContext(ctx).Where("snapshot_id = ?", snapshotID).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("snapshot %s has no package coverage", snapshotID)
	}
	result := make(map[string]storage.PackageCoverage, len(rows))
	for _, row := range rows {
		result[row.PackagePath] = row
	}
	return result, nil
}
