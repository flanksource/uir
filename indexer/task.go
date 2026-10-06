package indexer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/flanksource/clicky"
	clickytask "github.com/flanksource/clicky/task"
	commonscontext "github.com/flanksource/commons/context"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/taskruns"
)

// ModuleIndexKind is the clicky task kind of every run that publishes snapshots.
const ModuleIndexKind = "module-index"

// indexAttempts bounds how often a task runs an index whose inputs changed under it.
const indexAttempts = 3

// noTaskRetries replaces clicky's default retry policy, which retries any error whose text mentions a
// timeout or a connection. An index is retried only by indexTarget, and only when its inputs changed.
var noTaskRetries = clickytask.WithRetryConfig(clickytask.RetryConfig{})

type taskRunKey struct{}
type runGroupKey struct{}

// WithTaskRunID marks ctx as running inside the task run id, so every snapshot published under it
// records the run.
func WithTaskRunID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, taskRunKey{}, id)
}

func taskRunID(ctx context.Context) *string {
	id, found := ctx.Value(taskRunKey{}).(string)
	if !found || id == "" {
		return nil
	}
	return &id
}

// moduleTarget is one module checkout a run indexes as its own task, with the options it is indexed
// under.
type moduleTarget struct {
	rootKey  string
	location string
	options  ModuleOptions
}

// moduleRunSpec is everything that identifies a run: two requests with one identity share a run.
type moduleRunSpec struct {
	name     string
	identity string
	labels   map[string]string
	targets  []moduleTarget
}

// ModuleIndexRun is a started module-index run: one task per module, indexed one at a time. A request
// identical to a run still in progress joins that run instead of starting another.
type ModuleIndexRun struct {
	group clickytask.TypedGroup[[]ModuleResult]
}

// ID is the run's clicky task id, recorded on every snapshot it publishes.
func (run *ModuleIndexRun) ID() string { return run.group.ID() }

// StartModulesTask starts one run that indexes the modules below every request's path, under ctx,
// which bounds the run, not the caller's wait. The requests share one reason. The modules are located
// before the run starts, so a path without modules, or an unregistered checkout to reindex, fails
// here.
func StartModulesTask(ctx context.Context, indexer *Indexer, requests []ModuleOptions) (*ModuleIndexRun, error) {
	if indexer == nil {
		return nil, errors.New("UIR indexer is required")
	}
	if len(requests) == 0 {
		return nil, errors.New("a module index run needs at least one path")
	}
	paths, identities := make([]string, 0, len(requests)), make([]string, 0, len(requests))
	var targets []moduleTarget
	located := map[string]bool{}
	for _, options := range requests {
		if options.Reason != requests[0].Reason || options.ExistingOnly != requests[0].ExistingOnly {
			return nil, fmt.Errorf("one module index run cannot mix reasons %q and %q", requests[0].Reason, options.Reason)
		}
		found, err := indexer.moduleTargets(ctx, options)
		if err != nil {
			return nil, err
		}
		for _, target := range found {
			if !located[target.location] {
				located[target.location] = true
				targets = append(targets, target)
			}
		}
		path := options.Path
		if path == "" {
			path = "."
		}
		paths = append(paths, path)
		identities = append(identities, fmt.Sprintf("%s exact=%s tests=%t force=%t", found[0].options.Path, options.ExactLocation, options.IncludeTests, options.Force))
	}
	action := "Index"
	if requests[0].ExistingOnly {
		action = "Reindex"
	}
	return startModuleRun(ctx, indexer, moduleRunSpec{
		name:     fmt.Sprintf("%s %s", action, strings.Join(paths, ", ")),
		identity: fmt.Sprintf("%s:%s reason=%s", ModuleIndexKind, strings.Join(identities, " + "), requests[0].Reason),
		labels:   map[string]string{"path": strings.Join(paths, ","), "reason": string(requests[0].Reason)},
		targets:  targets,
	})
}

// StartMissingModulesTask starts reindexing every registered checkout that has no head.
func StartMissingModulesTask(ctx context.Context, indexer *Indexer, includeTests bool) (*ModuleIndexRun, error) {
	if indexer == nil {
		return nil, errors.New("UIR indexer is required")
	}
	targets, err := indexer.missingHeadTargets(ctx, includeTests)
	if err != nil {
		return nil, err
	}
	return startModuleRun(ctx, indexer, moduleRunSpec{
		name:     "Reindex registered checkouts without heads",
		identity: fmt.Sprintf("%s:missing-heads tests=%t", ModuleIndexKind, includeTests),
		labels:   map[string]string{"reason": string(storage.ReasonReindex)},
		targets:  targets,
	})
}

