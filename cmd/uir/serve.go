package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/route"
	"github.com/flanksource/clicky/rpc"
	rpchttp "github.com/flanksource/clicky/rpc/http"
	"github.com/flanksource/clicky/task"
	uiweb "github.com/flanksource/uir/web"
	"github.com/spf13/cobra"
)

type serveOptions struct {
	Host string
	Port int
	Dev  bool
}

func newServeCommand(runtime *commandRuntime) *cobra.Command {
	options := serveOptions{Host: "localhost", Port: 8080}
	command := &cobra.Command{
		Use:   "serve",
		Short: "Serve saved UIR snapshots in a web browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd.Context(), cmd.Root(), runtime, options)
		},
	}
	command.Flags().StringVar(&options.Host, "host", options.Host, "HTTP listen host")
	command.Flags().IntVar(&options.Port, "port", options.Port, "HTTP listen port")
	command.Flags().BoolVar(&options.Dev, "dev", false, "Start and proxy the Vite development server")
	clicky.MarkLocalOnly(command)
	return command
}

func runServe(ctx context.Context, root *cobra.Command, runtime *commandRuntime, options serveOptions) (returnErr error) {
	if strings.TrimSpace(options.Host) == "" {
		return errors.New("serve host is required")
	}
	if options.Port < 1 || options.Port > 65535 {
		return fmt.Errorf("serve port must be between 1 and 65535, got %d", options.Port)
	}
	if _, err := runtime.Database(ctx); err != nil {
		return fmt.Errorf("open UIR database for serve: %w", err)
	}
	ui, err := uiweb.Handler()
	if err != nil {
		return fmt.Errorf("load UIR web assets: %w", err)
	}
	if options.Dev {
		var cleanup func() error
		ui, cleanup, err = startVite(ctx)
		if err != nil {
			return err
		}
		defer func() { returnErr = errors.Join(returnErr, cleanup()) }()
	}
	handler, err := newServeHandler(root, runtime, ui)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(options.Host, strconv.Itoa(options.Port)))
	if err != nil {
		return fmt.Errorf("listen for UIR browser: %w", err)
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	shutdownResult := make(chan error, 1)
	serveDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-serveDone:
			return
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		shutdownResult <- server.Shutdown(shutdownCtx)
	}()
	fmt.Fprintf(os.Stderr, "UIR browser listening on http://%s\n", listener.Addr())
	err = server.Serve(listener)
	close(serveDone)
	if errors.Is(err, http.ErrServerClosed) {
		return <-shutdownResult
	}
	return err
}

func newServeHandler(root *cobra.Command, runtime *commandRuntime, ui http.Handler) (http.Handler, error) {
	if ui == nil {
		return nil, errors.New("UIR web handler is required")
	}
	config := &rpc.ServeConfig{
		Title: "UIR", Version: "1", StructuredErrorResponses: true,
		Executor: &rpc.ExecutorConfig{Enabled: true, SkipPreRun: true, PathPrefix: "/api/v1"},
	}
	server := rpc.NewSwaggerServer(config, root, &rpc.OpenAPIConfig{Title: "UIR", Version: "1"})
	if server.Executor() == nil {
		return nil, errors.New("register UIR Clicky operations: executor is unavailable")
	}
	mux := http.NewServeMux()
	router := route.NewRouter(mux)
	server.RegisterRoutes(router)
	task.RegisterHandlers(router, "/api/v1")
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health" {
			http.NotFound(w, r)
			return
		}
		ui.ServeHTTP(w, r)
	}))
	return rpchttp.TimingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), runtimeContextKey{}, runtime)
		mux.ServeHTTP(w, r.WithContext(ctx))
	})), nil
}
