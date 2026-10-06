package main

import (
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"runtime/pprof"
	"runtime/trace"

	"github.com/spf13/cobra"
)

// profileFiles are the profile files a command writes, open from the root's pre-run until run ends.
// cpu and trace are set only once their profile has started; heap is written when profiling stops.
type profileFiles struct {
	cpu, heap, trace *os.File
}

// bindProfileFlags adds --cpuprofile, --memprofile and --trace to every command, starting the
// profiles they name before the command runs; run stops them, so they are written even when the
// command fails. No other command defines a persistent pre-run, which would shadow this one.
func bindProfileFlags(root *cobra.Command, runtime *commandRuntime) {
	flags := root.PersistentFlags()
	flags.StringVar(&runtime.CPUProfile, "cpuprofile", "", "Write a CPU profile of the command to this file")
	flags.StringVar(&runtime.MemProfile, "memprofile", "", "Write a heap profile, taken after a GC when the command ends, to this file")
	flags.StringVar(&runtime.Trace, "trace", "", "Write an execution trace of the command to this file")
	root.PersistentPreRunE = func(*cobra.Command, []string) error { return runtime.startProfiles() }
}

func (runtime *commandRuntime) startProfiles() error {
	if runtime.CPUProfile != "" {
		file, err := createProfile(runtime.CPUProfile, "CPU profile")
		if err != nil {
			return err
		}
		if err := pprof.StartCPUProfile(file); err != nil {
			return errors.Join(fmt.Errorf("start CPU profile %q: %w", runtime.CPUProfile, err), closeProfile(file))
		}
		runtime.profiles.cpu = file
	}
	if runtime.MemProfile != "" {
		file, err := createProfile(runtime.MemProfile, "heap profile")
		if err != nil {
			return err
		}
		runtime.profiles.heap = file
	}
	if runtime.Trace != "" {
		file, err := createProfile(runtime.Trace, "execution trace")
		if err != nil {
			return err
		}
		if err := trace.Start(file); err != nil {
			return errors.Join(fmt.Errorf("start execution trace %q: %w", runtime.Trace, err), closeProfile(file))
		}
		runtime.profiles.trace = file
	}
	return nil
}

// stopProfiles stops every profile startProfiles started and closes its file, writing the heap
// profile after a GC so it shows what the command still held.
func (runtime *commandRuntime) stopProfiles() error {
	var errs []error
	if file := runtime.profiles.cpu; file != nil {
		pprof.StopCPUProfile()
		errs = append(errs, closeProfile(file))
	}
	if file := runtime.profiles.trace; file != nil {
		trace.Stop()
		errs = append(errs, closeProfile(file))
	}
	if file := runtime.profiles.heap; file != nil {
		goruntime.GC()
		if err := pprof.Lookup("heap").WriteTo(file, 0); err != nil {
			errs = append(errs, fmt.Errorf("write heap profile %q: %w", file.Name(), err))
		}
		errs = append(errs, closeProfile(file))
	}
	runtime.profiles = profileFiles{}
	return errors.Join(errs...)
}

func createProfile(path, kind string) (*os.File, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create %s %q: %w", kind, path, err)
	}
	return file, nil
}

func closeProfile(file *os.File) error {
	if err := file.Close(); err != nil {
		return fmt.Errorf("close profile %q: %w", file.Name(), err)
	}
	return nil
}
