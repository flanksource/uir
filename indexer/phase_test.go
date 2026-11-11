package indexer

import (
	"context"
	"errors"
	"runtime/pprof"
	"time"

	rpchttp "github.com/flanksource/clicky/rpc/http"
	"github.com/flanksource/clicky/task"
	"github.com/flanksource/uir/internal/progress"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Phase", func() {
	It("preserves nested profiling labels and timing counters without a task", func(ctx SpecContext) {
		timed, timings := rpchttp.WithTimings(ctx)
		Expect(Phase(timed, "outer", func(ctx context.Context) error {
			label, ok := pprof.Label(ctx, "phase")
			Expect(ok).To(BeTrue())
			Expect(label).To(Equal("outer"))
			PhaseCount(ctx, "files", 3)
			return Phase(ctx, "inner", func(ctx context.Context) error {
				label, ok := pprof.Label(ctx, "phase")
				Expect(ok).To(BeTrue())
				Expect(label).To(Equal("inner"))
				PhaseCount(ctx, "packages", 2)
				return nil
			})
		})).To(Succeed())
		files, found := timings.Counter("outer", "files")
		Expect(found).To(BeTrue())
		Expect(files).To(Equal(int64(3)))
		packages, found := timings.Counter("inner", "packages")
		Expect(found).To(BeTrue())
		Expect(packages).To(Equal(int64(2)))
	})
	It("uses the native task for breadcrumbs, progress, failure logs and failed timings", func(ctx SpecContext) {
		failure := errors.New("load failed")
		_, err := progress.Run(ctx, "Index", ModuleIndexKind, func(ctx context.Context) (int, error) {
			current := task.TaskFromContext(ctx)
			current.SetDescription("/checkout")
			timed, timings := rpchttp.WithTimings(withProgressUnits(ctx))
			addProgress(timed, 2, 0)
			addProgress(timed, 0, 1)
			done, total := current.Progress()
			Expect([]int{done, total}).To(Equal([]int{1, 3}))
			Expect(Phase(timed, "typecheck", func(ctx context.Context) error {
				Expect(current.Description()).To(Equal("Analyzing types"))
				return Phase(ctx, "discover", func(context.Context) error {
					Expect(current.Description()).To(Equal("Filesystem"))
					time.Sleep(time.Millisecond)
					return failure
				})
			})).To(MatchError(failure))
			Expect(current.Description()).To(Equal("/checkout"))
			duration, found := timings.Duration("discover")
			Expect(found).To(BeTrue())
			Expect(duration).To(BeNumerically(">=", time.Millisecond))
			Expect(task.SnapshotTask(current, nil).Logs).To(ContainElement(HaveField("Message", ContainSubstring("discover failed after"))))
			return 0, nil
		})
		Expect(err).NotTo(HaveOccurred())
	})
})
