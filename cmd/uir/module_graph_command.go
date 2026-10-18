package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/entity"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/symboldiff"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

type moduleGraphOptions struct {
	Selector   string   `flag:"selector" args:"true" help:"Symbol expression naming the function or method at the root of the graph; also the first argument"`
	Symbol     string   `flag:"symbol" help:"Canonical symbol id of the root, as a graph node or candidate carries it"`
	Direction  string   `flag:"direction" help:"Calls to follow from the root: callees, callers, or both (the default)"`
	Depth      int      `flag:"depth"`
	Limit      int      `flag:"limit"`
	Exclude    []string `flag:"exclude"`
	Access     []string `flag:"access" help:"Edge types to follow: call (calls and dispatches), read, write (repeatable or comma separated; default call)"`
	RootKey    string   `flag:"root" help:"Module path to query; defaults to the module of the current checkout"`
	Location   string   `flag:"location" help:"Registered checkout path; defaults to the current checkout"`
	SnapshotID string   `flag:"snapshot" help:"Explicit immutable snapshot UUID"`
	Since      string   `flag:"since" help:"Revision to compare against, resolved as uir diff resolves one: reports which callees of the root changed since its newest clean snapshot, and the path to each"`
	// SinceSnapshot is named as uir diff names its snapshot overrides.
	SinceSnapshot string `flag:"since-snapshot" help:"Snapshot UUID to use for --since instead of its newest clean snapshot"`
	allRootsOption
}

// moduleGraphLong documents the change query and the edges it cannot see.
const moduleGraphLong = `With --since <rev>, the graph is a change query: it walks the callees of the root in the selected
snapshot, marks each node whose symbol was added or whose signature, structure or body changed since
the snapshot of <rev>, and each node of a module-cache dependency whose selected version changed, and
reports a shortest path from the root to each. "changed: false" means nothing the walk reached changed.

The answer is only as complete as the call edges the index records. Known false negatives:
  - an implementation outside the import closure of the interface it satisfies
  - dispatch through generic or type-set interfaces
  - calls through method values, and through func-typed fields or variables
  - anything beyond --depth or --limit, behind an unresolved call, or in another root of the scope`

// moduleGraphResult is the graph envelope: the graph's own fields at the top level beside the
// stages and candidates. JSON and YAML retain every field while human-readable formats
// render the tree of callers and callees.
type moduleGraphResult struct {
	query.GraphResult
}

func registerModuleGraphCommand(root *cobra.Command) {
	command := clicky.AddNamedCommandWithContext("graph", root, moduleGraphOptions{}, func(ctx context.Context, options moduleGraphOptions) (moduleGraphResult, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return moduleGraphResult{}, err
		}
		return graphModules(ctx, database, options)
	})
	// Use names no argument: the API generator would publish a second selector parameter beside the flag.
	command.Short = "Draw the callers and callees of one function or method, or the readers and writers of a data symbol"
	command.Long = moduleGraphLong
	command.Example = "  uir graph example.org/service/orders.Submit --direction callers --depth 3\n  uir graph example.org/service/orders.Submit --exclude external\n  uir graph example.org/service/orders.Order.Total --direction callers --access read,write\n  uir graph --symbol <id> --exclude none --format json\n  uir graph example.org/service/orders.Submit --since v1.4.0 --depth 6"
	command.Args = cobra.MaximumNArgs(1)
	command.Flags().Lookup("depth").Usage = fmt.Sprintf("Calls away from the root, from 1 through %d (default %d)", graph.MaxDepth, graph.DefaultDepth)
	command.Flags().Lookup("limit").Usage = fmt.Sprintf("Maximum nodes from 1 through %d (default %d)", graph.MaxLimit, graph.DefaultLimit)
	command.Flags().Lookup("exclude").Usage = fmt.Sprintf(
		"Comma-separated package patterns whose nodes are left out: an import path, a path ending in /..., or %s, %s, %s; %s alone excludes nothing (default %s)",
		query.ExcludeStd, query.ExcludeBuiltin, query.ExcludeExternal, query.ExcludeNone, strings.Join(query.DefaultGraphExclusions, ","))
	setModuleRoute(command, "modules/graph")
	command.Annotations["clicky/operation-method"] = http.MethodGet
}

func graphModules(ctx context.Context, database *gorm.DB, options moduleGraphOptions) (moduleGraphResult, error) {
	access, err := graph.ParseAccess(options.Access)
	if err != nil {
		return moduleGraphResult{}, entity.NewStatusError(http.StatusBadRequest, "invalid_query", err.Error())
	}
	scope, err := moduleScope(ctx, database, checkoutDefaults{
		Root: options.RootKey, Location: options.Location, Snapshot: options.SnapshotID, All: options.All, Required: options.Since != "",
	})
	if err != nil {
		return moduleGraphResult{}, err
	}
	since, err := graphSince(ctx, database, options, scope)
	if err != nil {
		return moduleGraphResult{}, err
	}
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return moduleGraphResult{}, err
	}
	result, err := pipeline.Graph(ctx, query.GraphOptions{
		Selector: options.Selector, Symbol: options.Symbol, Access: access,
		Direction: graph.Direction(options.Direction), Depth: options.Depth, Limit: options.Limit, Exclude: options.Exclude,
		Since: since, Scope: scope,
	})
	if err != nil {
		return moduleGraphResult{}, queryStatusError(err)
	}
	return moduleGraphResult{GraphResult: result}, nil
}

// graphSince selects the snapshot of --since in the scope's root, as uir diff selects one side of a
// range; nil without --since.
func graphSince(ctx context.Context, database *gorm.DB, options moduleGraphOptions, scope query.ModuleScopeOptions) (*query.GraphSince, error) {
	var failure *entity.StatusError
	switch {
	case options.Since == "" && options.SinceSnapshot != "":
		failure = entity.NewStatusError(http.StatusBadRequest, "invalid_query", "since-snapshot overrides the snapshot of since, which is missing")
	case options.Since == "":
		return nil, nil
	case scope.RootKey == "":
		failure = entity.NewStatusError(http.StatusBadRequest, "missing_root", "since resolves its revision in one root, and none is selected")
	}
	if failure != nil {
		failure.Hint = "Pass since with the revision to compare against, and root with the module it belongs to."
		return nil, failure
	}
	snapshot, err := symboldiff.RevisionSnapshot(ctx, database, symboldiff.RevisionSnapshotOptions{
		RootKey: scope.RootKey, Location: scope.Location, Revision: options.Since, Snapshot: options.SinceSnapshot, SnapshotFlag: "--since-snapshot",
	})
	if err != nil {
		return nil, err
	}
	return &query.GraphSince{Revision: options.Since, SnapshotID: snapshot.ID}, nil
}

// queryStatusError gives a query failure the HTTP status it answers with: 400 for a query that
// cannot be run as written, 404 for a symbol that is not indexed. Any other error is returned as is.
func queryStatusError(err error) error {
	var invalid *query.InvalidQueryError
	if errors.As(err, &invalid) {
		failure := entity.NewStatusError(http.StatusBadRequest, "invalid_query", invalid.Message)
		failure.Hint = invalid.Hint
		if invalid.Line > 0 {
			failure.Context = map[string]any{"line": invalid.Line, "column": invalid.Column}
		}
		return failure
	}
	var unresolved *query.UnresolvedSymbolError
	if errors.As(err, &unresolved) {
		failure := entity.NewStatusError(http.StatusNotFound, "symbol_not_found", unresolved.Error())
		failure.Hint = "Check the symbol spelling or select a snapshot where it is indexed."
		return failure
	}
	return err
}
