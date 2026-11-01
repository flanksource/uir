//go:build hack

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/exp/trace"
)

type openRegion struct {
	Name   string
	Start  trace.Time
	Nested time.Duration
}

type phaseTotal struct {
	Count       int     `json:"count"`
	InclusiveMS float64 `json:"inclusive_ms"`
	ExclusiveMS float64 `json:"exclusive_ms"`
}

func analyseProfiles(ctx context.Context, result observation) error {
	phases, err := tracePhases(result.Trace)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(result.Case.Directory, "phases.json"), phases); err != nil {
		return err
	}
	for name, args := range map[string][]string{
		"cpu-top.txt":  {"tool", "pprof", "-top", result.CPU},
		"heap-top.txt": {"tool", "pprof", "-top", "-sample_index=alloc_space", result.Heap},
	} {
		body, err := commandOutput(ctx, "go", args...)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(result.Case.Directory, name), []byte(body+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func tracePhases(path string) (totals map[string]phaseTotal, returnErr error) {
	totals, _, err := tracePhaseBreakdown(path)
	return totals, err
}

func tracePhaseBreakdown(path string) (totals, paths map[string]phaseTotal, returnErr error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	reader, err := trace.NewReader(file)
	if err != nil {
		return nil, nil, err
	}
	stacks, totals, paths := map[trace.GoID][]openRegion{}, map[string]phaseTotal{}, map[string]phaseTotal{}
	for {
		event, err := reader.ReadEvent()
		if errors.Is(err, io.EOF) {
			return totals, paths, nil
		}
		if err != nil {
			return nil, nil, err
		}
		goroutine := event.Goroutine()
		switch event.Kind() {
		case trace.EventRegionBegin:
			stacks[goroutine] = append(stacks[goroutine], openRegion{Name: event.Region().Type, Start: event.Time()})
		case trace.EventRegionEnd:
			stack := stacks[goroutine]
			if len(stack) == 0 || stack[len(stack)-1].Name != event.Region().Type {
				return nil, nil, fmt.Errorf("unmatched trace region %q on goroutine %d", event.Region().Type, goroutine)
			}
			breadcrumb := make([]string, len(stack))
			for i, region := range stack {
				breadcrumb[i] = region.Name
			}
			top := stack[len(stack)-1]
			elapsed := event.Time().Sub(top.Start)
			stack = stack[:len(stack)-1]
			if len(stack) > 0 {
				stack[len(stack)-1].Nested += elapsed
			}
			stacks[goroutine] = stack
			total := totals[top.Name]
			total.Count++
			total.InclusiveMS += float64(elapsed) / float64(time.Millisecond)
			total.ExclusiveMS += float64(elapsed-top.Nested) / float64(time.Millisecond)
			totals[top.Name] = total
			path := strings.Join(breadcrumb, "/")
			pathTotal := paths[path]
			pathTotal.Count++
			pathTotal.InclusiveMS += float64(elapsed) / float64(time.Millisecond)
			pathTotal.ExclusiveMS += float64(elapsed-top.Nested) / float64(time.Millisecond)
			paths[path] = pathTotal
		}
	}
}
