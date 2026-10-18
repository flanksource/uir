package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if err := dispatch(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatch(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: go run ./hack/snapshotbench run|case|verify <options>")
	}
	switch args[0] {
	case "captain":
		return runCaptain(ctx, args[1:])
	case "trace-paths":
		if len(args) != 3 {
			return fmt.Errorf("trace-paths requires a trace and output JSON path")
		}
		_, paths, err := tracePhaseBreakdown(args[1])
		if err != nil {
			return err
		}
		return writeJSON(args[2], paths)
	case "run":
		return run(ctx, args[1:])
	case "case":
		if len(args) != 2 {
			return fmt.Errorf("case requires one descriptor JSON file")
		}
		return executeCase(ctx, args[1])
	case "verify":
		if len(args) != 2 {
			return fmt.Errorf("verify requires one run directory")
		}
		return verifyRun(args[1])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func writeJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}

func readJSON(path string, value any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, value); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