func startModuleRun(ctx context.Context, indexer *Indexer, spec moduleRunSpec) (*ModuleIndexRun, error) {
	published := &publishedSnapshots{}
	group := clicky.StartGroup[[]ModuleResult](spec.name, clickytask.WithKind(ModuleIndexKind), clickytask.WithLabels(spec.labels),
		clickytask.WithConcurrency(1), clickytask.WithGroupIdentity(spec.identity), clickytask.WithDetailsProvider(published.details))
	run := &ModuleIndexRun{group: group}
	if group.Joined() {
		return run, nil
	}
	if len(spec.targets) == 0 {
		group.Add("Nothing to index", func(commonscontext.Context, *clickytask.Task) ([]ModuleResult, error) {
			return []ModuleResult{}, nil
		}, clickytask.WithContext(ctx), noTaskRetries)
		return run, nil
	}
	memo := newRunMemo()
	for _, target := range spec.targets {
		module := group.Add(target.rootKey, func(taskContext commonscontext.Context, progress *clickytask.Task) ([]ModuleResult, error) {
			indexCtx := withRunMemo(context.WithValue(WithTaskRunID(taskContext, run.ID()), runGroupKey{}, group.Group), memo)
			results, err := indexer.indexTarget(indexCtx, target.options, progress.Warnf)
			if refreshErr := published.refresh(context.WithoutCancel(taskContext), indexer, run.ID()); refreshErr != nil {
				err = errors.Join(err, refreshErr)
			}
			if err != nil {
				return []ModuleResult{{RootKey: target.rootKey, Location: target.location, Error: err.Error()}}, err
			}
			for _, result := range results {
				progress.Infof("root=%s location=%s snapshot=%s files=%d parsed=%d reused=%d unchanged=%t",
					result.RootKey, result.Location, result.SnapshotID, result.Files, result.ParsedFiles, result.ReusedFiles, result.Unchanged)
			}
			return results, nil
		}, clickytask.WithContext(ctx), clickytask.WithCancellationDrain(), noTaskRetries)
		module.SetDescription(target.location)
	}
	return run, nil
}

// Wait waits until every module of the run has an outcome, or ctx is done, and returns one result per
// module: a failed module's result carries its error, and the returned error names every failure.
func (run *ModuleIndexRun) Wait(ctx context.Context) ([]ModuleResult, error) {
	if err := waitForRun(ctx, run.group.Group); err != nil {
		return nil, err
	}
	var results []ModuleResult
	var failures []error
	modules := 0
	for _, item := range run.group.GetTasks() {
		module, isModule := item.(clickytask.TypedTask[[]ModuleResult])
		if !isModule {
			continue
		}
		modules++
		indexed, err := module.GetResult()
		if err != nil && len(indexed) == 0 {
			// Cancelled before it started, so its callback never reported the module.
			indexed = []ModuleResult{{RootKey: module.Name(), Location: module.Description(), Error: err.Error()}}
		}
		results = append(results, indexed...)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s at %s: %w", module.Name(), module.Description(), err))
		}
	}
	// Snapshotting the finished run observes it terminal, which hands it to the installed clicky store
	// now rather than whenever a worker next retires a task, so a CLI that closes its store right after
	// Wait still records the run.
	clickytask.SnapshotByID(run.ID())
	if len(failures) > 0 {
		return results, fmt.Errorf("%d of %d modules failed: %w", len(failures), modules, errors.Join(failures...))
	}
	return results, nil
}

// waitForRun waits until every task of the run is terminal, or ctx is done; ctx bounds only the wait,
// never the run.
func waitForRun(ctx context.Context, group *clickytask.Group) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for status := group.Status(); status == clickytask.StatusPending || status == clickytask.StatusRunning; status = group.Status() {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for task run %s: %w", group.ID(), context.Cause(ctx))
		case <-ticker.C:
		}
	}
	return nil
}

// indexTarget indexes one module checkout, again while its inputs change under it, up to
// indexAttempts times in all; any other outcome is returned at once. An attempt whose inputs changed
// makes the run forget everything it remembered, so the next attempt indexes every dependency whose
// inputs changed again instead of reusing its stale snapshot.
func (indexer *Indexer) indexTarget(ctx context.Context, options ModuleOptions, warn func(string, ...any)) ([]ModuleResult, error) {
	ctx, memo := ensureRunMemo(ctx)
	for attempt := 1; ; attempt++ {
		results, err := indexer.IndexModules(ctx, options)
		if !errors.Is(err, ErrIndexInputsChanged) {
			return results, err
		}
		if attempt == indexAttempts {
			return nil, fmt.Errorf("index inputs changed in %d consecutive attempts: %w", indexAttempts, err)
		}
		memo.forget()
		warn("attempt %d: %v; indexing again", attempt, err)
	}
}

// indexStep runs index as a step of the task run in ctx, so work a module does on the way, such as a
// local dependency, shows as its own task; outside a run it just runs index.
func indexStep(ctx context.Context, name string, index func() ([]ModuleResult, error)) ([]ModuleResult, error) {
	group, inRun := ctx.Value(runGroupKey{}).(*clickytask.Group)
	if !inRun {
		return index()
	}
	step := group.StartStep(name)
	results, err := index()
	for _, result := range results {
		step.Task().Infof("root=%s snapshot=%s unchanged=%t", result.RootKey, result.SnapshotID, result.Unchanged)
	}
	step.Finish(err)
	return results, err
}

// publishedSnapshots is the run's group details: the snapshots published under the run, read back
// after each module so the record holds exactly what committed.
type publishedSnapshots struct {
	mu  sync.Mutex
	ids []string
}

func (published *publishedSnapshots) refresh(ctx context.Context, indexer *Indexer, runID string) error {
	ids, err := storage.TaskRunSnapshotIDs(ctx, indexer.database, runID)
	if err != nil {
		return err
	}
	published.mu.Lock()
	published.ids = ids
	published.mu.Unlock()
	return nil
}

func (published *publishedSnapshots) details() any {
	published.mu.Lock()
	defer published.mu.Unlock()
	return taskruns.RunDetails{SnapshotIDs: append([]string{}, published.ids...)}
}
