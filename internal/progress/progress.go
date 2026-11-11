package progress

import (
	"context"
	"errors"
	"sync"

	"github.com/flanksource/clicky/task"
)

type scopeKey struct{}

type scope struct {
	mu          sync.Mutex
	name        string
	done, total int
}

func Run[T any](ctx context.Context, name, kind string, fn func(context.Context) (T, error)) (result T, err error) {
	if task.TaskFromContext(ctx) != nil {
		return fn(ctx)
	}
	run := task.StartManagedRun(name, task.WithKind(kind), task.WithGroupContext(ctx))
	returned := false
	defer func() {
		status := task.StatusSuccess
		switch {
		case !returned:
			status = task.StatusFailed
			err = errors.New("operation panicked")
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			status = task.StatusCancelled
		case err != nil:
			status = task.StatusFailed
		}
		run.Finish(status, err)
	}()
	result, err = fn(run.Task().Context())
	if err == nil {
		err = run.Task().Context().Err()
	}
	returned = true
	return result, err
}

func Begin(ctx context.Context, name string, total int) (context.Context, func()) {
	if total < 0 {
		panic("progress total must not be negative")
	}
	current := task.TaskFromContext(ctx)
	if current == nil {
		return ctx, func() {}
	}
	description := current.Description()
	done, maximum := current.Progress()
	current.SetDescription(name)
	current.SetProgress(0, total)
	return context.WithValue(ctx, scopeKey{}, &scope{name: name, total: total}), func() {
		current.SetDescription(description)
		current.SetProgress(done, maximum)
	}
}

func Item(ctx context.Context, name string) {
	state, ok := ctx.Value(scopeKey{}).(*scope)
	if !ok {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	task.TaskFromContext(ctx).SetDescription(state.name + " › " + name)
}

func Advance(ctx context.Context, count int) {
	state, ok := ctx.Value(scopeKey{}).(*scope)
	if !ok {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if count < 0 || state.total > 0 && state.done+count > state.total {
		panic("invalid progress count")
	}
	state.done += count
	task.TaskFromContext(ctx).SetProgress(state.done, state.total)
}
