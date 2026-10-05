package main

import (
	"context"
	"fmt"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir/storage"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

// modulePruneOptions names the root to prune, every registered root when none, and how many of its
// newest snapshots to keep.
type modulePruneOptions struct {
	Root string `flag:"root" args:"true" help:"Module root to prune; every registered root when omitted; also the first argument"`
	Keep int    `flag:"keep" help:"Newest snapshots of each root to keep, at least 1; location heads are always kept" required:"true"`
}

func registerPruneCommand(root *cobra.Command) {
	command := clicky.AddNamedCommandWithContext("prune", root, modulePruneOptions{}, func(ctx context.Context, options modulePruneOptions) ([]storage.PruneResult, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return nil, err
		}
		return pruneModules(ctx, database, options)
	})
	command.Use = "prune [root] --keep N"
	command.Short = "Delete each root's snapshots older than its newest N, and the documents, sources, and symbols only they used"
	command.Args = cobra.MaximumNArgs(1)
	setModuleRoute(command, "modules/prune")
}

// pruneModules prunes the named root, or each registered root in root key order, each in its own
// transaction; the first failure stops it.
func pruneModules(ctx context.Context, database *gorm.DB, options modulePruneOptions) ([]storage.PruneResult, error) {
	roots := []string{options.Root}
	if options.Root == "" {
		roots = nil
		if err := database.WithContext(ctx).Model(&storage.ModuleRoot{}).Order("root_key").Pluck("root_key", &roots).Error; err != nil {
			return nil, fmt.Errorf("list module roots: %w", err)
		}
	}
	results := make([]storage.PruneResult, 0, len(roots))
	for _, rootKey := range roots {
		result, err := storage.PruneHistory(ctx, database, rootKey, options.Keep)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}
