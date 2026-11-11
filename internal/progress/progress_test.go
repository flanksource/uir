package progress

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/flanksource/clicky/task"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestProgress(t *testing.T) { RegisterFailHandler(Fail); RunSpecs(t, "Progress") }

var _ = Describe("Operation progress", func() {
	It("propagates cancellation from the owning task and marks the run cancelled", func(ctx SpecContext) {
		var id string
		_, err := Run(ctx, "Querying", "module-query", func(ctx context.Context) (int, error) {
			id = task.GroupFromContext(ctx).ID()
			task.TaskFromContext(ctx).Cancel()
			<-ctx.Done()
			return 0, ctx.Err()
		})
		Expect(err).To(MatchError(context.Canceled))
		Expect(task.SnapshotByID(id)).To(ContainElement(HaveField("Status", string(task.StatusCancelled))))
	})
	It("isolates a dependency step's task context and serializes concurrent progress increments", func(ctx SpecContext) {
		_, err := Run(ctx, "Indexing", "module-index", func(ctx context.Context) (int, error) {
			parent := task.TaskFromContext(ctx)
			step := task.GroupFromContext(ctx).StartStep("Dependency", task.WithContext(ctx))
			child := step.Task().Context()
			Expect(task.TaskFromContext(child)).To(BeIdenticalTo(step.Task()))
			Expect(task.TaskFromContext(ctx)).To(BeIdenticalTo(parent))
			const files = 20
			child, restore := Begin(child, "Filesystem", files)
			var workers sync.WaitGroup
			for range files {
				workers.Go(func() { Advance(child, 1) })
			}
			workers.Wait()
			Expect(task.SnapshotTask(step.Task(), nil)).To(And(HaveField("Progress", files), HaveField("MaxValue", files)))
			restore()
			step.Finish(nil)
			return 0, nil
		})
		Expect(err).NotTo(HaveOccurred())
	})
	It("attaches native task and group contexts, preserves values, and restores nested counts", func(ctx SpecContext) {
		type key struct{}
		_, err := Run(context.WithValue(ctx, key{}, "request"), "Querying", "module-query", func(ctx context.Context) (int, error) {
			current := task.TaskFromContext(ctx)
			Expect(current).NotTo(BeNil())
			Expect(task.GroupFromContext(ctx)).NotTo(BeNil())
			Expect(ctx.Value(key{})).To(Equal("request"))
			outer, finish := Begin(ctx, "Filesystem", 2)
			defer finish()
			Item(outer, "first.go")
			Advance(outer, 1)
			Expect(current.Description()).To(Equal("Filesystem › first.go"))
			Expect(task.SnapshotTask(current, nil)).To(And(HaveField("Progress", 1), HaveField("MaxValue", 2)))
			inner, restore := Begin(outer, "Git", 0)
			Item(inner, "HEAD")
			Expect(current.Description()).To(Equal("Git › HEAD"))
			restore()
			Expect(current.Description()).To(Equal("Filesystem › first.go"))
			Expect(task.SnapshotTask(current, nil)).To(And(HaveField("Progress", 1), HaveField("MaxValue", 2)))
			return 7, nil
		})
		Expect(err).NotTo(HaveOccurred())
	})
	It("keeps a failing operation's error and terminal status", func(ctx SpecContext) {
		failure := errors.New("invalid query")
		var id string
		value, err := Run(ctx, "Querying", "module-query", func(ctx context.Context) (int, error) {
			Expect(task.GroupFromContext(ctx)).NotTo(BeNil())
			id = task.GroupFromContext(ctx).ID()
			return 9, failure
		})
		Expect(value).To(Equal(9))
		Expect(err).To(MatchError(failure))
		Expect(task.SnapshotByID(id)).To(ContainElement(And(HaveField("Type", "task"), HaveField("Status", "failed"))))
	})
})
