package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

func startVite(ctx context.Context) (http.Handler, func() error, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("locate UIR web directory: %w", err)
	}
	webDir := filepath.Join(cwd, "web")
	vite := filepath.Join(webDir, "node_modules", ".bin", "vite")
	if _, err := os.Stat(vite); err != nil {
		return nil, nil, fmt.Errorf("vite is unavailable; run pnpm --dir web install: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, fmt.Errorf("reserve Vite port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return nil, nil, fmt.Errorf("release Vite port: %w", err)
	}
	processCtx, cancel := context.WithCancel(ctx)
	command := exec.CommandContext(processCtx, vite, "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--strictPort")
	command.Dir = webDir
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("start Vite: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	cleanup := func() error {
		cancel()
		if err := <-done; err != nil && !errors.Is(processCtx.Err(), context.Canceled) {
			return fmt.Errorf("wait for Vite: %w", err)
		}
		return nil
	}
	target, err := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		_ = cleanup()
		return nil, nil, fmt.Errorf("create Vite URL: %w", err)
	}
	client := &http.Client{Timeout: 250 * time.Millisecond}
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			_ = cleanup()
			return nil, nil, err
		}
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return httputil.NewSingleHostReverseProxy(target), cleanup, nil
			}
		}
		select {
		case err := <-done:
			cancel()
			return nil, nil, fmt.Errorf("vite exited before readiness: %w", err)
		case <-deadline.C:
			_ = cleanup()
			return nil, nil, errors.New("vite did not become ready within 15 seconds")
		case <-ctx.Done():
			_ = cleanup()
			return nil, nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
