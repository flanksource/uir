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
)

type phaseKey struct{}
type phaseLogKey struct{}
type progressKey struct{}

// phaseLog is where a phase reports its outcome, the phases running and the task's progress: a run's
// task, a *clickytask.Task.
type phaseLog interface {
	Infof(format string, args ...any)
	Errorf(format string, args ...any)
	SetDescription(description string)
	Description() string
	SetProgress(value, maximum int)
}

// withPhaseLog makes every phase run under ctx log one line to log when it ends.
func withPhaseLog(ctx context.Context, log phaseLog) context.Context {
	return context.WithValue(ctx, phaseLogKey{}, log)
}

// phaseCounts is one running phase's counters, in the order they were first counted.
type phaseCounts struct {
	depth      int
	log        phaseLog
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
	counts := &phaseCounts{breadcrumb: name, counters: []rpchttp.TimingCounter{{Name: "calls", Value: 1}}}
	log, logged := ctx.Value(phaseLogKey{}).(phaseLog)
	parent, nested := ctx.Value(phaseKey{}).(*phaseCounts)
	if nested {
		counts.depth = parent.depth + 1
	}
	if logged {
		counts.log = log
		if nested && parent.log == log {
			counts.breadcrumb = parent.breadcrumb + " › " + name
		}
		defer log.SetDescription(log.Description())
		log.SetDescription(counts.breadcrumb)
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
	if log, logged := ctx.Value(phaseLogKey{}).(phaseLog); logged {
		logPhase(log, counts, name, elapsed, err)
	}
	return err
}

// progressUnits is one task's bounded progress: the units of work known so far, the task's own module
// and each versioned dependency it found to index, and how many of them are done.
type progressUnits struct {
	mu          sync.Mutex
	log         phaseLog
	done, total int
}

// withProgressUnits starts log's progress at none done of one unit, the task's own module.
func withProgressUnits(ctx context.Context, log phaseLog) context.Context {
	log.SetProgress(0, 1)
	return context.WithValue(ctx, progressKey{}, &progressUnits{log: log, total: 1})
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
	units.log.SetProgress(units.done, units.total)
}

func logPhase(log phaseLog, counts *phaseCounts, name string, elapsed time.Duration, err error) {
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
	log.Infof("%s", line)
}
