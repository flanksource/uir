package indexer

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/clicky"
	clickytask "github.com/flanksource/clicky/task"
	commonscontext "github.com/flanksource/commons/context"
)

// RevisionIndexRun is a started index of one commit, as a module-index run.
type RevisionIndexRun struct {
	group clickytask.TypedGroup[ModuleResult]
}

// ID is the run's clicky task id, recorded on the snapshot it publishes.
func (run *RevisionIndexRun) ID() string { return run.group.ID() }

// StartRevisionTask starts indexing one commit under ctx, which bounds the run, not the caller's wait.
// The run is labelled with the root, commit, and reason; a request for a commit that is still being
// indexed under the same test setting joins that run, so concurrent requests publish one snapshot.
func StartRevisionTask(ctx context.Context, indexer *Indexer, options RevisionOptions) (*RevisionIndexRun, error) {
	if indexer == nil {
		return nil, errors.New("UIR indexer is required")
	}
	if options.RootKey == "" || options.Commit == "" {
		return nil, fmt.Errorf("index revision: root %q and commit %q are both required", options.RootKey, options.Commit)
	}
	published := &publishedSnapshots{}
	short := options.Commit
	if len(short) > 12 {
		short = short[:12]
	}
	group := clicky.StartGroup[ModuleResult](fmt.Sprintf("Index %s@%s", options.RootKey, short), clickytask.WithKind(ModuleIndexKind),
		clickytask.WithLabels(map[string]string{"root": options.RootKey, "commit": options.Commit, "reason": string(options.Reason)}),
		clickytask.WithGroupIdentity(fmt.Sprintf("%s:%s@%s tests=%t", ModuleIndexKind, options.RootKey, options.Commit, options.IncludeTests)),
		clickytask.WithDetailsProvider(published.details))
	run := &RevisionIndexRun{group: group}
	if group.Joined() {
		return run, nil
	}
	group.Add(options.RootKey+"@"+short, func(taskContext commonscontext.Context, progress *clickytask.Task) (ModuleResult, error) {
		progress.Infof("indexing %s at %s from %s", options.RootKey, options.Commit, options.Checkout)
		result, err := indexer.IndexRevision(WithTaskRunID(taskContext, run.ID()), options)
		if refreshErr := published.refresh(context.WithoutCancel(taskContext), indexer, run.ID()); refreshErr != nil {
			err = errors.Join(err, refreshErr)
		}
		if err != nil {
			return ModuleResult{}, err
		}
		progress.Infof("root=%s commit=%s snapshot=%s unchanged=%t", result.RootKey, options.Commit, result.SnapshotID, result.Unchanged)
		return result, nil
	}, clickytask.WithContext(ctx), clickytask.WithCancellationDrain(), noTaskRetries)
	return run, nil
}

// Wait waits for the commit's snapshot, or until ctx is done.
func (run *RevisionIndexRun) Wait(ctx context.Context) (ModuleResult, error) {
	if err := waitForRun(ctx, run.group.Group); err != nil {
		return ModuleResult{}, err
	}
	clickytask.SnapshotByID(run.ID())
	for _, item := range run.group.GetTasks() {
		if revision, isRevision := item.(clickytask.TypedTask[ModuleResult]); isRevision {
			return revision.GetResult()
		}
	}
	return ModuleResult{}, fmt.Errorf("task run %s has no revision task", run.ID())
}
