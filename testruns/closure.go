package testruns

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/internal/canonicalhash"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

// closureVersion versions the closure fingerprint encoding.
const closureVersion = "uir-test-closure-v1"

// closure is the set of symbols a result depends on and how it was found.
type closure struct {
	source      storage.ClosureSource
	fingerprint string
}

// closures fingerprints the closures of one run's results against its snapshot. A static closure is a
// test symbol's complete callee graph, memoized per symbol.
type closures struct {
	database *gorm.DB
	pipeline *query.Pipeline
	snapshot storage.ModuleSnapshot
	inputs   string
	static   map[int64]*closure
}

// fingerprint digests the source, the run's dependency set and non-indexed inputs, and the sorted
// (canonical id, content fingerprint) pairs of handles at the snapshot.
func (closures *closures) fingerprint(ctx context.Context, source storage.ClosureSource, handles []int64) (string, bool, error) {
	generations, err := storage.SymbolGenerations(ctx, closures.database, closures.snapshot.ID, handles)
	if err != nil {
		return "", false, err
	}
	ids, err := storage.SymbolIDs(ctx, closures.database, handles)
	if err != nil {
		return "", false, err
	}
	pairs := map[string]int64{}
	for handle, id := range ids {
		generation, defined := generations[handle]
		if !defined || generation.ContentFP == nil {
			return "", false, nil
		}
		pairs[id] = *generation.ContentFP
	}
	digest := closures.header(source)
	digest.Count(len(pairs))
	for _, id := range slices.Sorted(maps.Keys(pairs)) {
		digest.Text(id)
		digest.Text(strconv.FormatInt(pairs[id], 10))
	}
	return digest.Sum(), true, nil
}

func (closures *closures) header(source storage.ClosureSource) canonicalhash.Hash {
	digest := canonicalhash.New(closureVersion)
	digest.Text(string(source))
	dependencies := ""
	if closures.snapshot.DependencySetHash != nil {
		dependencies = *closures.snapshot.DependencySetHash
	}
	digest.Text(dependencies)
	digest.Text(closures.inputs)
	return digest
}

// snapshotClosure is the closure of every symbol: the snapshot's whole content and context.
func (closures *closures) snapshotClosure() closure {
	digest := closures.header(storage.ClosureSnapshot)
	digest.Text(closures.snapshot.ContentSetHash)
	digest.Text(closures.snapshot.ContextHash)
	return closure{source: storage.ClosureSnapshot, fingerprint: digest.Sum()}
}

// of picks a result's closure: the declarations its own coverage executed, else the complete callee
// graph of the symbol the test is, else the snapshot. covered are the handles with covered statements
// in the test's own coverage, nil when it has none; symbol is the test's linked handle, if any.
func (closures *closures) of(ctx context.Context, covered []int64, symbol *int64) (closure, error) {
	if len(covered) > 0 {
		fingerprint, complete, err := closures.fingerprint(ctx, storage.ClosureCoverage, covered)
		if err != nil {
			return closure{}, err
		}
		if complete {
			return closure{source: storage.ClosureCoverage, fingerprint: fingerprint}, nil
		}
	}
	if symbol != nil {
		static, err := closures.staticClosure(ctx, *symbol)
		if err != nil {
			return closure{}, err
		}
		if static != nil {
			return *static, nil
		}
	}
	return closures.snapshotClosure(), nil
}

// staticClosure is the fingerprint of the callee graph of handle within the snapshot, or nil when that
// graph is not complete: cut by the depth or node limit, holding an unresolved call, or reaching a
// symbol the snapshot does not define. Standard library, builtin and external callees are left out;
// the environment and the dependency set stand for them.
func (closures *closures) staticClosure(ctx context.Context, handle int64) (*closure, error) {
	if memo, found := closures.static[handle]; found {
		return memo, nil
	}
	closures.static[handle] = nil
	ids, err := storage.SymbolIDs(ctx, closures.database, []int64{handle})
	if err != nil {
		return nil, err
	}
	built, err := closures.pipeline.Graph(ctx, query.GraphOptions{
		Symbol: ids[handle], Direction: graph.DirectionCallees, Depth: graph.MaxDepth, Limit: graph.MaxLimit,
		Exclude: []string{query.ExcludeStd, query.ExcludeBuiltin, query.ExcludeExternal},
		Scope:   query.ModuleScopeOptions{SnapshotID: closures.snapshot.ID.String()},
	})
	if err != nil {
		return nil, fmt.Errorf("build the call graph of test symbol %s: %w", ids[handle], err)
	}
	if len(built.Nodes) == 0 || built.Omitted.NodeLimit || built.Omitted.BeyondDepth > 0 || built.Omitted.Unresolved > 0 {
		return nil, nil
	}
	reached := make([]string, 0, len(built.Nodes))
	for _, node := range built.Nodes {
		if node.Unresolved {
			return nil, nil
		}
		reached = append(reached, node.ID)
	}
	handles, err := storage.StoredSymbolHandles(ctx, closures.database, reached)
	if err != nil || len(handles) != len(reached) {
		return nil, err
	}
	fingerprint, complete, err := closures.fingerprint(ctx, storage.ClosureStatic, slices.Collect(maps.Values(handles)))
	if err != nil || !complete {
		return nil, err
	}
	closures.static[handle] = &closure{source: storage.ClosureStatic, fingerprint: fingerprint}
	return closures.static[handle], nil
}
