package indexer

import (
	"context"
	"fmt"
)

// CheckModulesCurrent verifies all modules below path without publishing snapshots.
func (indexer *Indexer) CheckModulesCurrent(ctx context.Context, path string, options CheckOptions) ([]ModuleResult, error) {
	roots, err := locateModuleRoots(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("no Go modules below %q", path)
	}
	results := make([]ModuleResult, 0, len(roots))
	for _, root := range roots {
		result, err := indexer.CheckCurrent(ctx, root.LocalPath, options)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}
