package indexer

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/clicky"
	clickytask "github.com/flanksource/clicky/task"
	commonscontext "github.com/flanksource/commons/context"
)

// RunTask executes one incremental reindex as a context-bound Clicky task.
func RunTask(ctx context.Context, indexer *Indexer, options Options) (Result, error) {
	if indexer == nil {
		return Result{}, errors.New("UIR indexer is required")
	}
	running := clicky.StartTask(fmt.Sprintf("Reindex %s", options.ProjectKey), func(taskContext commonscontext.Context, progress *clickytask.Task) (Result, error) {
		progress.Infof("discovering Go sources under %s", options.Path)
		result, err := indexer.Index(taskContext, options)
		if err != nil {
			return Result{}, err
		}
		progress.Infof("snapshot=%s files=%d parsed=%d reused=%d", result.SnapshotID, result.Files, result.ParsedFiles, result.ReusedFiles)
		return result, nil
	}, clickytask.WithContext(ctx), clickytask.WithCancellationDrain())
	return running.GetResult()
}
