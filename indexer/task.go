package indexer

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/clicky"
	clickytask "github.com/flanksource/clicky/task"
	commonscontext "github.com/flanksource/commons/context"
)

func RunModulesTask(ctx context.Context, indexer *Indexer, options ModuleOptions) ([]ModuleResult, error) {
	if indexer == nil {
		return nil, errors.New("UIR indexer is required")
	}
	path := options.Path
	if path == "" {
		path = "."
	}
	action := "Index"
	if options.ExistingOnly {
		action = "Reindex"
	}
	group := clicky.StartGroup[[]ModuleResult](fmt.Sprintf("%s %s", action, path), clickytask.WithKind("module-index"), clickytask.WithLabels(map[string]string{"path": path}))
	running := group.Add(fmt.Sprintf("%s Go modules under %s", action, path), func(taskContext commonscontext.Context, progress *clickytask.Task) ([]ModuleResult, error) {
		progress.Infof("discovering Go modules under %s", path)
		results, err := indexer.IndexModules(taskContext, options)
		if err != nil {
			return nil, err
		}
		for _, result := range results {
			progress.Infof("root=%s snapshot=%s files=%d parsed=%d reused=%d", result.RootKey, result.SnapshotID, result.Files, result.ParsedFiles, result.ReusedFiles)
		}
		return results, nil
	}, clickytask.WithContext(ctx), clickytask.WithCancellationDrain())
	return running.GetResult()
}
