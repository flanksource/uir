package indexer

import (
	"context"
	"fmt"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"sync"
	"time"

	rpchttp "github.com/flanksource/clicky/rpc/http"
	"github.com/flanksource/clicky/task"
)

type phaseKey struct{}
type progressKey struct{}

// phaseCounts is one running phase's counters, in the order they were first counted.
type phaseCounts struct {
	depth      int
	taskID     string
	breadcrumb string
	mu         sync.Mutex
	counters   []rpchttp.TimingCounter
}

// PhaseCount adds value to the named counter of the innermost phase running under ctx; outside a phase
// it does nothing.
func PhaseCount(ctx context.Context, counter string, value int64) {
	counts, inPhase := ctx.Value(phaseKey{}).(*phaseCounts)
	if !inPhase {
		return
	}
	counts.mu.Lock()
	defer counts.mu.Unlock()
	for index := range counts.counters {
		if counts.counters[index].Name == counter {
			counts.counters[index].Value += value
			return
		}
	}
	counts.counters = append(counts.counters, rpchttp.TimingCounter{Name: counter, Value: value})
}

// Phase runs fn as one named indexing phase: CPU samples taken while it runs, including on goroutines
// it starts, carry the pprof label phase=<name>, and an execution trace shows it as a region. Phases nest; the innermost phase's
// label wins in a profile, and regions nest in a trace. Its duration, with calls=1 and every PhaseCount
// made inside it, is added to the server timings in ctx, and it logs one line to the phase log in ctx.
// While it runs, the phase log's description is the breadcrumb of the phases running on that log.
func Phase(ctx context.Context, name string, fn func(context.Context) error) error {
	counts := &phaseCounts{breadcrumb: displayPhase(name), counters: []rpchttp.TimingCounter{{Name: "calls", Value: 1}}}
	log := task.TaskFromContext(ctx)
	logged := log != nil
	parent, nested := ctx.Value(phaseKey{}).(*phaseCounts)
	if nested {
		counts.depth = parent.depth + 1
	}
	if logged {
		counts.taskID = log.ID()
		if nested && parent.taskID == log.ID() && counts.breadcrumb == "" {
			counts.breadcrumb = parent.breadcrumb
		}
		if displayPhase(name) != "" {
			defer log.SetDescription(log.Description())
			log.SetDescription(counts.breadcrumb)
		}
	}
	started := time.Now()
	var err error
	pprof.Do(context.WithValue(ctx, phaseKey{}, counts), pprof.Labels("phase", name), func(ctx context.Context) {
		trace.WithRegion(ctx, name, func() { err = fn(ctx) })
	})
	elapsed := time.Since(started)
	counts.mu.Lock()
	defer counts.mu.Unlock()
	rpchttp.AddTiming(ctx, rpchttp.TimingMetric{Name: name, Duration: elapsed, Counters: counts.counters})
	if log := task.TaskFromContext(ctx); log != nil {
		logPhase(log, counts, name, elapsed, err)
	}
	return err
}

// progressUnits is one task's bounded progress: the units of work known so far, the task's own module
// and each versioned dependency it found to index, and how many of them are done.
type progressUnits struct {
	mu          sync.Mutex
	done, total int
}

// withProgressUnits starts log's progress at none done of one unit, the task's own module.
func withProgressUnits(ctx context.Context) context.Context {
	if current := task.TaskFromContext(ctx); current != nil {
		current.SetProgress(0, 1)
	}
	return context.WithValue(ctx, progressKey{}, &progressUnits{total: 1})
}

// addProgress adds total units found and done units finished to the task progress in ctx, and reports
// it; outside a task it does nothing.
func addProgress(ctx context.Context, total, done int) {
	units, found := ctx.Value(progressKey{}).(*progressUnits)
	if !found {
		return
	}
	units.mu.Lock()
	defer units.mu.Unlock()
	units.total += total
	units.done += done
	if current := task.TaskFromContext(ctx); current != nil {
		current.SetProgress(units.done, units.total)
	}
}

func logPhase(log *task.Task, counts *phaseCounts, name string, elapsed time.Duration, err error) {
	line := strings.Repeat("  ", counts.depth) + name
	if err != nil {
		line += " failed after"
	}
	line += " " + elapsed.Round(time.Millisecond).String()
	for _, counter := range counts.counters[1:] {
		line += fmt.Sprintf(" %s=%d", counter.Name, counter.Value)
	}
	if err != nil {
		log.Errorf("%s: %v", line, err)
		return
	}
	if displayPhase(name) == "" {
		log.Debugf("%s", line)
	} else {
		log.Infof("%s", line)
	}
}

func displayPhase(name string) string {
	switch name {
	case "discover", "fingerprint-check", "verify-inputs":
		return "Filesystem"
	case "typecheck", "extract", "render-documents":
		return "Analyzing types"
	case "dependencies", "local-deps", "versioned-deps":
		return "Dependencies"
	case "checkout", "revision-base":
		return "Git"
	default:
		return ""
	}
}
