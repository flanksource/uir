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
	"github.com/flanksource/clicky/entity"
	"github.com/flanksource/clicky/shutdown"
	clickytask "github.com/flanksource/clicky/task"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/taskruns"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

// version is set at link time by `make binary VERSION=...`.
var version = "dev"

// dsnEnv and schemaEnv supply --dsn and --schema when the flags are not set.
const (
	dsnEnv    = "UIR_DSN"
	schemaEnv = "UIR_SCHEMA"
)

type runtimeContextKey struct{}

type commandRuntime struct {
	DSN        string
	Schema     string
	GopatchBin string
	database   *gorm.DB
	owned      bool
	// taskRuns is the clicky run store installed while the database is open, so every run this
	// process finishes is kept in task_runs.
	taskRuns *taskruns.Store
	// serveContext bounds the runs an HTTP request starts and does not wait for; only `uir serve`
	// sets it, and cancelling it on shutdown cancels those runs.
	serveContext context.Context
	// detached counts the runs started under serveContext that have not finished.
	detached sync.WaitGroup
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
	defer func() { returnErr = errors.Join(returnErr, runtime.Close()) }()
	defer shutdown.Shutdown()
	root := newRootCommand(runtime)
	root.SetArgs(args)
	return root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))
}

func newRootCommand(runtime *commandRuntime) *cobra.Command {
	root := &cobra.Command{
		Use:          "uir",
		Short:        "Query and incrementally index Universal Intermediate Representation modules",
		Version:      version,
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&runtime.DSN, "dsn", "", "PostgreSQL DSN, sqlite:// URL, or .db path (default $"+dsnEnv+", then ~/.config/uir/uir.db)")
	root.PersistentFlags().StringVar(&runtime.Schema, "schema", "", "PostgreSQL schema (default $"+schemaEnv+")")
	properties.BindFlags(root.PersistentFlags())
	clicky.BindAllFlagsToCommand(root, "tasks", "format")
	clicky.GenerateCLI(root)
	registerModuleCommands(root)
	registerRefactorCommands(root, runtime)
	registerDiffCommand(root)
	registerHistoryCommand(root)
	registerSystemInfoCommand(root)
	root.AddCommand(newServeCommand(runtime))
	versionCommand := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s version %s\n", cmd.Root().Name(), cmd.Root().Version)
			return err
		},
	}
	clicky.MarkLocalOnly(versionCommand)
	root.AddCommand(versionCommand)
	return root
}

// Database opens the UIR database once, and installs the task-run store over it.
func (runtime *commandRuntime) Database(ctx context.Context) (*gorm.DB, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.database == nil {
		if err := runtime.open(ctx); err != nil {
			return nil, err
		}
	}
	if runtime.taskRuns == nil {
		runtime.taskRuns = taskruns.New(runtime.database)
		clickytask.SetStore(context.Background(), runtime.taskRuns)
	}
	return runtime.database, nil
}

// TaskRuns is the store of finished task runs, for serving them.
func (runtime *commandRuntime) TaskRuns(ctx context.Context) (*taskruns.Store, error) {
	if _, err := runtime.Database(ctx); err != nil {
		return nil, err
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.taskRuns, nil
}

func (runtime *commandRuntime) open(ctx context.Context) error {
	if runtime.DSN == "" {
		runtime.DSN = os.Getenv(dsnEnv)
	}
	if runtime.Schema == "" {
		runtime.Schema = os.Getenv(schemaEnv)
	}
	if runtime.DSN == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("find home directory for UIR database: %w", err)
		}
		directory := filepath.Join(home, ".config", "uir")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create UIR data directory %q: %w", directory, err)
		}
		runtime.DSN = filepath.Join(directory, "uir.db")
	}
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: runtime.DSN, Schema: runtime.Schema})
	if err != nil {
		return err
	}
	runtime.database, runtime.owned = database, true
	return nil
}

// Close detaches the task-run store, which first saves every run already finished, and then closes
// the database when the runtime opened it.
func (runtime *commandRuntime) Close() error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.taskRuns != nil {
		clickytask.SetStore(context.Background(), nil)
		runtime.taskRuns = nil
	}
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

func databaseFor(ctx context.Context) (*gorm.DB, error) {
	runtime, err := runtimeFor(ctx)
	if err != nil {
		return nil, err
	}
	return runtime.Database(ctx)
}

func runtimeFor(ctx context.Context) (*commandRuntime, error) {
	runtime, ok := ctx.Value(runtimeContextKey{}).(*commandRuntime)
	if !ok || runtime == nil {
		return nil, errors.New("UIR command runtime is missing")
	}
	return runtime, nil
}

// detachedRequest reports whether an operation runs for an HTTP request, which starts its task run
// under the serve context and answers with the run id instead of waiting for it.
func detachedRequest(ctx context.Context) bool {
	return entity.OperationSurfaceFromContext(ctx) == "http"
}

// runContext is the context a task run started by this operation runs under: the serve context for
// an HTTP request, so the run outlives the request, else the operation's own.
func runContext(ctx context.Context) (context.Context, error) {
	if !detachedRequest(ctx) {
		return ctx, nil
	}
	runtime, err := runtimeFor(ctx)
	if err != nil {
		return nil, err
	}
	if runtime.serveContext == nil {
		return nil, errors.New("an HTTP request can start a task run only under uir serve, which provides the serve context")
	}
	return runtime.serveContext, nil
}
