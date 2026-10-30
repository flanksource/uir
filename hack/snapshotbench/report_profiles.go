//go:build hack

package main

import (
	"fmt"
	"sort"
	"strings"
)

func writeProfileSummary(report *strings.Builder, observations []observation) {
	report.WriteString("\n## SQL and sampled resources\n\n| Corpus | Path | Case | SQL queries | SQL ms | Max SQL ms | Samples | Peak RSS bytes | Disk read bytes | Disk write bytes |\n| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, result := range observations {
		if result.Evidence == nil || result.Evidence.SQL == nil {
			continue
		}
		sql := result.Evidence.SQL
		samples, rss, read, write := "unmeasured", "unmeasured", "unmeasured", "unmeasured"
		if process := result.Evidence.Process; process != nil && process.Samples > 0 {
			samples, rss = fmt.Sprint(process.Samples), fmt.Sprint(process.PeakRSS)
			if process.DiskIO != nil && process.DiskIO.Samples > 0 {
				read, write = fmt.Sprint(process.DiskIO.Read), fmt.Sprint(process.DiskIO.Write)
			}
		}
		fmt.Fprintf(report, "| %s | %s | %s | %d | %.2f | %.2f | %s | %s | %s | %s |\n", result.Case.Corpus, result.Case.Path, result.Case.ID, sql.QueryCount, sql.TotalMS, sql.MaxMS, samples, rss, read, write)
	}
	totals := map[string]phaseTotal{}
	for _, result := range observations {
		for name, phase := range result.Phases {
			key := result.Case.Corpus + "/" + result.Case.Path + "/" + name
			total := totals[key]
			total.Count += phase.Count
			total.InclusiveMS += phase.InclusiveMS
			total.ExclusiveMS += phase.ExclusiveMS
			totals[key] = total
		}
	}
	keys := make([]string, 0, len(totals))
	for key := range totals {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return totals[keys[i]].ExclusiveMS > totals[keys[j]].ExclusiveMS })
	report.WriteString("\n## Ranked trace regions\n\nAggregate region time across observations, sorted by exclusive time. Nested regions on the same goroutine are subtracted; regions on different goroutines can overlap, so these totals are not wall-clock percentages.\n\n| Corpus/path/region | Calls | Inclusive ms | Exclusive ms |\n| --- | ---: | ---: | ---: |\n")
	for _, key := range keys {
		phase := totals[key]
		fmt.Fprintf(report, "| %s | %d | %.2f | %.2f |\n", key, phase.Count, phase.InclusiveMS, phase.ExclusiveMS)
	}
	for _, result := range observations {
		if result.Coverage != nil {
			fmt.Fprintf(report, "\nOIPA filesystem coverage: %s. Omitted: %s. Individual snapshot coverage is in the JSON results; package diagnostics remain in the databases and captured SQL. Native Go uses the indexer's default coverage policy with test files excluded.\n", strings.Join(result.Coverage.Included, ", "), strings.Join(result.Coverage.Omitted, ", "))
			break
		}
	}
}
