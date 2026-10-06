package indexer

import (
	"context"
	"runtime/pprof"
	"runtime/trace"
)

// Phase runs fn as one named indexing phase: CPU samples taken while it runs, including on goroutines
// it starts, carry the pprof label phase=<name>, and an execution trace shows it as a region. Phases nest; the innermost phase's
// label wins in a profile, and regions nest in a trace.
func Phase(ctx context.Context, name string, fn func(context.Context) error) error {
	var err error
	pprof.Do(ctx, pprof.Labels("phase", name), func(ctx context.Context) {
		trace.WithRegion(ctx, name, func() { err = fn(ctx) })
	})
	return err
}
