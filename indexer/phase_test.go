package indexer

import (
	"context"
	"errors"
	"fmt"
	"runtime/pprof"
	"time"

	rpchttp "github.com/flanksource/clicky/rpc/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// recordedPhaseLog keeps every line a phase logs, prefixed by its level, the task description it
// holds, and every progress it was set to.
type recordedPhaseLog struct {
	lines       []string
	description string
	progress    [][2]int
}

func (log *recordedPhaseLog) SetDescription(description string) { log.description = description }

func (log *recordedPhaseLog) Description() string { return log.description }

func (log *recordedPhaseLog) SetProgress(value, maximum int) {
	log.progress = append(log.progress, [2]int{value, maximum})
}

func (log *recordedPhaseLog) Infof(format string, args ...any) {
	log.lines = append(log.lines, "info "+fmt.Sprintf(format, args...))
}

func (log *recordedPhaseLog) Errorf(format string, args ...any) {
	log.lines = append(log.lines, "error "+fmt.Sprintf(format, args...))
}

var _ = Describe("Phase", func() {
	const outer, inner = "outer-phase", "inner-phase"

	phaseLabel := func(ctx context.Context) string {
		label, ok := pprof.Label(ctx, "phase")
		Expect(ok).To(BeTrue(), "the phase context carries no phase label")
		return label
	}

	It("runs fn under a context labelled with the phase, the innermost phase winning", func(ctx SpecContext) {
		var seen []string
		Expect(Phase(ctx, outer, func(ctx context.Context) error {
			seen = append(seen, phaseLabel(ctx))
			return Phase(ctx, inner, func(ctx context.Context) error {
				seen = append(seen, phaseLabel(ctx))
				return nil
			})
		})).To(Succeed())
		Expect(seen).To(Equal([]string{outer, inner}))
		_, labelled := pprof.Label(ctx, "phase")
		Expect(labelled).To(BeFalse(), "the caller's context must not gain the phase label")
	})

	It("records each phase's calls and counts in the context's timings, the innermost phase owning a count", func(ctx SpecContext) {
		const files, packages = 3, 2
		timed, timings := rpchttp.WithTimings(ctx)
		for range 2 {
			Expect(Phase(timed, outer, func(ctx context.Context) error {
				PhaseCount(ctx, "files", files)
				return Phase(ctx, inner, func(ctx context.Context) error {
					PhaseCount(ctx, "packages", packages)
					return nil
				})
			})).To(Succeed())
		}
		PhaseCount(timed, "files", files)

		Expect(timings.Metrics()).To(ConsistOf(
			And(HaveField("Name", outer), HaveField("Counters", Equal([]rpchttp.TimingCounter{{Name: "calls", Value: 2}, {Name: "files", Value: 2 * files}}))),
			And(HaveField("Name", inner), HaveField("Counters", Equal([]rpchttp.TimingCounter{{Name: "calls", Value: 2}, {Name: "packages", Value: 2 * packages}}))),
		))
	})

	It("returns fn's error and still records the failed phase's duration", func(ctx SpecContext) {
		const pause = 5 * time.Millisecond
		failure := errors.New("phase failed")
		timed, timings := rpchttp.WithTimings(ctx)
		Expect(Phase(timed, outer, func(context.Context) error {
			time.Sleep(pause)
			return failure
		})).To(MatchError(failure))
		duration, recorded := timings.Duration(outer)
		Expect(recorded).To(BeTrue())
		Expect(duration).To(BeNumerically(">=", pause))
	})

	It("logs one line per phase to the installed phase log, nested phases indented, failures as errors", func(ctx SpecContext) {
		log := &recordedPhaseLog{}
		failure := errors.New("load failed")
		Expect(Phase(withPhaseLog(ctx, log), outer, func(ctx context.Context) error {
			PhaseCount(ctx, "files", 3)
			return Phase(ctx, inner, func(context.Context) error { return failure })
		})).To(MatchError(failure))

		Expect(log.lines).To(HaveExactElements(
			MatchRegexp(`^error   inner-phase failed after \S+: load failed$`),
			MatchRegexp(`^error outer-phase failed after \S+ files=3: load failed$`),
		))
	})

	It("logs nothing without a phase log", func(ctx SpecContext) {
		log := &recordedPhaseLog{}
		Expect(Phase(withPhaseLog(ctx, log), outer, func(context.Context) error { return nil })).To(Succeed())
		Expect(Phase(ctx, inner, func(context.Context) error { return nil })).To(Succeed())
		Expect(log.lines).To(HaveExactElements(MatchRegexp(`^info outer-phase \S+$`)))
	})

	It("describes the task by the breadcrumb of running phases from each phase's start, restoring the parent's at its end", func(ctx SpecContext) {
		const location = "/checkout"
		log := &recordedPhaseLog{description: location}
		var seen []string
		Expect(Phase(withPhaseLog(ctx, log), outer, func(ctx context.Context) error {
			seen = append(seen, log.description)
			Expect(Phase(ctx, inner, func(context.Context) error {
				seen = append(seen, log.description)
				return nil
			})).To(Succeed())
			seen = append(seen, log.description)
			return nil
		})).To(Succeed())
		Expect(append(seen, log.description)).To(Equal([]string{outer, outer + " › " + inner, outer, location}))
	})

	It("reports a task's progress as units done of units known, never decreasing, the total only growing", func(ctx SpecContext) {
		log := &recordedPhaseLog{}
		units := withProgressUnits(ctx, log)
		addProgress(units, 2, 0)
		addProgress(units, 0, 1)
		addProgress(units, 0, 1)
		addProgress(units, 0, 1)
		addProgress(ctx, 0, 1)
		Expect(log.progress).To(Equal([][2]int{{0, 1}, {0, 3}, {1, 3}, {2, 3}, {3, 3}}))
	})
})
