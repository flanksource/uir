package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
)

func startProfiles(result *observation, native, forceGC bool) (func() error, error) {
	result.CPU, result.Heap = os.Getenv("GAVEL_CPU_PROFILE"), os.Getenv("GAVEL_MEM_PROFILE")
	if result.CPU == "" {
		result.CPU = filepath.Join(result.Case.Directory, "cpu.pprof")
	}
	if result.Heap == "" {
		result.Heap = filepath.Join(result.Case.Directory, "mem.pprof")
	}
	if !native {
		return func() error { return nil }, nil
	}
	cpu, err := os.Create(result.CPU)
	if err != nil {
		return nil, err
	}
	if err := pprof.StartCPUProfile(cpu); err != nil {
		return nil, errors.Join(err, cpu.Close())
	}
	execution, err := os.Create(result.Trace)
	if err != nil {
		pprof.StopCPUProfile()
		return nil, errors.Join(err, cpu.Close())
	}
	if err := trace.Start(execution); err != nil {
		pprof.StopCPUProfile()
		return nil, errors.Join(err, cpu.Close(), execution.Close())
	}
	return func() error {
		trace.Stop()
		pprof.StopCPUProfile()
		heap, err := os.Create(result.Heap)
		if err != nil {
			return errors.Join(err, cpu.Close(), execution.Close())
		}
		if forceGC {
			runtime.GC()
		}
		return errors.Join(pprof.WriteHeapProfile(heap), heap.Close(), cpu.Close(), execution.Close())
	}, nil
}
