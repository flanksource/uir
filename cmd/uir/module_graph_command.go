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
	RootKey    string   `flag:"root" help:"Module path to query"`
	Location   string   `flag:"location" help:"Registered checkout path"`
	SnapshotID string   `flag:"snapshot" help:"Explicit immutable snapshot UUID"`
}

// moduleGraphResult is the graph envelope: the graph's own fields at the top level beside the
// stages, warnings and candidates. JSON and YAML retain every field while human-readable formats
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
	command.Example = "  uir graph example.org/service/orders.Submit --direction callers --depth 3\n  uir graph example.org/service/orders.Submit --exclude external\n  uir graph example.org/service/orders.Order.Total --direction callers --access read,write\n  uir graph --symbol <id> --exclude none --format json"
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
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return moduleGraphResult{}, err
	}
	result, err := pipeline.Graph(ctx, query.GraphOptions{
		Selector: options.Selector, Symbol: options.Symbol, Access: access,
		Direction: graph.Direction(options.Direction), Depth: options.Depth, Limit: options.Limit, Exclude: options.Exclude,
		Scope: query.ModuleScopeOptions{RootKey: options.RootKey, Location: options.Location, SnapshotID: options.SnapshotID},
	})
	if err != nil {
		return moduleGraphResult{}, queryStatusError(err)
	}
	warnMissingHeads(ctx, result.Warnings)
	return moduleGraphResult{GraphResult: result}, nil
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
