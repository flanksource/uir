// Command uir exposes database-backed module roots, queries, and indexing.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir/storage"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

// version is set at link time by `make binary VERSION=...`.
var version = "dev"

type runtimeContextKey struct{}

type commandRuntime struct {
	DSN      string
	Schema   string
	database *gorm.DB
	owned    bool
	mu       sync.Mutex
}

func main() {
	os.Exit(execute())
}

func execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func run(ctx context.Context, args []string) (returnErr error) {
	runtime := &commandRuntime{}
	root := newRootCommand(runtime)
	root.SetArgs(args)
	defer func() { returnErr = errors.Join(returnErr, runtime.Close()) }()
	return root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))
}

func newRootCommand(runtime *commandRuntime) *cobra.Command {
	root := &cobra.Command{
		Use:          "uir",
		Short:        "Query and incrementally index Universal Intermediate Representation modules",
		Version:      version,
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&runtime.DSN, "dsn", "", "PostgreSQL DSN, sqlite:// URL, or .db path")
	root.PersistentFlags().StringVar(&runtime.Schema, "schema", "", "PostgreSQL schema")
	clicky.BindAllFlagsToCommand(root, "tasks", "format")
	clicky.GenerateCLI(root)
	registerModuleCommands(root)
	root.AddCommand(newServeCommand(runtime))
	return root
}

func (runtime *commandRuntime) Database(ctx context.Context) (*gorm.DB, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.database != nil {
		return runtime.database, nil
	}
	if runtime.DSN == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("find UIR configuration directory: %w", err)
		}
		directory := filepath.Join(configDir, "uir")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create UIR data directory %q: %w", directory, err)
		}
		runtime.DSN = filepath.Join(directory, "uir.db")
	}
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: runtime.DSN, Schema: runtime.Schema})
	if err != nil {
		return nil, err
	}
	runtime.database, runtime.owned = database, true
	return database, nil
}

func (runtime *commandRuntime) Close() error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.database == nil || !runtime.owned {
		return nil
	}
	sqlDB, err := runtime.database.DB()
	if err != nil {
		return fmt.Errorf("access UIR database for close: %w", err)
	}
	runtime.database = nil
	return sqlDB.Close()
}

func withDatabase(ctx context.Context, database *gorm.DB) context.Context {
	return context.WithValue(ctx, runtimeContextKey{}, &commandRuntime{database: database})
}

func databaseFor(ctx context.Context) (*gorm.DB, error) {
	runtime, ok := ctx.Value(runtimeContextKey{}).(*commandRuntime)
	if !ok || runtime == nil {
		return nil, errors.New("UIR command runtime is missing")
	}
	return runtime.Database(ctx)
}
